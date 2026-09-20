package recon

// The API surface beyond messaging and discovery: preferences, profile
// editing, saved filters, the social graph, the feed, sponsored content,
// location, membership and moderation.
//
// Provenance: [client] throughout — these were read from Recon's own web
// bundle, not observed on the wire. Paths, parameters and body shapes are
// authoritative; what is missing is confirmation that the server behaves as the
// client expects. None of the write paths here has been exercised live. Treat
// a surprising response as new information about the API, not as a bug here.

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/vladzaharia/scruff-recon-sdk/core"
)

// --- account preferences ---------------------------------------------------

// AccountPreferences fetches the account's display preferences.
func (c *Client) AccountPreferences(ctx context.Context) (*AccountPreferences, error) {
	s := c.store.Get()
	var out AccountPreferences
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/account/accounts/" + s.AccountID + "/preferences",
		Query:  url.Values{"deviceTypeId": {strconv.Itoa(DeviceTypeWeb)}},
	}, &out)
	return &out, err
}

// SetAccountPreferences replaces the account's display preferences.
//
// This is a full replace, not a merge: fetch with AccountPreferences, modify,
// and send the whole struct back.
func (c *Client) SetAccountPreferences(ctx context.Context, p AccountPreferences) error {
	s := c.store.Get()
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPut,
		Path:   "/account/accounts/" + s.AccountID + "/preferences",
		JSON:   p,
	}, nil)
}

// MessagingPreferences fetches message notification settings.
func (c *Client) MessagingPreferences(ctx context.Context) (*MessagingPreferences, error) {
	var out MessagingPreferences
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/messaging/profiles/" + c.ProfileID() + "/preferences",
	}, &out)
	return &out, err
}

// SetMessagingPreferences replaces message notification settings.
func (c *Client) SetMessagingPreferences(ctx context.Context, p MessagingPreferences) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPut,
		Path:   "/messaging/profiles/" + c.ProfileID() + "/preferences",
		JSON:   p,
	}, nil)
}

// Contacts lists the messaging contact set.
func (c *Client) Contacts(ctx context.Context) ([]RelationEntry, error) {
	var out envelope[RelationEntry]
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/messaging/profiles/" + c.ProfileID() + "/contacts",
	}, &out)
	return out.Data, err
}

// DeleteAttachment removes an attachment from a conversation.
func (c *Client) DeleteAttachment(ctx context.Context, conversationID, attachmentID string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodDelete,
		Path:   c.msgPath("/" + conversationID + "/attachments/" + attachmentID),
	}, nil)
}

// --- profile editing -------------------------------------------------------

// ProfileDetail fetches the long bio for a profile version.
//
// It is a separate document from Profile because it is cached and invalidated
// independently; Profile.LongText is always null over the wire.
func (c *Client) ProfileDetail(ctx context.Context, profileID string, version int) (*ProfileDetail, error) {
	var out ProfileDetail
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/profile/profiles/" + profileID + "/detail/" + strconv.Itoa(version),
	}, &out)
	return &out, err
}

// OfficialProfile fetches a system or blog persona.
//
// These differ from member profiles in ways that break naive handling:
// membershipLevelId and age are 0, detailUrl is null, and lastUpdatedDate is
// the zero time.
func (c *Client) OfficialProfile(ctx context.Context, profileID string) (*Profile, error) {
	var out Profile
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/profile/officialProfiles/" + profileID,
	}, &out)
	return &out, err
}

// ProfileByName resolves a display name to a profile id.
func (c *Client) ProfileByName(ctx context.Context, profileName string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/profileSearch/profiles/name/" + url.PathEscape(profileName),
	}, &out)
	return out.ID, err
}

// PatchProfile applies RFC 6902 JSON Patch operations to a profile.
//
// Always include a replace of /rowVersion with the value from the profile you
// read: the server uses it for optimistic concurrency, and omitting it is how
// concurrent edits silently clobber each other. PatchRowVersion builds that op.
//
// Note /safeSexId is readable but is absent from every patch set the official
// client sends, so its editability is unconfirmed.
func (c *Client) PatchProfile(ctx context.Context, profileID string, ops []PatchOp) (*Profile, error) {
	var out Profile
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPatch,
		Path:   "/profile/profiles/" + profileID,
		JSON:   ops,
	}, &out)
	return &out, err
}

// PatchRowVersion returns the optimistic-concurrency op that every patch set
// should carry.
func PatchRowVersion(rowVersion any) PatchOp {
	return PatchOp{Op: "replace", Path: "/rowVersion", Value: rowVersion}
}

// Replace returns a "replace" patch operation, the only op the official client
// ever sends.
func Replace(path string, value any) PatchOp {
	return PatchOp{Op: "replace", Path: path, Value: value}
}

// UpdateProfile replaces a profile wholesale.
//
// Prefer PatchProfile: a full replace sends every field, so any field you did
// not populate is sent as its zero value.
func (c *Client) UpdateProfile(ctx context.Context, profileID string, p Profile) (*Profile, error) {
	var out Profile
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPut,
		Path:   "/profile/profiles/" + profileID,
		JSON:   p,
	}, &out)
	return &out, err
}

