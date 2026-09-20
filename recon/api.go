package recon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vladzaharia/scruff-recon-sdk/core"
)

// Image size codes. Only 100–104 exist.
//
// A URL such as /profiles/{id}/661 is a profile VERSION, not a size — a common
// misreading that produces requests for a rendition that does not exist.
const (
	ImageSizeThumbSmall  = 100 // fixed small thumbnail
	ImageSizeThumbMobile = 101
	ImageSizeThumb       = 102
	ImageSizeLargeMobile = 103
	ImageSizeLarge       = 104
)

// DefaultImageSizes is the small+large pair used for messaging content.
const DefaultImageSizes = "100,104"

// ProfileImageSizes is the pair used when resolving profiles.
const ProfileImageSizes = "102,104"

// DefaultPageSize is the message history page size the web client uses.
const DefaultPageSize = 20

// --- account ---------------------------------------------------------------

// AppSettings fetches the anonymous settings document. Unauthenticated.
//
// This is the authoritative source of every server limit. Prefer it over
// hardcoded values: the real caps (1000-character messages, 50 MiB uploads)
// differ from what is commonly assumed.
func AppSettingsAnonymous(ctx context.Context, opts ...Option) (*AppSettings, error) {
	t := unauthTransport(newConfig(opts...))
	var out AppSettings
	err := t.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/account/appSettings",
		Query:  url.Values{"deviceTypeId": {strconv.Itoa(DeviceTypeWeb)}},
	}, &out)
	return &out, err
}

// AccountAppSettings fetches the account-scoped settings document, which is the
// same schema personalised to the account.
func (c *Client) AccountAppSettings(ctx context.Context) (*AppSettings, error) {
	s := c.store.Get()
	var out AppSettings
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/account/accounts/" + s.AccountID + "/appSettings",
		Query:  url.Values{"deviceTypeId": {strconv.Itoa(DeviceTypeWeb)}},
	}, &out)
	return &out, err
}

// --- messaging -------------------------------------------------------------

// Conversations lists conversation threads.
//
// This endpoint is not paginated — one observed account returned all 873
// threads in a single response — so expect a large body.
func (c *Client) Conversations(ctx context.Context) ([]Conversation, error) {
	var out envelope[Conversation]
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   c.msgPath(""),
		Query:  url.Values{"isHidden": {"false"}},
	}, &out)
	return out.Data, err
}

// Conversation fetches one thread.
func (c *Client) Conversation(ctx context.Context, conversationID string) (*Conversation, error) {
	var out Conversation
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   c.msgPath("/" + conversationID),
	}, &out)
	return &out, err
}

// FindConversation looks up the 1:1 thread with a peer, returning nil when none
// exists yet. An empty result is not an error.
func (c *Client) FindConversation(ctx context.Context, peerProfileID string) (*Conversation, error) {
	me := c.ProfileID()
	var out envelope[Conversation]
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   c.msgPath(""),
		// The profile id appears both in the path and as a query parameter.
		Query: url.Values{"profileId": {me}, "otherProfileId": {peerProfileID}},
	}, &out)
	if err != nil {
		return nil, err
	}
	if len(out.Data) == 0 {
		return nil, nil
	}
	return &out.Data[0], nil
}

// CreateConversation starts a 1:1 thread.
//
// If one already exists the server answers with a conflict plus a Location
// header, which this follows automatically.
func (c *Client) CreateConversation(ctx context.Context, peerProfileID string) (*Conversation, error) {
	me := c.ProfileID()
	req := &core.Request{
		Method: http.MethodPost,
		Path:   c.msgPath(""),
		JSON: map[string]any{
			// PascalCase ProfileId is correct here — this and
			// LastMessageReadDate are the only two non-camelCase bodies in the
			// entire API.
			"participants": []map[string]any{
				{"ProfileId": peerProfileID},
				{"ProfileId": me},
			},
			"isGroup":    false,
			"isOfficial": false,
			"isArchived": false,
		},
	}

	body, resp, err := c.tr.Do(ctx, req)
	if err != nil {
		if resp != nil && (resp.StatusCode == http.StatusConflict ||
			(resp.StatusCode >= 300 && resp.StatusCode < 400)) {
			if loc := resp.Header.Get("Location"); loc != "" {
				return c.conversationByLocation(ctx, loc)
			}
		}
		return nil, err
	}
	var out Conversation
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("recon: decode conversation: %w", err)
	}
	return &out, nil
}

