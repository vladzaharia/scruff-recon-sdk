package recon

// Recon's social primitives — block, cruise (a like), visit (a profile view),
// friend (mutual, request-based) and follow (one-way) — plus media galleries
// and events.
//
// Provenance: [client] throughout, read from Recon's own web bundle. The read
// paths for blocks, cruises and counts are [observed]; the writes are not. None
// of the writes here has been exercised against the live API.
//
// Several of these are visible to the other person and are not undoable from
// the client. Cruising, friend-requesting and following all notify. Call them
// in response to a deliberate human action, and do not loop over a profile
// list: that is exactly the bulk behaviour this SDK is meant not to enable.

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/vladzaharia/scruff-recon-sdk/core"
)

// relPath builds a profileRelations path rooted at the caller's profile.
func (c *Client) relPath(suffix string) string {
	return "/profileRelations/profiles/" + c.ProfileID() + suffix
}

// --- blocks ----------------------------------------------------------------

// Block blocks a profile.
//
// Capped by appSettings.limits.blocks (50 on the observed account). This is
// the only relations list that embeds profileName, so Blocked() needs no
// dereferencing.
func (c *Client) Block(ctx context.Context, profileID string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPut,
		Path:   c.relPath("/blocks/" + profileID),
		JSON:   map[string]any{"profileId": profileID},
	}, nil)
}

// Unblock removes a block.
func (c *Client) Unblock(ctx context.Context, profileID string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodDelete,
		Path:   c.relPath("/blocks/" + profileID),
	}, nil)
}

// --- cruises ---------------------------------------------------------------

// Cruise marks a profile as liked. This is visible to them and notifies.
//
// Re-cruising the same profile is throttled server-side by
// appSettings.limits.cruiseMinUpdatePeriodDays (7). The rejection arrives as a
// normal error whose title contains a {cruiseMinUpdatePeriod} token — treat it
// as "too soon", not as a transport failure, and do not retry it.
func (c *Client) Cruise(ctx context.Context, profileID string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPut,
		Path:   c.relPath("/cruisedProfiles/" + profileID),
		JSON:   nil,
	}, nil)
}

// Uncruise withdraws a cruise.
func (c *Client) Uncruise(ctx context.Context, profileID string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodDelete,
		Path:   c.relPath("/cruisedProfiles/" + profileID),
	}, nil)
}

// MarkCruisedByViewed moves the "seen" watermark on the cruised-by list.
//
// Pass the mostRecentDate from the list response rather than time.Now(): that
// is what the official client does, and it avoids marking entries seen that
// arrived between your read and this call.
func (c *Client) MarkCruisedByViewed(ctx context.Context, viewedDate string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPost,
		Path:   c.relPath("/logCruisedByViewed"),
		JSON:   map[string]any{"viewedDate": viewedDate},
	}, nil)
}

// --- visits ----------------------------------------------------------------

// Visit records that you viewed a profile.
//
// Recording a visit is what makes you appear in their visitors list. It is
// suppressed server-side when your own RelationPreferences.ShowVisits is false,
// which is what "stealth mode" means. Fetching a profile with Profile() does
// NOT record a visit; this call is the explicit act.
func (c *Client) Visit(ctx context.Context, profileID string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPut,
		Path:   c.relPath("/visitedProfiles/" + profileID),
		JSON:   nil,
	}, nil)
}

// MarkVisitorsViewed moves the "seen" watermark on the visitors list.
func (c *Client) MarkVisitorsViewed(ctx context.Context, viewedDate string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPost,
		Path:   c.relPath("/logVisitorsViewed"),
		JSON:   map[string]any{"viewedDate": viewedDate},
	}, nil)
}

// ListViewStats returns the watermarks used to highlight unseen cruise and
// visitor entries.
func (c *Client) ListViewStats(ctx context.Context) (*ListViewStats, error) {
	var out ListViewStats
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   c.relPath("/listViewStats"),
	}, &out)
	return &out, err
}

