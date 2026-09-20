package scruff

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vladzaharia/scruff-recon-sdk/core"
)

// --- inbox -----------------------------------------------------------------

// InboxOptions controls an inbox fetch.
type InboxOptions struct {
	// Cutoff pages backwards. Pass the ActionAt of the oldest conversation you
	// hold, as unix seconds. Zero fetches the first page.
	Cutoff time.Time
	// Limit is the page size. Zero lets the server choose, which is what the
	// app does when remote config supplies no limit.
	Limit int
}

// Inbox lists conversation threads, most recently active first.
//
// A thread has no id of its own: each conversation's ID is the peer's profile
// id, which is also what you pass to Chat and Send.
func (c *Client) Inbox(ctx context.Context, opt InboxOptions) (*InboxPage, error) {
	q := url.Values{}
	if !opt.Cutoff.IsZero() {
		q.Set("cutoff_timestamp", strconv.FormatInt(opt.Cutoff.Unix(), 10))
	}
	if opt.Limit > 0 {
		q.Set("limit", strconv.Itoa(opt.Limit))
	}
	var out InboxPage
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet, Path: "/app/inbox/stream", Query: q,
	}, &out)
	return &out, err
}

// MarkInboxViewed clears unread badges up to a timestamp.
func (c *Client) MarkInboxViewed(ctx context.Context, upTo time.Time) error {
	r := &core.Request{Method: http.MethodPost, Path: "/app/inbox/view"}
	r.SetForm("last_message_timestamp", strconv.FormatInt(upTo.Unix(), 10))
	return c.tr.JSON(ctx, r, nil)
}

// DeleteThread removes a conversation.
func (c *Client) DeleteThread(ctx context.Context, peerProfileID string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodDelete, Path: "/app/inbox/thread",
		Query: url.Values{"profile_id": {peerProfileID}},
	}, nil)
}

// --- chat ------------------------------------------------------------------

// ChatOptions controls a history fetch.
type ChatOptions struct {
	// MaxVersion pages backwards: the server returns messages with a version
	// strictly below this. Zero fetches the newest page.
	MaxVersion int64
	// Limit is the page size; zero lets the server choose.
	Limit int
}

// Chat fetches one page of history with a peer, ascending by version.
//
// Version — not id, not timestamp — is the ordering key: it is a dense,
// gap-free per-conversation sequence starting at 1.
func (c *Client) Chat(ctx context.Context, peerProfileID string, opt ChatOptions) (*ChatPage, error) {
	q := url.Values{
		"profile_id":    {peerProfileID},
		"inbox_style":   {"2"}, // the only value the app sends
		"free_features": {"[]"},
	}
	if opt.MaxVersion > 0 {
		q.Set("max_version", strconv.FormatInt(opt.MaxVersion, 10))
	}
	if opt.Limit > 0 {
		q.Set("limit", strconv.Itoa(opt.Limit))
	}
	var out ChatPage
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet, Path: "/app/chat", Query: q,
	}, &out)
	if err != nil {
		// A 404 here means "no more messages", which is how backfill
		// terminates rather than an error condition.
		if e, ok := core.AsAPIError(err); ok && e.IsNotFound() {
			return &ChatPage{}, nil
		}
		return nil, err
	}
	return &out, nil
}

// AllMessages walks a conversation backwards from the newest message, calling
// yield for each until the start of history or maxMessages.
func (c *Client) AllMessages(ctx context.Context, peerProfileID string, maxMessages int, yield func(Message) error) error {
	page := func(ctx context.Context, cursor int64) ([]Message, int64, bool, error) {
		p, err := c.Chat(ctx, peerProfileID, ChatOptions{MaxVersion: cursor})
		if err != nil {
			return nil, 0, false, err
		}
		if len(p.Results) == 0 {
			return nil, 0, true, nil
		}
		// Results ascend by version, so the lowest is the next cursor.
		lowest := p.Results[0].Version
		// Version 1 is the first message ever; min_version+1 is the free-tier
		// floor. Either means we are done.
		done := lowest <= 1 || lowest <= p.MinVersion+1
		return p.Results, lowest, done, nil
	}
	return core.Paginate(ctx, int64(0), maxMessages, page, yield)
}

