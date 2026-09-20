package scruff

// The user-facing surface beyond messaging: woofs, looks, favorites, blocks,
// profile editing, hashtags, notes, albums, moments, and SCRUFF Venture
// (events, cities, trips).
//
// Provenance: mostly [client] — read from the Android app — with the woof,
// look and grid reads [observed]. None of the write paths here has been
// exercised against the live API.
//
// Several of these are visible to the other person and are not undoable from
// the client: Woof, RecordLook, Favorite, Block and album sharing all notify or
// appear in someone else's list. Call them from a deliberate human action, and
// do not loop them over a grid page — the app enforces its own per-action caps
// (limit_add_favorite_user 80, limit_hide_block_user_v2 150) precisely because
// this surface is abusable.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/vladzaharia/scruff-recon-sdk/core"
)

// Grid paths for the lists this file writes to.
const (
	GridFavoriteFolders = "/app/favorite/folders"
	GridAlbumsUnlocked  = "/app/albums/permissions"
	GridEventRSVPs      = "/app/events/rsvps"
)

// --- woofs and looks -------------------------------------------------------

// Woof sends a woof. This notifies the recipient and cannot be withdrawn.
//
// The server echoes it on the realtime socket as class 1. momentID is optional
// and attributes the woof to a moment of theirs.
func (c *Client) Woof(ctx context.Context, recipientProfileID, momentID string) error {
	r := &core.Request{Method: http.MethodPost, Path: "/app/poke"}
	r.SetForm("recipient", recipientProfileID)
	if momentID != "" {
		r.SetForm("moment_id", momentID)
	}
	return c.tr.JSON(ctx, r, nil)
}

// RecordLook records that you viewed a profile, which puts you in their
// "viewed you" grid.
//
// Fetching a profile with Profile() does not do this; the look is a separate,
// explicit act. It is suppressed when your account has stealth set.
func (c *Client) RecordLook(ctx context.Context, targetProfileID string) error {
	r := &core.Request{Method: http.MethodPost, Path: "/app/profile/view"}
	r.SetForm("target", targetProfileID)
	return c.tr.JSON(ctx, r, nil)
}

// WoofsReceived lists woofs sent to you.
func (c *Client) WoofsReceived(ctx context.Context, opt GridOptions) (*GridPage, error) {
	return c.Grid(ctx, GridWoofsReceived, opt)
}

// WoofsSent lists woofs you have sent.
func (c *Client) WoofsSent(ctx context.Context, opt GridOptions) (*GridPage, error) {
	return c.Grid(ctx, GridWoofsSent, opt)
}

// ViewedYou lists profiles that looked at you.
func (c *Client) ViewedYou(ctx context.Context, opt GridOptions) (*GridPage, error) {
	return c.Grid(ctx, GridViewedYou, opt)
}

// YouViewed lists profiles you looked at.
func (c *Client) YouViewed(ctx context.Context, opt GridOptions) (*GridPage, error) {
	return c.Grid(ctx, GridYouViewed, opt)
}

// --- favorites -------------------------------------------------------------

// Favorite adds a profile to your favorites, optionally into a folder.
//
// Capped by the app's own limit_add_favorite_user (80).
func (c *Client) Favorite(ctx context.Context, profileID, folderID string) error {
	r := &core.Request{Method: http.MethodPost, Path: "/app/favorite"}
	r.SetForm("recipient", profileID)
	if folderID != "" {
		r.SetForm("folder_id", folderID)
	}
	return c.tr.JSON(ctx, r, nil)
}

// Unfavorite removes a profile from favorites, or from one folder when
// folderID is set.
func (c *Client) Unfavorite(ctx context.Context, profileID, folderID string) error {
	r := &core.Request{Method: http.MethodDelete, Path: "/app/favorite"}
	r.SetParam("recipient", profileID)
	if folderID != "" {
		r.SetParam("folder_id", folderID)
	}
	return c.tr.JSON(ctx, r, nil)
}

// SetFavoriteFolders replaces the folder membership for one favorite.
//
// This is a replace, not a merge: folders you omit are removed.
func (c *Client) SetFavoriteFolders(ctx context.Context, profileID string, folderIDs []string) error {
	r := &core.Request{Method: http.MethodPut, Path: "/app/favorite"}
	r.SetForm("target_id", profileID)
	for _, id := range folderIDs {
		r.AddForm("folder_ids[]", id)
	}
	return c.tr.JSON(ctx, r, nil)
}