// conversationByLocation follows a Location header to an existing thread.
func (c *Client) conversationByLocation(ctx context.Context, location string) (*Conversation, error) {
	path := location
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		if strings.HasPrefix(path, "/api/") {
			path = strings.TrimPrefix(path, "/api")
		}
	}
	var out Conversation
	err := c.tr.JSON(ctx, &core.Request{Method: http.MethodGet, Path: path}, &out)
	return &out, err
}

// MessagesOptions controls a history fetch.
type MessagesOptions struct {
	// Limit is the page size. Zero uses DefaultPageSize.
	Limit int
	// Before pages backwards: pass the CreatedDate of the oldest message you
	// already hold. This is a timestamp cursor, not an id or an offset.
	Before string
	// ImageSizes selects attachment renditions. Empty uses DefaultImageSizes.
	ImageSizes string
}

// Messages fetches one page of history, newest first.
//
// Page backwards by passing the CreatedDate of the oldest message you hold as
// Before. An empty result means the start of the conversation.
func (c *Client) Messages(ctx context.Context, conversationID string, opt MessagesOptions) ([]Message, error) {
	if opt.Limit <= 0 {
		opt.Limit = DefaultPageSize
	}
	if opt.ImageSizes == "" {
		opt.ImageSizes = DefaultImageSizes
	}
	q := url.Values{
		"take":       {strconv.Itoa(opt.Limit)},
		"imageSizes": {opt.ImageSizes},
	}
	if opt.Before != "" {
		q.Set("before", opt.Before)
	}
	var out envelope[Message]
	// NB: out.TotalRecords here is the size of THIS page, not the conversation
	// total. It is deliberately not returned, to stop callers using it for
	// paging arithmetic.
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   c.msgPath("/" + conversationID + "/content"),
		Query:  q,
	}, &out)
	return out.Data, err
}

// AllMessages walks history backwards, calling yield for each message until the
// start of the conversation or until maxMessages have been yielded.
func (c *Client) AllMessages(ctx context.Context, conversationID string, maxMessages int, yield func(Message) error) error {
	page := func(ctx context.Context, cursor string) ([]Message, string, bool, error) {
		msgs, err := c.Messages(ctx, conversationID, MessagesOptions{Before: cursor})
		if err != nil {
			return nil, "", false, err
		}
		if len(msgs) == 0 {
			return nil, "", true, nil
		}
		// Results are newest-first, so the cursor is the last element.
		return msgs, msgs[len(msgs)-1].CreatedDate, false, nil
	}
	return core.Paginate(ctx, "", maxMessages, page, yield)
}

// Message fetches a single message, which is how a realtime notification is
// hydrated when it reports attachments.
func (c *Client) Message(ctx context.Context, conversationID, messageID string) (*Message, error) {
	var out Message
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   c.msgPath("/" + conversationID + "/messages/" + messageID),
		Query:  url.Values{"imageSizes": {DefaultImageSizes}},
	}, &out)
	return &out, err
}

// SendText sends a text message.
func (c *Client) SendText(ctx context.Context, conversationID, text string) (*Message, error) {
	var out Message
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPost,
		Path:   c.msgPath("/" + conversationID + "/messages"),
		// files and attachments are always present as empty arrays, never
		// omitted. There is no contentTypeId in the request.
		JSON: map[string]any{
			"text":        text,
			"files":       []string{},
			"attachments": []string{},
		},
	}, &out)
	return &out, err
}

// SendMedia sends a message with inline binary attachments.
//
// This is the path the web client uses for images and GIFs — bytes go inline as
// multipart on the send endpoint rather than being uploaded separately first.
func (c *Client) SendMedia(ctx context.Context, conversationID, text string, files []OutgoingFile) (*Message, error) {
	if len(files) == 0 {
		return c.SendText(ctx, conversationID, text)
	}
	req := &core.Request{
		Method: http.MethodPost,
		Path:   c.msgPath("/" + conversationID + "/messages"),
		Query:  url.Values{"imageSizes": {DefaultImageSizes}},
	}
	req.AddPart("text", text)
	// attachments is a JSON-encoded STRING array of pre-uploaded ids, always
	// written even when empty.
	req.AddPart("attachments", "[]")
	for _, f := range files {
		name := f.Name
		if name == "" {
			name = "image.jpg"
		}
		req.AddFilePart("files", name, f.MimeType, f.Data)
	}

	var out Message
	err := c.tr.JSON(ctx, req, &out)
	return &out, err
}