// SendOptions describes an outgoing message.
type SendOptions struct {
	// MessageType defaults to MessageTypeText.
	MessageType int
	// Text is the body for text messages.
	Text string
	// Image and Video attach binaries.
	Image     []byte
	ImageMime string
	Video     []byte
	VideoName string
	// Reaction and ReactedTo form a type-8 reaction.
	Reaction  string
	ReactedTo string
	// ReplyGUID quotes another message; ReplyMomentID quotes a Moment.
	ReplyGUID     string
	ReplyMomentID string
	// SharedAlbumID shares a private album. This is how album sharing works —
	// there is no separate permissions call in this protocol version.
	SharedAlbumID   string
	AlbumShareLimit string
	// MediaBehavior selects normal or single-view media.
	MediaBehavior int
	// Location is the body for type-4 messages.
	Location string
	// GUID overrides the generated message guid. Leave empty in normal use.
	GUID string
}

// Send posts a message and returns its guid.
//
// Parts are emitted in the exact order the app uses, which keeps the request
// indistinguishable from the real client.
func (c *Client) Send(ctx context.Context, peerProfileID string, opt SendOptions) (string, error) {
	if opt.MessageType == 0 {
		opt.MessageType = MessageTypeText
	}
	guid := opt.GUID
	if guid == "" {
		guid = NewMessageGUID()
	}

	r := &core.Request{
		Method:               http.MethodPost,
		Path:                 "/app/chat",
		MultipartContentType: "multipart/mixed",
	}

	// Order matters: image, video, then the scalar fields.
	if len(opt.Image) > 0 {
		mime := opt.ImageMime
		if mime == "" {
			mime = "image/jpg"
		}
		r.AddFilePart("image", "image.jpg", mime, opt.Image)
	}
	if len(opt.Video) > 0 {
		name := opt.VideoName
		if name == "" {
			name = "video.mp4"
		}
		r.AddFilePart("video", name, "application/octet-stream", opt.Video)
	}

	r.AddPart("recipient", peerProfileID)
	r.AddPart("guid", guid)
	// request_guid equals guid on a send, which is what the app does.
	r.AddPart("request_guid", guid)
	r.AddPart("message_type", strconv.Itoa(opt.MessageType))

	addIf := func(name, v string) {
		if v != "" {
			r.AddPart(name, v)
		}
	}
	addIf("location", opt.Location)
	addIf("reaction", opt.Reaction)
	addIf("reacted_to", opt.ReactedTo)
	addIf("message", opt.Text)
	if opt.VideoName != "" {
		r.AddPart("video_filename", opt.VideoName)
	}
	if opt.MediaBehavior != 0 {
		r.AddPart("media_behavior", strconv.Itoa(opt.MediaBehavior))
	}
	addIf("reply[guid]", opt.ReplyGUID)
	addIf("album_share_limit", opt.AlbumShareLimit)
	addIf("shared_album_id", opt.SharedAlbumID)
	addIf("reply[moment_id]", opt.ReplyMomentID)

	var out results[struct {
		GUID string `json:"guid"`
	}]
	if err := c.tr.JSON(ctx, r, &out); err != nil {
		return "", err
	}
	if out.Results.GUID != "" {
		return out.Results.GUID, nil
	}
	return guid, nil
}

// SendText is Send for a plain text message.
func (c *Client) SendText(ctx context.Context, peerProfileID, text string) (string, error) {
	return c.Send(ctx, peerProfileID, SendOptions{Text: text})
}

// React adds an emoji reaction to a message.
func (c *Client) React(ctx context.Context, peerProfileID, targetGUID, emoji string) (string, error) {
	return c.Send(ctx, peerProfileID, SendOptions{
		MessageType: MessageTypeReaction, Reaction: emoji, ReactedTo: targetGUID,
	})
}

// Unsend retracts a message you sent.
func (c *Client) Unsend(ctx context.Context, peerProfileID, guid string, version int64) error {
	r := &core.Request{Method: http.MethodPut, Path: "/app/chat"}
	r.SetForm("profile_id", peerProfileID)
	r.SetForm("unsent", "true")
	r.SetForm("guid", guid)
	r.SetForm("free_features", "[]")
	if version > 0 {
		r.SetForm("version", strconv.FormatInt(version, 10))
	}
	return c.tr.JSON(ctx, r, nil)
}