// Favorites lists your favorites.
func (c *Client) Favorites(ctx context.Context, opt GridOptions) (*GridPage, error) {
	return c.Grid(ctx, GridFavorites, opt)
}

// FavoriteFolders lists your favorite folders.
func (c *Client) FavoriteFolders(ctx context.Context) ([]FavoriteFolder, error) {
	var out []FavoriteFolder
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   GridFavoriteFolders,
	}, &out)
	return out, err
}

// CreateFavoriteFolder makes a new folder.
func (c *Client) CreateFavoriteFolder(ctx context.Context, name string) error {
	r := &core.Request{Method: http.MethodPost, Path: GridFavoriteFolders}
	r.SetForm("name", name)
	return c.tr.JSON(ctx, r, nil)
}

// RenameFavoriteFolder renames a folder.
func (c *Client) RenameFavoriteFolder(ctx context.Context, folderID, name string) error {
	r := &core.Request{Method: http.MethodPut, Path: GridFavoriteFolders}
	r.SetForm("folder_id", folderID)
	r.SetForm("name", name)
	return c.tr.JSON(ctx, r, nil)
}

// DeleteFavoriteFolder removes a folder. The favorites in it are not deleted.
func (c *Client) DeleteFavoriteFolder(ctx context.Context, folderID string) error {
	r := &core.Request{Method: http.MethodDelete, Path: GridFavoriteFolders}
	r.SetParam("folder_id", folderID)
	return c.tr.JSON(ctx, r, nil)
}

// --- blocks and hides ------------------------------------------------------

// Block blocks a profile: they can no longer see you or contact you.
//
// Capped by the app's own limit_hide_block_user_v2 (150).
func (c *Client) Block(ctx context.Context, profileID string) error {
	return c.blockOrHide(ctx, profileID, false)
}

// Hide hides a profile from your grids without blocking them.
//
// Same endpoint as Block; the hide flag is what distinguishes them, and the
// two are easy to invert. hide=true hides, hide=false blocks.
func (c *Client) Hide(ctx context.Context, profileID string) error {
	return c.blockOrHide(ctx, profileID, true)
}

func (c *Client) blockOrHide(ctx context.Context, profileID string, hide bool) error {
	r := &core.Request{Method: http.MethodPost, Path: "/app/block"}
	r.SetForm("recipient", profileID)
	r.SetForm("hide", strconv.FormatBool(hide))
	return c.tr.JSON(ctx, r, nil)
}

// Unblock removes one block.
//
// Note that DELETE /app/block with no parameters clears EVERY block, so this
// method always sends target_id. Use UnblockAll if you really mean all of them.
func (c *Client) Unblock(ctx context.Context, profileID string) error {
	if profileID == "" {
		// Sending this with no parameters would clear every block on the
		// account. UnblockAll is the explicit way to ask for that.
		return errors.New("scruff: Unblock needs a profile id; use UnblockAll to clear every block")
	}
	r := &core.Request{Method: http.MethodDelete, Path: "/app/block"}
	r.SetParam("target_id", profileID)
	return c.tr.JSON(ctx, r, nil)
}

// UnblockAll clears every block on the account.
//
// This is the parameterless DELETE, which is irreversible and affects everyone
// you have ever blocked. It is a separate method precisely so it cannot happen
// by passing an empty id to Unblock.
func (c *Client) UnblockAll(ctx context.Context) error {
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodDelete,
		Path:   "/app/block",
	}, nil)
}

// Blocks lists blocked and hidden profiles.
func (c *Client) Blocks(ctx context.Context, opt GridOptions) (*GridPage, error) {
	return c.Grid(ctx, GridBlocks, opt)
}

// --- discovery -------------------------------------------------------------

// Discover fetches the ranked profile stacks on the discover tab.
//
// This endpoint works unauthenticated (signed only), which is unusual for this
// API.
func (c *Client) Discover(ctx context.Context) (json.RawMessage, error) {
	lat, lon, _ := latLngStrings(c.cfg.loc())
	var out json.RawMessage
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/app/discover",
		Query:  url.Values{"latitude": {lat}, "longitude": {lon}, "locale": {"en"}},
	}, &out)
	return out, err
}