// --- friends ---------------------------------------------------------------

// SendFriendRequest asks another member to be friends. This notifies them.
//
// Capped by appSettings.limits.friends (500). Error code 103000002 means you
// are at your limit, 103000009 means they are at theirs — the second is not
// something the user can fix, so surface it differently.
func (c *Client) SendFriendRequest(ctx context.Context, profileID string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPut,
		Path:   c.relPath("/friendRequests/" + profileID),
		JSON:   nil,
	}, nil)
}

// CancelFriendRequest withdraws a request you sent.
func (c *Client) CancelFriendRequest(ctx context.Context, profileID string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodDelete,
		Path:   c.relPath("/friendRequests/" + profileID),
	}, nil)
}

// FriendRequests lists pending requests in both directions. Compare
// RequestingProfileID against your own id to tell them apart.
func (c *Client) FriendRequests(ctx context.Context) ([]FriendRequest, error) {
	var out struct {
		Total int             `json:"total"`
		Data  []FriendRequest `json:"data"`
	}
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   c.relPath("/friendRequests"),
	}, &out)
	return out.Data, err
}

// AcceptFriendRequest accepts a request from another member.
func (c *Client) AcceptFriendRequest(ctx context.Context, profileID string) error {
	return c.friendRequestResponse(ctx, profileID, "accept")
}

// RejectFriendRequest declines a request from another member.
func (c *Client) RejectFriendRequest(ctx context.Context, profileID string) error {
	return c.friendRequestResponse(ctx, profileID, "reject")
}

func (c *Client) friendRequestResponse(ctx context.Context, profileID, verb string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPost,
		Path:   c.relPath("/friendRequestResponse/" + profileID + "/" + verb),
		JSON:   nil,
	}, nil)
}

// RemoveFriend ends an existing friendship.
func (c *Client) RemoveFriend(ctx context.Context, profileID string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodDelete,
		Path:   c.relPath("/friends/" + profileID),
	}, nil)
}

// --- followers -------------------------------------------------------------

// Follow follows a profile. One-way and needs no approval, unlike friending.
//
// Capped by appSettings.limits.following (500).
func (c *Client) Follow(ctx context.Context, profileID string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPut,
		Path:   c.relPath("/followers/" + profileID),
		JSON:   nil,
	}, nil)
}

// Unfollow stops following a profile.
//
// The same endpoint serves double duty in the official client: it both
// unfollows someone you follow and removes a follower of yours. Which it does
// depends on the relationship, not on the call.
func (c *Client) Unfollow(ctx context.Context, profileID string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodDelete,
		Path:   c.relPath("/followers/" + profileID),
	}, nil)
}

// FollowerCount counts a profile's followers.
//
// Note the singular/plural asymmetry in this API: followers/count and
// following/count (singular "following") live on profileRelations, while the
// followers and followings *lists* live on profileSearch.
func (c *Client) FollowerCount(ctx context.Context, profileID string) (int, error) {
	return c.followCount(ctx, profileID, "followers")
}

// FollowingCount counts the profiles a profile follows.
func (c *Client) FollowingCount(ctx context.Context, profileID string) (int, error) {
	return c.followCount(ctx, profileID, "following")
}

func (c *Client) followCount(ctx context.Context, profileID, kind string) (int, error) {
	if profileID == "" {
		profileID = c.ProfileID()
	}
	var out struct {
		Count int `json:"count"`
	}
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/profileRelations/profiles/" + profileID + "/" + kind + "/count",
	}, &out)
	return out.Count, err
}

// --- relation preferences --------------------------------------------------

// RelationPreferences fetches social notification settings and stealth mode.
func (c *Client) RelationPreferences(ctx context.Context) (*RelationPreferences, error) {
	var out RelationPreferences
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   c.relPath("/preferences"),
	}, &out)
	return &out, err
}