// MarkRead advances the read watermark for a conversation.
func (c *Client) MarkRead(ctx context.Context, conversationID string, readUpTo time.Time) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPost,
		Path:   c.msgPath("/" + conversationID + "/logMessagesRead"),
		// PascalCase key, unlike the rest of the API.
		JSON: map[string]any{"LastMessageReadDate": readUpTo.UTC().Format(time.RFC3339)},
	}, nil)
}

// DeleteConversation hides a whole thread. Recon has no per-message delete.
func (c *Client) DeleteConversation(ctx context.Context, conversationID string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodDelete,
		Path:   c.msgPath("/" + conversationID),
	}, nil)
}

// RecentAttachments lists recently sent attachments.
//
// URLs here point at the authenticated gateway rather than the public CDN.
func (c *Client) RecentAttachments(ctx context.Context) ([]Attachment, error) {
	var out envelope[Attachment]
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/messaging/profiles/" + c.ProfileID() + "/recentAttachments",
		Query:  url.Values{"imageSizes": {ProfileImageSizes}},
	}, &out)
	return out.Data, err
}

// --- profile ---------------------------------------------------------------

// Profile fetches a member profile.
//
// Requesting version 1 redirects to the current version, which the HTTP client
// follows, so this is the idiomatic "give me current".
func (c *Client) Profile(ctx context.Context, profileID string) (*Profile, error) {
	var out Profile
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/profile/profiles/" + profileID + "/1",
		Query:  url.Values{"imageSizes": {ProfileImageSizes}},
	}, &out)
	return &out, err
}

// LookupTable fetches one of the profile attribute tables: ethnicities,
// positions, bodytypes, bodyhairs, safesexes, roles, interests.
//
// Prefer this over hardcoding. The interest ids in particular are sparse —
// roughly a third of the range is retired — so assuming a contiguous range is
// wrong.
func (c *Client) LookupTable(ctx context.Context, table string) ([]LookupValue, error) {
	var out envelope[LookupValue]
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/profile/" + table,
	}, &out)
	return out.Data, err
}

// LookupValue is one row of a profile attribute table.
type LookupValue struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// ValidHeightsCm returns the accepted height values.
//
// Unlike the other tables this one's data is a bare integer array.
func (c *Client) ValidHeightsCm(ctx context.Context) ([]int, error) {
	var out envelope[int]
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/profile/validHeightsCm",
	}, &out)
	return out.Data, err
}

// --- discovery (read-only) -------------------------------------------------

// SearchOptions filters a profile search. Nil fields are omitted.
//
// Several filters are premium-only and are nulled client-side by the web client
// for free accounts; whether the server also enforces that is untested.
type SearchOptions struct {
	Name                string
	AgeMin, AgeMax      int
	HeightCmMin         int
	HeightCmMax         int
	Latitude, Longitude float64
	RadiusMetres        int
	ActiveWithinMinutes int
	JoinedWithinDays    int
	IsExplore           bool
	PositionIDs         []int
	RoleIDs             []int
	InterestIDs         []int
	SafeSexIDs          []int
	BodyTypeIDs         []int
	BodyHairIDs         []int
	EthnicityIDs        []int
}