// MarkViewed reports that a specific message has been read.
func (c *Client) MarkViewed(ctx context.Context, peerProfileID, guid string, version int64) error {
	r := &core.Request{Method: http.MethodPost, Path: "/app/chat/message_viewed"}
	r.SetForm("target_id", peerProfileID)
	r.SetForm("guid", guid)
	if version > 0 {
		r.SetForm("version", strconv.FormatInt(version, 10))
	}
	return c.tr.JSON(ctx, r, nil)
}

// SendTyping publishes a typing indicator. Send-only: the receive side arrives
// over the realtime channel as class 208.
func (c *Client) SendTyping(ctx context.Context, peerProfileID string) error {
	r := &core.Request{Method: http.MethodPost, Path: "/app/chat/typing"}
	r.SetForm("target_id", peerProfileID)
	return c.tr.JSON(ctx, r, nil)
}

// --- media -----------------------------------------------------------------

// ResolveChatMedia mints a signed URL for media on a message.
//
// The URL is CloudFront-signed, expiring, and served from a rotating host. Use
// it immediately and re-resolve on failure rather than caching it.
func (c *Client) ResolveChatMedia(ctx context.Context, m Message, mediaKind int) (core.MediaRef, error) {
	q := url.Values{
		"guid":         {m.GUID},
		"message_type": {strconv.Itoa(m.MessageType)},
		"media_type":   {strconv.Itoa(mediaKind)},
		"sender_id":    {m.SenderID.String()},
		"recipient_id": {m.RecipientID.String()},
		"version":      {strconv.FormatInt(m.Version, 10)},
	}
	var out results[struct {
		ImageURL        string            `json:"image_url"`
		VideoURL        string            `json:"video_url"`
		ManifestURL     string            `json:"manifest_url"`
		ManifestCookies map[string]string `json:"manifest_cookies"`
	}]
	if err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet, Path: "/app/chat/media", Query: q,
	}, &out); err != nil {
		return core.MediaRef{}, err
	}

	ref := core.MediaRef{
		Width: m.FullsizeWidth, Height: m.FullsizeHeight, Expires: true,
		Cookies: out.Results.ManifestCookies,
	}
	switch {
	case mediaKind == ChatMediaVideo && out.Results.VideoURL != "":
		ref.URL = out.Results.VideoURL
	case mediaKind == ChatMediaVideo && out.Results.ManifestURL != "":
		// An HLS manifest, not a media file. Downloading it verbatim yields a
		// playlist; the segments need the accompanying cookies.
		ref.URL, ref.MimeType = out.Results.ManifestURL, "application/vnd.apple.mpegurl"
	default:
		ref.URL = out.Results.ImageURL
	}
	if ref.URL == "" {
		return core.MediaRef{}, core.ErrNoMedia
	}
	return ref, nil
}

// ResolveMedia implements core.MediaResolver for an absolute URL, which for
// SCRUFF means profile media that needs no signing.
func (c *Client) ResolveMedia(ctx context.Context, id string) (core.MediaRef, error) {
	if id == "" {
		return core.MediaRef{}, core.ErrNoMedia
	}
	if strings.HasPrefix(id, "http://") || strings.HasPrefix(id, "https://") {
		return core.MediaRef{URL: id}, nil
	}
	return core.MediaRef{URL: c.profileCDN() + "/" + id}, nil
}

// --- profiles --------------------------------------------------------------

// Profile fetches a member profile.
func (c *Client) Profile(ctx context.Context, profileID string) (*Profile, error) {
	lat, lon, _ := latLngStrings(c.cfg.loc())
	q := url.Values{
		"target":               {profileID},
		"latitude":             {lat},
		"longitude":            {lon},
		"thumbnail_constraint": {"-thumbnail"},
		"fullsize_constraint":  {"-fullsize"},
		"api_version":          {"2"},
	}
	// The response wraps a one-element array.
	var out results[[]Profile]
	if err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet, Path: "/app/profile", Query: q,
	}, &out); err != nil {
		return nil, err
	}
	if len(out.Results) == 0 {
		return nil, core.ErrNotFound
	}
	return &out.Results[0], nil
}

// --- grids (read-only) -----------------------------------------------------