// SetRelationPreferences replaces social notification settings.
func (c *Client) SetRelationPreferences(ctx context.Context, p RelationPreferences) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPut,
		Path:   c.relPath("/preferences"),
		JSON:   p,
	}, nil)
}

// PatchRelationPreferences changes individual preference fields.
//
// SetStealthMode is the common case and is easier to get right.
func (c *Client) PatchRelationPreferences(ctx context.Context, ops []PatchOp) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPatch,
		Path:   c.relPath("/preferences"),
		JSON:   ops,
	}, nil)
}

// SetStealthMode turns visit recording on or off.
//
// Stealth on means showVisits false: your views stop appearing in other
// people's visitor lists.
func (c *Client) SetStealthMode(ctx context.Context, on bool) error {
	return c.PatchRelationPreferences(ctx, []PatchOp{Replace("/showVisits", !on)})
}

// --- legacy favourites -----------------------------------------------------

// CanImportV2Favourites reports whether legacy favourites are available to
// import.
func (c *Client) CanImportV2Favourites(ctx context.Context) (bool, error) {
	var out struct {
		Value bool `json:"value"`
	}
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/profileRelations/v2Favourites/" + c.ProfileID() + "/canImport",
	}, &out)
	return out.Value, err
}

// ImportV2Favourites imports legacy favourites as cruises. Not reversible in
// bulk — check CanImportV2Favourites first.
func (c *Client) ImportV2Favourites(ctx context.Context) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPost,
		Path:   "/profileRelations/v2Favourites/" + c.ProfileID() + "/import",
	}, nil)
}

// --- media galleries -------------------------------------------------------

// Gallery fetches one gallery.
func (c *Client) Gallery(ctx context.Context, profileID, galleryID string) (*Gallery, error) {
	var out Gallery
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/media/profiles/" + profileID + "/galleries/" + galleryID,
		Query:  c.mediaQuery(profileID, url.Values{"imageSizes": {"100"}}),
	}, &out)
	return &out, err
}

// GalleryFiles lists the images in a gallery.
func (c *Client) GalleryFiles(ctx context.Context, profileID, galleryID string) ([]MediaFileProperties, error) {
	var out envelope[MediaFileProperties]
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/media/profiles/" + profileID + "/galleries/" + galleryID + "/fileProperties",
		Query:  c.mediaQuery(profileID, url.Values{"imageSizes": {"100,102"}, "sortProperty": {"2"}}),
	}, &out)
	return out.Data, err
}

// ProfileFiles lists every image on a profile, across galleries.
func (c *Client) ProfileFiles(ctx context.Context, profileID string) ([]MediaFileProperties, error) {
	var out envelope[MediaFileProperties]
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/media/profiles/" + profileID + "/fileProperties",
		Query:  c.mediaQuery(profileID, url.Values{"imageSizes": {"100,102"}, "sortProperty": {"2"}}),
	}, &out)
	return out.Data, err
}

// mediaQuery adds deviceTypeId, and noCache when reading your own media (which
// you may have just changed).
func (c *Client) mediaQuery(profileID string, q url.Values) url.Values {
	q.Set("deviceTypeId", strconv.Itoa(DeviceTypeWeb))
	if profileID == c.ProfileID() {
		q.Set("noCache", "true")
	}
	return q
}

// UploadPhoto uploads an image to a gallery.
//
// Server limits from appSettings.limits: 50 MiB per file, 500 files per
// profile, 5 in the main gallery, 50 per batch, minimum 400 px, and extensions
// .bmp .gif .jpg .jpeg .png. Read them from AccountAppSettings rather than
// hardcoding — they differ from what is commonly assumed.
func (c *Client) UploadPhoto(ctx context.Context, galleryID string, galleryTypeID int, f OutgoingFile) (*MediaFileProperties, error) {
	r := &core.Request{
		Method: http.MethodPost,
		Path:   "/media/profiles/" + c.ProfileID() + "/files",
	}
	name := f.Name
	if name == "" {
		name = "photo.jpg"
	}
	r.AddFilePart("file", name, f.MimeType, f.Data)
	if galleryID != "" {
		r.AddPart("galleryId", galleryID)
	}
	r.AddPart("galleryTypeId", strconv.Itoa(galleryTypeID))

	var out MediaFileProperties
	err := c.tr.JSON(ctx, r, &out)
	return &out, err
}