// DiscoverMore expands one discover stack.
func (c *Client) DiscoverMore(ctx context.Context, stackID string) ([]Profile, error) {
	var out struct {
		Results []Profile `json:"results"`
	}
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/app/discover/more",
		Query:  url.Values{"stack_id": {stackID}},
	}, &out)
	return out.Results, err
}

// Recommendations fetches recommended profile stacks.
func (c *Client) Recommendations(ctx context.Context) (json.RawMessage, error) {
	lat, lon, _ := latLngStrings(c.cfg.loc())
	var out json.RawMessage
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/app/recommendations",
		Query: url.Values{
			"flavor":    {FlavorScruff},
			"latitude":  {lat},
			"longitude": {lon},
		},
	}, &out)
	return out, err
}

// Alerts fetches the notifications list.
func (c *Client) Alerts(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.tr.JSON(ctx, &core.Request{Method: http.MethodGet, Path: "/app/alerts"}, &out)
	return out, err
}

// --- profile editing -------------------------------------------------------

// EditProfile updates your own profile.
//
// The body is AccountParamsDTO: a flat map of editable fields. It is passed as
// a map rather than a struct because the field set is large, sparsely used and
// enum-heavy, and because sending a zero value is not the same as omitting a
// field — a struct with omitempty would silently drop a deliberate 0.
//
// Watch one asymmetry: the write key is disable_auto_travel_creation while the
// read model exposes disable_auto_travel_icon.
//
// See docs/api/scruff.md section 7.2 for the full field list and
// scruff-enums.md for the enum values.
func (c *Client) EditProfile(ctx context.Context, fields map[string]any) error {
	body := make(map[string]any, len(fields)+1)
	for k, v := range fields {
		body[k] = v
	}
	body["request_guid"] = core.NewUUID()
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodPost,
		Path:   "/app/profile",
		JSON:   body,
	}, nil)
}

// SetNote stores a private note about another profile. Only you can see it.
func (c *Client) SetNote(ctx context.Context, targetProfileID, note string) error {
	r := &core.Request{Method: http.MethodPost, Path: "/app/profile/note"}
	r.SetForm("target_id", targetProfileID)
	r.SetForm("note", note)
	return c.tr.JSON(ctx, r, nil)
}

// AddHashtag adds a hashtag to your profile.
func (c *Client) AddHashtag(ctx context.Context, hashtag string) error {
	r := &core.Request{Method: http.MethodPost, Path: "/app/profile/hashtag"}
	r.SetForm("hashtag", hashtag)
	return c.tr.JSON(ctx, r, nil)
}

// RemoveHashtag removes a hashtag from your profile, by its id rather than its
// text.
func (c *Client) RemoveHashtag(ctx context.Context, hashtagID string) error {
	r := &core.Request{Method: http.MethodDelete, Path: "/app/profile/hashtag"}
	r.SetParam("hashtag_id", hashtagID)
	return c.tr.JSON(ctx, r, nil)
}

// PopularHashtag is a suggestion from the hashtag autocomplete.
type PopularHashtag struct {
	Hashtag string `json:"hashtag"`
	Count   int    `json:"count"`
}

// PopularHashtags searches the hashtag suggestions.
func (c *Client) PopularHashtags(ctx context.Context, query string) ([]PopularHashtag, error) {
	var out []PopularHashtag
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/app/profile/popular_hashtags",
		Query:  url.Values{"query": {query}},
	}, &out)
	return out, err
}

// NamedOption is an {id, name} lookup entry.
type NamedOption struct {
	ID   json.Number `json:"id"`
	Name string      `json:"name"`
}

// Genders lists the selectable gender identities.
func (c *Client) Genders(ctx context.Context) ([]NamedOption, error) {
	var out struct {
		Results []NamedOption `json:"results"`
	}
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/app/profile/genders",
	}, &out)
	return out.Results, err
}

// Pronouns lists the selectable pronoun sets.
func (c *Client) Pronouns(ctx context.Context) ([]NamedOption, error) {
	var out []NamedOption
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/app/profile/pronouns",
	}, &out)
	return out, err
}

