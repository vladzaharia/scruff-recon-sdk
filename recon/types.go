package recon

import "time"

// Session is everything needed to resume a logged-in client. It is plain data
// and safe to JSON-marshal for storage.
//
// AccessToken is short-lived (15 minutes) and rotates; persist the session again
// whenever OnSessionUpdate fires.
type Session struct {
	Email     string `json:"email,omitempty"`
	AccountID string `json:"account_id,omitempty"`
	// ProfileID is the active persona. All messaging, media, search and
	// relations calls are keyed by this, not by AccountID.
	ProfileID string `json:"profile_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	// AppInstallationID is created before login and is required to
	// authenticate.
	AppInstallationID string `json:"app_installation_id,omitempty"`

	// AccessToken is stored WITHOUT the "Bearer " prefix the API returns.
	AccessToken        string    `json:"access_token,omitempty"`
	AccessTokenExpiry  time.Time `json:"access_token_expiry,omitzero"`
	RefreshToken       string    `json:"refresh_token,omitempty"`
	RefreshTokenExpiry time.Time `json:"refresh_token_expiry,omitzero"`

	MembershipLevelID int `json:"membership_level_id,omitempty"`
}

// Valid reports whether the session has the fields needed to make calls.
func (s Session) Valid() bool {
	return s.AccessToken != "" && s.ProfileID != ""
}

// Credentials is an email and password.
type Credentials struct {
	Email    string
	Password string
}

// TokenSet is the authentication response.
//
// AccessToken arrives prefixed with "Bearer " — see the package documentation.
type TokenSet struct {
	AccessToken        string   `json:"accessToken"`
	AccessTokenExpiry  string   `json:"accessTokenExpiry"`
	RefreshToken       string   `json:"refreshToken"`
	RefreshTokenExpiry string   `json:"refreshTokenExpiry"`
	AccountID          string   `json:"accountId"`
	SessionID          string   `json:"sessionId"`
	ProfileIDs         []string `json:"profileIds"`
	MembershipLevelID  int      `json:"membershipLevelId"`
	// SignalRToken is returned but unused: the realtime channel authenticates
	// with the plain access token.
	SignalRToken string   `json:"signalRToken"`
	Roles        []string `json:"roles"`
	MFAEnabled   bool     `json:"mfaEnabled"`
}

// AppInstallation identifies this client install. One must exist before login.
type AppInstallation struct {
	ID             string `json:"id"`
	CreatedDate    string `json:"createdDate"`
	LastUsedDate   string `json:"lastUsedDate"`
	ApplicationID  int    `json:"applicationId"`
	DeviceTypeID   int    `json:"deviceTypeId"`
	AppVersion     string `json:"appVersion"`
	UserAgent      string `json:"userAgent"`
	Description    string `json:"description"`
	IsInstalledPWA bool   `json:"isInstalledPwa"`
}

// AppSettings is the server's own statement of its limits and feature flags.
//
// Fetch this rather than hardcoding limits — the real values differ from what
// is commonly assumed (messages cap at 1000 characters, uploads at 50 MiB).
type AppSettings struct {
	Environment     string `json:"environment"`
	TTLMinutes      int    `json:"ttlMinutes"`
	EnabledFeatures struct {
		Geolocation      bool `json:"geolocation"`
		PushNotification bool `json:"pushNotification"`
		Recaptcha        bool `json:"recaptcha"`
	} `json:"enabledFeatures"`
	Limits         Limits   `json:"limits"`
	SupportedLangs []string `json:"supportedLanguages"`
	IsAdminMFAReq  bool     `json:"isAdminMfaRequired"`
}

// Limits are the server-enforced bounds from AppSettings.
type Limits struct {
	Interests                             int      `json:"interests"`
	Blocks                                int      `json:"blocks"`
	Friends                               int      `json:"friends"`
	Following                             int      `json:"following"`
	MinImageResolution                    int      `json:"minImageResolution"`
	MinPasswordLength                     int      `json:"minPasswordLength"`
	MinimumZxcvbnScore                    int      `json:"minimumZxcvbnScore"`
	MaxFileSizeBytes                      int64    `json:"maxFileSizeBytes"`
	MaxFileAttachments                    int      `json:"maxFileAttachments"`
	AllowedFileExtensions                 []string `json:"allowedFileExtensions"`
	AllowedMediaMimeTypes                 []string `json:"allowedMediaMimeTypes"`
	MaxMessageLength                      int      `json:"maxMessageLength"`
	MinAge                                int      `json:"minAge"`
	MaxSearchAge                          int      `json:"maxSearchAge"`
	VisitRetentionPeriodDays              int      `json:"visitRetentionPeriodDays"`
	CruiseRetentionPeriodDays             int      `json:"cruiseRetentionPeriodDays"`
	StandardMemberVisitDisplayPeriodDays  int      `json:"standardMemberVisitDisplayPeriodDays"`
	StandardMemberCruiseDisplayPeriodDays int      `json:"standardMemberCruiseDisplayPeriodDays"`
	CruiseMinUpdatePeriodDays             int      `json:"cruiseMinUpdatePeriodDays"`
	MaximumFilesPerProfile                int      `json:"maximumFilesPerProfile"`
	MaximumFilesInMainGallery             int      `json:"maximumFilesInMainGallery"`
	MaximumUploadsInBatch                 int      `json:"maximumUploadsInBatch"`
}

// Conversation is a message thread. Name is empty and IsGroup false for the
// 1:1 threads that make up virtually all traffic.
type Conversation struct {
	ID                       string `json:"id"`
	Name                     string `json:"name"`
	IsGroup                  bool   `json:"isGroup"`
	IsOfficial               bool   `json:"isOfficial"`
	IsArchived               bool   `json:"isArchived"`
	IsReadOnly               bool   `json:"isReadOnly"`
	ThumbnailMediaMetadataID string `json:"thumbnailMediaMetadataId"`
	ThumbnailURL             string `json:"thumbnailUrl"`
	LastActivityDate         string `json:"lastActivityDate"`
	ParticipantCount         int    `json:"participantCount"`
	// Participants EXCLUDES you — its length is ParticipantCount-1, so a 1:1
	// thread has exactly one entry: the peer.
	Participants       []Participant `json:"participants"`
	CreatedDate        string        `json:"createdDate"`
	LastMessageExcerpt string        `json:"lastMessageExcerpt"`
	ContentURL         string        `json:"contentUrl"`
	ParticipantsURL    string        `json:"participantsUrl"`
	UserInfo           struct {
		IsFlagged          bool `json:"isFlagged"`
		UnreadMessageCount int  `json:"unreadMessageCount"`
	} `json:"userInfo"`
}

// Peer returns the other participant's profile id for a 1:1 conversation.
func (c Conversation) Peer() string {
	if len(c.Participants) == 0 {
		return ""
	}
	return c.Participants[0].ProfileID
}

// Participant is one member of a conversation, other than you.
type Participant struct {
	ProfileID  string `json:"profileId"`
	ProfileURL string `json:"profileUrl"`
}

// Message is a chat message as returned by REST.
//
// Note ContentTypeID, not MessageType: the wire field on a server response is
// contentTypeId (1 = text). The web client reads a messageType property that no
// server response actually contains, so unmarshalling that field silently
// yields zero.
type Message struct {
	ID              string       `json:"id"`
	SenderProfileID string       `json:"senderProfileId"`
	Text            string       `json:"text"`
	IsFlagged       bool         `json:"isFlagged"`
	IsRead          bool         `json:"isRead"`
	DisplayAsHTML   bool         `json:"displayAsHtml"`
	MarkupTypeID    *int         `json:"markupTypeId"`
	CreatedDate     string       `json:"createdDate"`
	ContentTypeID   int          `json:"contentTypeId"`
	Attachments     []Attachment `json:"attachments"`
	Files           []MediaFile  `json:"files"`
}

// Created parses CreatedDate.
func (m Message) Created() time.Time {
	t, _ := parseTime(m.CreatedDate)
	return t
}

// Attachment is media on a message.
type Attachment struct {
	ID              string      `json:"id"`
	MediaMetadataID string      `json:"mediaMetadataId"`
	ThumbnailURL    string      `json:"thumbnailUrl"`
	Files           []MediaFile `json:"files"`
	DownloadURL     string      `json:"downloadUrl"`
	IsRestricted    bool        `json:"isRestricted"`
}

// LargestURL returns the highest-resolution URL available, falling back to
// DownloadURL. Files are ordered small to large.
func (a Attachment) LargestURL() string {
	if n := len(a.Files); n > 0 && a.Files[n-1].URL != "" {
		return a.Files[n-1].URL
	}
	return a.DownloadURL
}

// MediaFile is one rendition of an image. ImageSize is a size code in 100–104;
// see docs/api/recon-enums.md.
type MediaFile struct {
	ImageSize int    `json:"imageSize"`
	URL       string `json:"url"`
}

// Profile is a member profile.
type Profile struct {
	ID                string      `json:"id"`
	Version           int         `json:"version"`
	Name              string      `json:"name"`
	DetailURL         string      `json:"detailUrl"`
	PrimaryImageFiles []MediaFile `json:"primaryImageFiles"`
	MembershipLevelID int         `json:"membershipLevelId"`
	Age               int         `json:"age"`
	HeightCm          *int        `json:"heightCm"`
	ShortText         string      `json:"shortText"`
	LongText          string      `json:"longText"`
	EthnicityID       *int        `json:"ethnicityId"`
	PositionID        *int        `json:"positionId"`
	BodyTypeID        *int        `json:"bodyTypeId"`
	BodyHairID        *int        `json:"bodyHairId"`
	SafeSexID         *int        `json:"safeSexId"`
	RoleID            *int        `json:"roleId"`
	Location          *struct {
		LocationID  int    `json:"locationId"`
		LocationURL string `json:"locationUrl"`
	} `json:"location"`
	LastUpdatedDate string `json:"lastUpdatedDate"`
	Interests       []int  `json:"interests"`
	CreatedDate     string `json:"createdDate"`
	IsPublic        bool   `json:"isPublic"`
	IsOfficial      bool   `json:"isOfficial"`
}

// AvatarURL returns the highest-resolution avatar available, or "".
func (p Profile) AvatarURL() string {
	best, bestSize := "", -1
	for _, f := range p.PrimaryImageFiles {
		if f.ImageSize > bestSize && f.URL != "" {
			best, bestSize = f.URL, f.ImageSize
		}
	}
	return best
}

// IsPremium reports a paid membership.
func (p Profile) IsPremium() bool { return p.MembershipLevelID > 0 }

// SearchResult is one hit from a profile search.
//
// Searches return stubs: to render anything you must fetch each ProfileURL. The
// version is the last path segment of ProfileURL.
type SearchResult struct {
	ProfileID      string `json:"profileId"`
	ProfileURL     string `json:"profileUrl"`
	DistanceMetres int    `json:"distanceMetres"`
	Date           string `json:"date"`
}

// RelationEntry is one entry in a cruise or visitor list.
type RelationEntry struct {
	Date        string `json:"date"`
	ProfileID   string `json:"profileId"`
	ProfileURL  string `json:"profileUrl"`
	ProfileName string `json:"profileName"` // populated on the block list only
}

// RelationCounts is the response from a relations count endpoint.
type RelationCounts struct {
	WithinStandardDisplayPeriod int `json:"withinStandardDisplayPeriod"`
	WithinPremiumDisplayPeriod  int `json:"withinPremiumDisplayPeriod"`
	SinceLastViewed             int `json:"sinceLastViewed"`
}

// FeedItem is a notification stub.
type FeedItem struct {
	FeedItemID     string `json:"feedItemId"`
	FeedItemURL    string `json:"feedItemUrl"`
	ProfileURL     string `json:"profileUrl"`
	IsSelfActor    bool   `json:"isSelfActor"`
	ActionDate     string `json:"actionDate"`
	IsRead         bool   `json:"isRead"`
	FeedItemTypeID int    `json:"feedItemTypeId"`
}

// Gallery is a media album.
type Gallery struct {
	ID              string      `json:"id"`
	Name            string      `json:"name"`
	EntityID        string      `json:"entityId"`
	ImageCount      int         `json:"imageCount"`
	CoverImageID    string      `json:"coverImageId"`
	CoverImageFiles []MediaFile `json:"coverImageFiles"`
	URL             string      `json:"url"`
	GalleryType     struct {
		ID           int    `json:"id"`
		Name         string `json:"name"`
		IsRestricted bool   `json:"isRestricted"`
	} `json:"galleryType"`
}

// OutgoingFile is a binary to attach to a message.
type OutgoingFile struct {
	Name     string
	MimeType string
	Data     []byte
}

// envelope is the collection wrapper most list endpoints use.
//
// TotalRecords is NOT always the collection total: on message history it is the
// size of the returned page. Do not use it for paging arithmetic.
type envelope[T any] struct {
	Data           []T    `json:"data"`
	TotalRecords   int    `json:"totalRecords"`
	MostRecentDate string `json:"mostRecentDate"`
}