// --- saved search filters --------------------------------------------------

// SearchFilters fetches the saved default filter set.
func (c *Client) SearchFilters(ctx context.Context) (*SearchFilters, error) {
	var out SearchFilters
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/profileSearch/profiles/" + c.ProfileID() + "/defaultFilters",
	}, &out)
	return &out, err
}

// SetSearchFilters saves the default filter set.
//
// Leave a field nil to mean "no filter"; that is distinct from a zero value,
// which is why SearchFilters uses pointers for every scalar.
func (c *Client) SetSearchFilters(ctx context.Context, f SearchFilters) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPut,
		Path:   "/profileSearch/profiles/" + c.ProfileID() + "/defaultFilters",
		JSON:   f,
	}, nil)
}

// DeleteSearchFilters clears the saved default filter set.
func (c *Client) DeleteSearchFilters(ctx context.Context) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodDelete,
		Path:   "/profileSearch/profiles/" + c.ProfileID() + "/defaultFilters",
	}, nil)
}

// --- social graph ----------------------------------------------------------

// Friends lists a profile's friends, split by whether the relation is mutual.
//
// Pass an empty profileID for your own. Note the naming asymmetry in this API:
// friends, followers and followings (plural) live on profileSearch, while
// followers/count and following/count (singular) live on profileRelations.
func (c *Client) Friends(ctx context.Context, profileID string) (*SocialList, error) {
	return c.socialList(ctx, profileID, "friends")
}

// Followers lists a profile's followers.
func (c *Client) Followers(ctx context.Context, profileID string) (*SocialList, error) {
	return c.socialList(ctx, profileID, "followers")
}

// Followings lists the profiles a profile follows.
func (c *Client) Followings(ctx context.Context, profileID string) (*SocialList, error) {
	return c.socialList(ctx, profileID, "followings")
}

func (c *Client) socialList(ctx context.Context, profileID, kind string) (*SocialList, error) {
	me := c.ProfileID()
	if profileID == "" {
		profileID = me
	}
	q := url.Values{"myProfileId": {me}}
	if profileID == me {
		// The official client bypasses the cache when reading its own lists,
		// since it has just changed them.
		q.Set("noCache", "true")
	}
	var out struct {
		Data SocialList `json:"data"`
	}
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/profileSearch/profiles/" + profileID + "/" + kind,
		Query:  q,
	}, &out)
	return &out.Data, err
}

// FriendCounts returns mutual and non-mutual friend counts.
func (c *Client) FriendCounts(ctx context.Context, profileID string) (*SocialCounts, error) {
	if profileID == "" {
		profileID = c.ProfileID()
	}
	var out SocialCounts
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/profileSearch/profiles/" + profileID + "/friends/count",
		Query:  url.Values{"myProfileId": {c.ProfileID()}},
	}, &out)
	return &out, err
}

// --- feed ------------------------------------------------------------------

// HomeFeed fetches the activity feed, as distinct from Notifications.
func (c *Client) HomeFeed(ctx context.Context) ([]FeedItem, error) {
	var out envelope[FeedItem]
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/feed/profiles/" + c.ProfileID() + "/feeds/home",
	}, &out)
	return out.Data, err
}

// Feed fetches an arbitrary named feed.
//
// The feed name is a path segment, so feeds beyond "notifications" and "home"
// may exist; this is the escape hatch for finding them.
func (c *Client) Feed(ctx context.Context, name string) ([]FeedItem, error) {
	var out envelope[FeedItem]
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/feed/profiles/" + c.ProfileID() + "/feeds/" + url.PathEscape(name),
	}, &out)
	return out.Data, err
}

// FeedItem dereferences one feed entry, which carries the display strings the
// stub omits.
func (c *Client) FeedItem(ctx context.Context, feedItemID string) (*FeedItem, error) {
	var out FeedItem
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/feed/feedItems/" + feedItemID,
	}, &out)
	return &out, err
}

// MarkNotificationsViewed marks the notification feed read up to a timestamp.
func (c *Client) MarkNotificationsViewed(ctx context.Context, viewedDate string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPost,
		Path:   "/feed/profiles/" + c.ProfileID() + "/logNotificationsViewed",
		JSON:   map[string]any{"viewedDate": viewedDate},
	}, nil)
}

// --- dvrt: sponsored content and broadcast messages ------------------------

// BulkMessages lists broadcast and sponsored messages.
//
// These drive the "official" threads that appear in the inbox alongside real
// conversations, and they come from a different service than messaging does.
func (c *Client) BulkMessages(ctx context.Context) ([]BulkMessage, error) {
	var out envelope[BulkMessage]
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/dvrt/profiles/" + c.ProfileID() + "/bulkMessages",
		Query:  url.Values{"deviceTypeId": {strconv.Itoa(DeviceTypeWeb)}},
	}, &out)
	return out.Data, err
}