// ProfileStats fetches the Insights numbers for a profile.
func (c *Client) ProfileStats(ctx context.Context, profileID string) (json.RawMessage, error) {
	if profileID == "" {
		profileID = c.ProfileID()
	}
	var out json.RawMessage
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/app/profile/stats",
		Query:  url.Values{"profile_id": {profileID}},
	}, &out)
	return out, err
}

// DeletePhoto removes one profile photo by slot index.
func (c *Client) DeletePhoto(ctx context.Context, photoIndex int) error {
	r := &core.Request{Method: http.MethodDelete, Path: "/app/profile/photo"}
	r.SetParam("photo_index", strconv.Itoa(photoIndex))
	return c.tr.JSON(ctx, r, nil)
}

// ReorderPhoto moves a profile photo between slots.
func (c *Client) ReorderPhoto(ctx context.Context, from, to int) error {
	r := &core.Request{Method: http.MethodPut, Path: "/app/profile/photo"}
	r.SetForm("from", strconv.Itoa(from))
	r.SetForm("to", strconv.Itoa(to))
	return c.tr.JSON(ctx, r, nil)
}

// --- albums ----------------------------------------------------------------

// CreateAlbum makes a private album. albumType 2 is Private; see
// docs/api/scruff.md section 8.1 for the rest.
func (c *Client) CreateAlbum(ctx context.Context, name string, albumType int) error {
	r := &core.Request{Method: http.MethodPost, Path: "/app/albums"}
	r.SetForm("name", name)
	r.SetForm("album_type", strconv.Itoa(albumType))
	return c.tr.JSON(ctx, r, nil)
}

// RenameAlbum renames an album.
func (c *Client) RenameAlbum(ctx context.Context, albumID, name string) error {
	r := &core.Request{Method: http.MethodPut, Path: "/app/albums"}
	r.SetForm("album_id", albumID)
	r.SetForm("name", name)
	return c.tr.JSON(ctx, r, nil)
}

// DeleteAlbum deletes an album and its contents.
func (c *Client) DeleteAlbum(ctx context.Context, albumID string) error {
	r := &core.Request{Method: http.MethodDelete, Path: "/app/albums"}
	r.SetParam("album_id", albumID)
	return c.tr.JSON(ctx, r, nil)
}

// DeleteAlbumImage removes one image from an album.
func (c *Client) DeleteAlbumImage(ctx context.Context, imageID string) error {
	r := &core.Request{Method: http.MethodDelete, Path: "/app/albums/images"}
	r.SetParam("image_id", imageID)
	return c.tr.JSON(ctx, r, nil)
}

// MoveAlbumImage moves an image into another album.
func (c *Client) MoveAlbumImage(ctx context.Context, imageID, albumID string) error {
	r := &core.Request{Method: http.MethodPut, Path: "/app/albums/images"}
	r.SetForm("image_id", imageID)
	r.SetForm("album_id", albumID)
	return c.tr.JSON(ctx, r, nil)
}

// EditAlbumImage sets an image's caption and sort order.
func (c *Client) EditAlbumImage(ctx context.Context, imageID, albumID, caption string, sortOrder int) error {
	r := &core.Request{Method: http.MethodPut, Path: "/app/albums/images"}
	r.SetForm("image_id", imageID)
	r.SetForm("album_id", albumID)
	r.SetForm("caption", caption)
	r.SetForm("sort_order", strconv.Itoa(sortOrder))
	return c.tr.JSON(ctx, r, nil)
}

// SaveSharedAlbumImage saves an image from someone else's shared album into
// your own archive.
func (c *Client) SaveSharedAlbumImage(ctx context.Context, albumID, imageID string) error {
	r := &core.Request{Method: http.MethodPost, Path: "/app/albums/cross_user_archive"}
	r.SetForm("album_id", albumID)
	r.SetForm("image_id", imageID)
	return c.tr.JSON(ctx, r, nil)
}