// Grid names. Every one of these is the same request against a different path,
// returning the same envelope.
const (
	GridNearby         = "/app/location"
	GridWoofsReceived  = "/app/woofs/incoming"
	GridWoofsSent      = "/app/woofs/outgoing"
	GridViewedYou      = "/app/viewers/incoming"
	GridYouViewed      = "/app/viewers/outgoing"
	GridAlbumsReceived = "/app/albums/received"
	GridUnreadInbox    = "/app/inbox/unread"
	GridRecentInbox    = "/app/inbox/recent"
	GridMutualMatches  = "/app/grid/matches_mutual"
	GridFavorites      = "/app/favorite"
	GridBlocks         = "/app/block"
)

// Sort orders for a grid.
const (
	SortDistance        = 0
	SortTime            = 1
	SortOnline          = 2
	SortDistanceNewness = 3
)

// GridOptions controls a grid fetch.
type GridOptions struct {
	// Sort is one of the Sort* constants.
	Sort int
	// Offset is the paging cursor. Advance it by the response's BlockSize.
	Offset int
	// Limit is the page size; zero lets the server choose.
	Limit int
	// CacheID must be echoed from the previous page to keep results stable.
	CacheID string
	// Extra carries grid-specific parameters such as folder_id or album_id.
	Extra url.Values
}

// Grid fetches one page of a profile grid.
//
// Pass one of the Grid* constants as path. Advance by the response's BlockSize
// rather than by the limit you asked for, and echo CacheID on the next call.
func (c *Client) Grid(ctx context.Context, path string, opt GridOptions) (*GridPage, error) {
	lat, lon, provider := latLngStrings(c.cfg.loc())
	q := url.Values{
		"query_sort_type":   {strconv.Itoa(opt.Sort)},
		"offset":            {strconv.Itoa(opt.Offset)},
		"latitude":          {lat},
		"longitude":         {lon},
		"location_provider": {provider},
	}
	if opt.Limit > 0 {
		q.Set("limit", strconv.Itoa(opt.Limit))
	}
	if opt.CacheID != "" {
		q.Set("cache_id", opt.CacheID)
	}
	for k, vs := range opt.Extra {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	var out GridPage
	err := c.tr.JSON(ctx, &core.Request{Method: http.MethodGet, Path: path, Query: q}, &out)
	return &out, err
}

// Nearby is Grid for the nearby grid.
func (c *Client) Nearby(ctx context.Context, opt GridOptions) (*GridPage, error) {
	return c.Grid(ctx, GridNearby, opt)
}

// --- albums (read-only) ----------------------------------------------------

// Albums lists albums. Pass an empty targetProfileID for your own.
func (c *Client) Albums(ctx context.Context, targetProfileID string) ([]Album, error) {
	q := url.Values{}
	if targetProfileID != "" {
		q.Set("target_id", targetProfileID)
	}
	var out results[[]Album]
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet, Path: "/app/albums", Query: q,
	}, &out)
	return out.Results, err
}

// AlbumImages fetches an album's contents.
//
// The returned URLs are CloudFront-signed with a wildcard covering every
// rendition of each image, and they expire.
func (c *Client) AlbumImages(ctx context.Context, albumID string) (*AlbumContents, error) {
	q := url.Values{
		"album_id":                {albumID},
		"full_size_constraints[]": {"-fullsize"},
		"thumbnail_constraints[]": {"-thumbnail"},
	}
	var out AlbumContents
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet, Path: "/app/albums/images", Query: q,
	}, &out)
	return &out, err
}

// AlbumCover fetches a signed cover image URL.
func (c *Client) AlbumCover(ctx context.Context, albumID, albumVersion string) (string, error) {
	var out struct {
		SignedURL string `json:"signed_url"`
	}
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet, Path: "/app/albums/cover_image",
		Query: url.Values{"album_id": {albumID}, "album_version": {albumVersion}},
	}, &out)
	return out.SignedURL, err
}

// --- location --------------------------------------------------------------

// PublishLocation posts your current position.
//
// Distinct from the nearby grid, which is a GET on the same path.
func (c *Client) PublishLocation(ctx context.Context, loc LatLng) error {
	lat, lon, provider := latLngStrings(loc)
	r := &core.Request{Method: http.MethodPost, Path: "/app/location"}
	r.SetForm("latitude", lat)
	r.SetForm("longitude", lon)
	r.SetForm("speed", "0")
	r.SetForm("provider", provider)
	return c.tr.JSON(ctx, r, nil)
}