// DeletePhotos removes images by id.
func (c *Client) DeletePhotos(ctx context.Context, fileIDs []string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPost,
		Path:   "/media/profiles/" + c.ProfileID() + "/files/delete",
		JSON:   fileIDs,
	}, nil)
}

// DeleteGalleryPhoto removes one image from one gallery.
func (c *Client) DeleteGalleryPhoto(ctx context.Context, galleryID, fileID string) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodDelete,
		Path:   "/media/profiles/" + c.ProfileID() + "/galleries/" + galleryID + "/files/" + fileID,
	}, nil)
}

// SetPhotoPosition reorders an image within a gallery.
//
// Position 1 in the primary gallery is what "set as main photo" means; there
// is no separate endpoint for it.
func (c *Client) SetPhotoPosition(ctx context.Context, galleryID, fileID string, position int) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPut,
		Path:   "/media/profiles/" + c.ProfileID() + "/galleries/" + galleryID + "/files/" + fileID + "/position",
		JSON:   map[string]any{"value": position},
	}, nil)
}

// --- events ----------------------------------------------------------------

// EventsOptions pages the event list.
type EventsOptions struct {
	Skip   int
	Take   int
	IsPast bool
}

// Events lists upcoming events, or past ones when IsPast is set.
//
// The official client pages these 24 at a time.
func (c *Client) Events(ctx context.Context, opt EventsOptions) ([]Event, error) {
	q := url.Values{}
	// The client appends skip/take only when take is non-zero; an unpaged
	// request returns the server's own default page.
	if opt.Take > 0 {
		q.Set("skip", strconv.Itoa(opt.Skip))
		q.Set("take", strconv.Itoa(opt.Take))
	}
	if opt.IsPast {
		q.Set("isPast", "true")
	}
	var out envelope[Event]
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/event/events",
		Query:  q,
	}, &out)
	return out.Data, err
}

// Event fetches one event.
func (c *Client) Event(ctx context.Context, eventID string) (*Event, error) {
	var out Event
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/event/events/" + eventID,
	}, &out)
	return &out, err
}

// AttendanceStatus reads your RSVP for an event.
//
// The meaning of each statusId is not resolvable from the client bundle — the
// values going/interested/not-going are not enumerated anywhere in it — so this
// returns the raw id rather than inventing names for them.
func (c *Client) AttendanceStatus(ctx context.Context, eventID string) (int, error) {
	var out struct {
		StatusID int `json:"statusId"`
	}
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/event/events/" + eventID + "/attendees/" + c.ProfileID(),
	}, &out)
	return out.StatusID, err
}

// SetAttendanceStatus RSVPs to an event. This may be visible to other
// attendees depending on the event's showAttendance flag.
//
// See AttendanceStatus for why statusID is an untyped int.
func (c *Client) SetAttendanceStatus(ctx context.Context, eventID string, statusID int) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPut,
		Path:   "/event/events/" + eventID + "/attendees/" + c.ProfileID(),
		JSON:   map[string]any{"statusId": statusID},
	}, nil)
}

// EventAttendeeCount counts RSVPs for an event.
func (c *Client) EventAttendeeCount(ctx context.Context, eventID string) (int, error) {
	var out struct {
		Value int `json:"value"`
	}
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   fmt.Sprintf("/profileSearch/events/%s/attendees/count", eventID),
	}, &out)
	return out.Value, err
}