// UnshareAlbum revokes album access from specific profiles.
//
// There is no POST counterpart in this build: sharing happens by sending a
// chat message of type 12 carrying shared_album_id. See SendOptions.
func (c *Client) UnshareAlbum(ctx context.Context, albumID string, targetProfileIDs []string) error {
	r := &core.Request{Method: http.MethodDelete, Path: "/app/albums/permissions"}
	r.SetParam("album_id", albumID)
	for _, id := range targetProfileIDs {
		r.AddQuery("target_ids[]", id)
	}
	return c.tr.JSON(ctx, r, nil)
}

// AlbumsUnlockedFor lists the profiles you have shared albums with.
func (c *Client) AlbumsUnlockedFor(ctx context.Context, opt GridOptions) (*GridPage, error) {
	return c.Grid(ctx, GridAlbumsUnlocked, opt)
}

// AlbumsReceived lists albums other people have shared with you.
func (c *Client) AlbumsReceived(ctx context.Context, opt GridOptions) (*GridPage, error) {
	return c.Grid(ctx, GridAlbumsReceived, opt)
}

// --- moments ---------------------------------------------------------------

// Moments fetches the moments feed.
func (c *Client) Moments(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.tr.JSON(ctx, &core.Request{Method: http.MethodGet, Path: "/app/moments"}, &out)
	return out, err
}

// Moment fetches one moment.
func (c *Client) Moment(ctx context.Context, momentID string) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/app/moments/show",
		Query:  url.Values{"moment_id": {momentID}},
	}, &out)
	return out, err
}

// NearbyMoments fetches moments near your current position.
func (c *Client) NearbyMoments(ctx context.Context) (json.RawMessage, error) {
	lat, lon, _ := latLngStrings(c.cfg.loc())
	var out json.RawMessage
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/app/moments/nearby",
		Query:  url.Values{"latitude": {lat}, "longitude": {lon}},
	}, &out)
	return out, err
}

// TrendingMoments fetches trending moments.
//
// This returned 400 in every observation, consistently with the unknown
// location the captures were taken under. Set a real position via
// WithLocation or WithLocationProvider before calling it.
func (c *Client) TrendingMoments(ctx context.Context) (json.RawMessage, error) {
	lat, lon, _ := latLngStrings(c.cfg.loc())
	var out json.RawMessage
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/app/moments/trending",
		Query:  url.Values{"latitude": {lat}, "longitude": {lon}, "locale": {"en"}},
	}, &out)
	return out, err
}

// MarkMomentViewed records that you watched a moment. Visible to its poster.
func (c *Client) MarkMomentViewed(ctx context.Context, momentID string) error {
	r := &core.Request{Method: http.MethodPost, Path: "/app/moment/view"}
	r.SetForm("moment_id", momentID)
	return c.tr.JSON(ctx, r, nil)
}

// DeleteMoment removes one of your moments.
func (c *Client) DeleteMoment(ctx context.Context, momentID string) error {
	r := &core.Request{Method: http.MethodDelete, Path: "/app/moment"}
	r.SetParam("moment_id", momentID)
	return c.tr.JSON(ctx, r, nil)
}

// MomentTargets lists the profiles a moment can be targeted at.
func (c *Client) MomentTargets(ctx context.Context) ([]Profile, error) {
	var out []Profile
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/app/moments/targets",
	}, &out)
	return out, err
}

// MuteMoments stops showing you another profile's moments.
func (c *Client) MuteMoments(ctx context.Context, targetProfileID string) error {
	r := &core.Request{Method: http.MethodPost, Path: "/app/moments/mutes"}
	r.SetForm("target_profile_id", targetProfileID)
	return c.tr.JSON(ctx, r, nil)
}

// UnmuteMoments undoes MuteMoments.
func (c *Client) UnmuteMoments(ctx context.Context, targetProfileID string) error {
	r := &core.Request{Method: http.MethodDelete, Path: "/app/moments/mutes"}
	r.SetParam("target_profile_id", targetProfileID)
	return c.tr.JSON(ctx, r, nil)
}

// --- SCRUFF Venture: events, cities, trips ---------------------------------

// Events lists events near your current position.
func (c *Client) Events(ctx context.Context) (json.RawMessage, error) {
	lat, lon, _ := latLngStrings(c.cfg.loc())
	var out json.RawMessage
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/app/events",
		Query:  url.Values{"latitude": {lat}, "longitude": {lon}},
	}, &out)
	return out, err
}