// Search runs a profile search.
//
// Two things to know: the endpoint is NOT paginated — it returns the entire
// result set, and the web client slices locally — and the results are stubs. To
// render anything you must fetch each result's profile individually.
func (c *Client) Search(ctx context.Context, opt SearchOptions) ([]SearchResult, error) {
	q := url.Values{
		"myProfileId":  {c.ProfileID()},
		"sortProperty": {"distance"}, // the only value the client ever sends
	}
	setInt := func(k string, v int) {
		if v != 0 {
			q.Set(k, strconv.Itoa(v))
		}
	}
	if opt.Name != "" {
		q.Set("name", opt.Name)
	}
	setInt("ageMin", opt.AgeMin)
	setInt("ageMax", opt.AgeMax)
	setInt("heightCmMin", opt.HeightCmMin)
	setInt("heightCmMax", opt.HeightCmMax)
	setInt("radiusMetres", opt.RadiusMetres)
	setInt("activeWithinMinutes", opt.ActiveWithinMinutes)
	setInt("joinedWithinDays", opt.JoinedWithinDays)
	if opt.Latitude != 0 || opt.Longitude != 0 {
		q.Set("latitude", strconv.FormatFloat(opt.Latitude, 'f', -1, 64))
		q.Set("longitude", strconv.FormatFloat(opt.Longitude, 'f', -1, 64))
	}
	if opt.IsExplore {
		q.Set("isExplore", "true")
	}
	setCSV := func(k string, v []int) {
		if len(v) > 0 {
			q.Set(k, joinInts(v))
		}
	}
	setCSV("positionIds", opt.PositionIDs)
	setCSV("roleIds", opt.RoleIDs)
	setCSV("interestIds", opt.InterestIDs)
	setCSV("safeSexIds", opt.SafeSexIDs)
	setCSV("bodyTypeIds", opt.BodyTypeIDs)
	setCSV("bodyHairIds", opt.BodyHairIDs)
	setCSV("ethnicityIds", opt.EthnicityIDs)

	var out envelope[SearchResult]
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/profileSearch/profiles",
		Query:  q,
	}, &out)
	return out.Data, err
}

// Blocked lists blocked profiles. Unlike the cruise and visit lists, entries
// here carry a display name.
func (c *Client) Blocked(ctx context.Context) ([]RelationEntry, error) {
	return c.relationList(ctx, "blocked")
}

// Cruised lists profiles you have cruised.
func (c *Client) Cruised(ctx context.Context) ([]RelationEntry, error) {
	return c.relationList(ctx, "cruisedProfiles")
}

// CruisedBy lists profiles that have cruised you.
func (c *Client) CruisedBy(ctx context.Context) ([]RelationEntry, error) {
	return c.relationList(ctx, "cruisedByProfiles")
}

// Visitors lists profiles that have viewed yours.
func (c *Client) Visitors(ctx context.Context) ([]RelationEntry, error) {
	return c.relationList(ctx, "visitorProfiles")
}

func (c *Client) relationList(ctx context.Context, kind string) ([]RelationEntry, error) {
	var out envelope[RelationEntry]
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/profileRelations/profiles/" + c.ProfileID() + "/" + kind,
	}, &out)
	return out.Data, err
}

// CruisedByCount returns cruise counters.
func (c *Client) CruisedByCount(ctx context.Context) (*RelationCounts, error) {
	return c.relationCount(ctx, "cruisedByProfiles")
}

// VisitorCount returns visitor counters.
func (c *Client) VisitorCount(ctx context.Context) (*RelationCounts, error) {
	return c.relationCount(ctx, "visitorProfiles")
}

func (c *Client) relationCount(ctx context.Context, kind string) (*RelationCounts, error) {
	var out RelationCounts
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/profileRelations/profiles/" + c.ProfileID() + "/" + kind + "/count",
	}, &out)
	return &out, err
}

// Notifications lists feed notifications.
func (c *Client) Notifications(ctx context.Context) ([]FeedItem, error) {
	var out envelope[FeedItem]
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/feed/profiles/" + c.ProfileID() + "/feeds/notifications",
		Query:  url.Values{"noCache": {"true"}},
	}, &out)
	return out.Data, err
}

// Galleries lists a profile's media albums. Pass an empty profileID for your
// own.
func (c *Client) Galleries(ctx context.Context, profileID string) ([]Gallery, error) {
	if profileID == "" {
		profileID = c.ProfileID()
	}
	q := url.Values{
		"imageSizes":   {ProfileImageSizes},
		"sortProperty": {"2"}, // upload date, newest first
		"deviceTypeId": {strconv.Itoa(DeviceTypeWeb)},
	}
	if profileID == c.ProfileID() {
		q.Set("noCache", "true")
	}
	var out envelope[Gallery]
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/media/profiles/" + profileID + "/galleries",
		Query:  q,
	}, &out)
	return out.Data, err
}

// --- helpers ---------------------------------------------------------------

// msgPath builds a messaging path rooted at the active profile.
func (c *Client) msgPath(suffix string) string {
	return "/messaging/profiles/" + c.ProfileID() + "/conversations" + suffix
}

func joinInts(v []int) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}