// MarkBulkMessageRead marks one broadcast message read.
func (c *Client) MarkBulkMessageRead(ctx context.Context, id string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPost,
		Path:   "/dvrt/profiles/" + c.ProfileID() + "/bulkMessages/" + id + "/read",
		JSON:   map[string]any{},
	}, nil)
}

// DeleteBulkMessage removes one broadcast message.
func (c *Client) DeleteBulkMessage(ctx context.Context, id string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodDelete,
		Path:   "/dvrt/profiles/" + c.ProfileID() + "/bulkMessages/" + id + "/delete",
	}, nil)
}

// Advertiser resolves the sender of a non-official BulkMessage.
//
// Only call this when BulkMessage.IsOfficial is false; when it is true the
// SenderURL points at /profile/officialProfiles/{id} instead, which
// OfficialProfile handles.
func (c *Client) Advertiser(ctx context.Context, dvrtsrID string) (*Advertiser, error) {
	var out Advertiser
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/dvrt/dvrtsrs/" + dvrtsrID,
	}, &out)
	return &out, err
}

// --- location --------------------------------------------------------------

// Location resolves a location id to display names.
func (c *Client) Location(ctx context.Context, locationID string) (*Location, error) {
	var out Location
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/location/locations/" + locationID,
	}, &out)
	return &out, err
}

// Distance returns the distance in metres between two location ids.
//
// The ids must be sorted lower-first; the endpoint is not symmetric in its
// path, so this method sorts them for you.
func (c *Client) Distance(ctx context.Context, locationA, locationB string) (int, error) {
	lo, hi := locationA, locationB
	if numericLess(hi, lo) {
		lo, hi = hi, lo
	}
	var out struct {
		DistanceMetres int `json:"distanceMetres"`
	}
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/location/granularLocations/" + lo + "/distances/" + hi,
	}, &out)
	return out.DistanceMetres, err
}

// numericLess compares two numeric-looking id strings by value, falling back to
// lexical order when either is not a number.
func numericLess(a, b string) bool {
	ai, aerr := strconv.Atoi(a)
	bi, berr := strconv.Atoi(b)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	return a < b
}

// PublishLocation reports the account's position.
//
// Cadence expected by the server, from appSettings.geolocation: no more often
// than every 2 minutes, at least every 15 minutes, and on movement over 99 m.
func (c *Client) PublishLocation(ctx context.Context, g GeoLocation) error {
	if g.ProfileID == "" {
		g.ProfileID = c.ProfileID()
	}
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPut,
		Path:   "/location/profiles/" + g.ProfileID + "/geolocation",
		JSON:   g,
	}, nil)
}

// --- membership ------------------------------------------------------------

// MembershipStatus fetches the subscription state for a profile.
func (c *Client) MembershipStatus(ctx context.Context, profileID string) (*MembershipStatus, error) {
	if profileID == "" {
		profileID = c.ProfileID()
	}
	var out MembershipStatus
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/membership/profiles/" + profileID + "/membershipStatus",
	}, &out)
	return &out, err
}

// --- moderation ------------------------------------------------------------

// ReportCategories lists the reasons a report can be filed under.
//
// Fetch these rather than hardcoding ids: only 13 ("Other") is fixed in the
// official client. Note the endpoint's trailing slash, which is required.
func (c *Client) ReportCategories(ctx context.Context) ([]ReportCategory, error) {
	var out envelope[ReportCategory]
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/contentReview/reportCategories/",
	}, &out)
	return out.Data, err
}

// ReportProfile files a content report against another member.
//
// This affects another person's account and is not reversible from the client.
// Call it in response to a deliberate human decision, never automatically.
func (c *Client) ReportProfile(ctx context.Context, profileID string, r Report) error {
	if r.ReportedByAccountID == "" {
		r.ReportedByAccountID = c.store.Get().AccountID
	}
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPost,
		Path:   "/contentReview/profiles/" + profileID + "/report",
		JSON:   r,
	}, nil)
}

// CheckProfileName asks the anti-abuse service whether a display name is
// acceptable. The client-side rule is ^[a-zA-Z0-9]{4,20}$; this is the
// server's view, which is stricter.
//
// The endpoint is unauthenticated, so it is also available before login via
// CheckProfileNameAnonymous.
func (c *Client) CheckProfileName(ctx context.Context, name string) (*NameCheck, error) {
	var out NameCheck
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPost,
		Path:   "/antiAbuse/profileNameChecks",
		JSON:   map[string]any{"profileName": name, "profileId": nil},
	}, &out)
	return &out, err
}

// CheckProfileNameAnonymous is CheckProfileName without a session.
func CheckProfileNameAnonymous(ctx context.Context, name string, opts ...Option) (*NameCheck, error) {
	t := unauthTransport(newConfig(opts...))
	var out NameCheck
	err := t.JSON(ctx, &core.Request{
		Method: http.MethodPost,
		Path:   "/antiAbuse/profileNameChecks",
		JSON:   map[string]any{"profileName": name, "profileId": nil},
	}, &out)
	return &out, err
}