// EventDetails fetches one event.
func (c *Client) EventDetails(ctx context.Context, eventID string) (json.RawMessage, error) {
	lat, lon, _ := latLngStrings(c.cfg.loc())
	var out json.RawMessage
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/app/events/details",
		Query:  url.Values{"event_id": {eventID}, "latitude": {lat}, "longitude": {lon}},
	}, &out)
	return out, err
}

// RSVP marks you as attending an event. This may be visible to other
// attendees.
func (c *Client) RSVP(ctx context.Context, eventID string) error {
	r := &core.Request{Method: http.MethodPost, Path: GridEventRSVPs}
	r.SetForm("event_id", eventID)
	return c.tr.JSON(ctx, r, nil)
}

// CancelRSVP withdraws an RSVP.
func (c *Client) CancelRSVP(ctx context.Context, eventID string) error {
	r := &core.Request{Method: http.MethodDelete, Path: GridEventRSVPs}
	r.SetParam("event_id", eventID)
	return c.tr.JSON(ctx, r, nil)
}

// RSVPs lists the events you have RSVPed to.
func (c *Client) RSVPs(ctx context.Context, opt GridOptions) (*GridPage, error) {
	return c.Grid(ctx, GridEventRSVPs, opt)
}

// Cities lists explorer cities near your position.
func (c *Client) Cities(ctx context.Context) (json.RawMessage, error) {
	lat, lon, _ := latLngStrings(c.cfg.loc())
	var out json.RawMessage
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/app/explorer/cities",
		Query:  url.Values{"latitude": {lat}, "longitude": {lon}},
	}, &out)
	return out, err
}

// CityDetails fetches one city's here-now, here-soon, ambassadors and events.
func (c *Client) CityDetails(ctx context.Context, locationID string) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/app/explorer/details",
		Query:  url.Values{"location_id": {locationID}},
	}, &out)
	return out, err
}

// Geocode resolves a place query to locations.
func (c *Client) Geocode(ctx context.Context, query string) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/app/explorer/geocode",
		Query:  url.Values{"query": {query}, "language_code": {"en"}},
	}, &out)
	return out, err
}

// Trips lists a profile's travel plans. Empty profileID means your own.
func (c *Client) Trips(ctx context.Context, profileID string) (json.RawMessage, error) {
	if profileID == "" {
		profileID = c.ProfileID()
	}
	var out json.RawMessage
	err := c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet,
		Path:   "/app/explorer/trips",
		Query:  url.Values{"profile_id": {profileID}},
	}, &out)
	return out, err
}

// TripOptions describes a travel plan.
type TripOptions struct {
	ID         string // set to update an existing trip
	Location   string
	LocationID string
	StartsAt   string
	EndsAt     string
	Notes      string
	Category   int
	Ongoing    bool
}

// SaveTrip creates a trip, or updates one when opt.ID is set.
//
// Trips are visible on your profile to anyone who can see it.
func (c *Client) SaveTrip(ctx context.Context, opt TripOptions) error {
	method := http.MethodPost
	if opt.ID != "" {
		method = http.MethodPut
	}
	r := &core.Request{Method: method, Path: "/app/explorer/trips"}
	if opt.ID != "" {
		r.SetForm("id", opt.ID)
	}
	if opt.Location != "" {
		r.SetForm("location", opt.Location)
	}
	if opt.LocationID != "" {
		r.SetForm("location_id", opt.LocationID)
	}
	if opt.StartsAt != "" {
		r.SetForm("starts_at", opt.StartsAt)
	}
	if opt.EndsAt != "" {
		r.SetForm("ends_at", opt.EndsAt)
	}
	if opt.Notes != "" {
		r.SetForm("notes", opt.Notes)
	}
	r.SetForm("category", strconv.Itoa(opt.Category))
	r.SetForm("ongoing", strconv.FormatBool(opt.Ongoing))
	return c.tr.JSON(ctx, r, nil)
}

// DeleteTrip removes a trip.
func (c *Client) DeleteTrip(ctx context.Context, tripID string) error {
	r := &core.Request{Method: http.MethodDelete, Path: "/app/explorer/trips"}
	r.SetParam("id", tripID)
	return c.tr.JSON(ctx, r, nil)
}
