package scruff

import (
	"encoding/json"
	"strings"
	"time"
)

// MessageType values. See docs/api/scruff-enums.md.
const (
	MessageTypeUnset    = 0
	MessageTypeText     = 1
	MessageTypeImage    = 2
	MessageTypeVideo    = 3
	MessageTypeLocation = 4
	MessageTypeEmoji    = 5
	MessageTypeGif      = 6
	MessageTypeHLSVideo = 7
	MessageTypeReaction = 8
	MessageTypeTyping   = 9
	MessageTypeAlbum    = 12
)

// MediaBehavior values. This enum is complete.
const (
	MediaBehaviorNormal     = 0
	MediaBehaviorSingleView = 1
)

// Chat media kinds for ResolveMedia.
const (
	ChatMediaImage = 0
	ChatMediaVideo = 1
)

// Session is everything needed to resume a logged-in client. It is plain data
// and safe to JSON-marshal for storage.
//
// Unlike most APIs there is nothing here that expires. DeviceID is the entire
// credential; guard it like a password.
type Session struct {
	Email string `json:"email,omitempty"`
	// DeviceID is the credential: "droid-" plus 40 hex characters,
	// client-generated and bound to the account by Connect.
	DeviceID string `json:"device_id,omitempty"`
	// HardwareID is a per-install identity, sent alongside DeviceID.
	HardwareID string `json:"hardware_id,omitempty"`
	// ProfileID is your own profile id.
	ProfileID string `json:"profile_id,omitempty"`

	// AES256Key and AES256IV are client-generated and key the realtime stream.
	// They are registered with the server at login and cannot change afterwards
	// without re-registering.
	AES256Key string `json:"aes256_key,omitempty"`
	AES256IV  string `json:"aes256_iv,omitempty"`

	// Socket credentials, refreshed by Register.
	SocketHost string `json:"socket_host,omitempty"`
	SocketPort int    `json:"socket_port,omitempty"`
	SocketPwd  string `json:"socket_pwd,omitempty"`

	// CDN hosts as delivered by the server. Prefer these over the constants.
	ProfileCDN  string `json:"profile_cdn,omitempty"`
	AlbumCDN    string `json:"album_cdn,omitempty"`
	AppCDN      string `json:"app_cdn,omitempty"`
	ProfilesCDN string `json:"profiles_cdn,omitempty"`
	AccountTier string `json:"account_tier,omitempty"`
}

// Valid reports whether the session can make authenticated calls.
func (s Session) Valid() bool { return s.DeviceID != "" && s.ProfileID != "" }

// Credentials is an email and password.
type Credentials struct {
	Email    string
	Password string
}

// LatLng supplies coordinates for the request signature and for location-aware
// endpoints. A zero value is sent as "0.0", which the app also does when it has
// no fix.
type LatLng struct {
	Latitude  float64
	Longitude float64
	// Provider labels the source, e.g. "fused" or "unknown".
	Provider string
}

// RegisterResponse is the session payload.
//
// The API returns 21 top-level keys; the ones modelled here are those a client
// actually needs. See docs/api/scruff.md for the complete list.
type RegisterResponse struct {
	Socket        SocketInfo  `json:"socket"`
	Profile       Profile     `json:"profile"`
	Now           string      `json:"now"`
	CDN           string      `json:"cdn"`
	ProfileCDN    string      `json:"profile_cdn"`
	AppCDN        string      `json:"app_cdn"`
	AlbumImageCDN string      `json:"album_image_cdn"`
	AccountTier   AccountTier `json:"account_tier"`
	// DeviceSettings is a JSON-encoded STRING, not an object. Decode it twice.
	DeviceSettings  string           `json:"device_settings"`
	Indicators      Indicators       `json:"indicators"`
	FavoriteFolders []FavoriteFolder `json:"favorite_folders"`
	Features        []Feature        `json:"features"`
	StripePublicKey string           `json:"stripe_public_key"`
	GDPR            bool             `json:"gdpr"`
	// SuggestedEmail appears on the anonymous bootstrap response and is the
	// device's Google account address. Do not log it.
	SuggestedEmail string `json:"suggested_email"`
}

// SocketInfo is the realtime endpoint and its password.
type SocketInfo struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	Pwd  string `json:"pwd"`
}

// AccountTier is the membership state.
type AccountTier struct {
	Tier    string `json:"tier"` // "free" | "pro"
	ProType *struct {
		Type        string `json:"type"`
		ActivatedAt string `json:"activated_at"`
		ExpiresAt   string `json:"expires_at"`
		AutoRenew   bool   `json:"auto_renew"`
		StoreID     string `json:"store_id"`
	} `json:"pro_type"`
}

// Indicators are per-category last/viewed watermarks.
type Indicators struct {
	Woofs    Indicator `json:"woofs"`
	Albums   Indicator `json:"albums"`
	Looks    Indicator `json:"looks"`
	Messages Indicator `json:"messages"`
}

// Indicator is one last/viewed pair.
type Indicator struct {
	Last   string `json:"last"`
	Viewed string `json:"viewed"`
}

// FavoriteFolder groups favourited profiles.
type FavoriteFolder struct {
	ID         json.Number `json:"id"`
	CreatorID  json.Number `json:"creator_id"`
	FolderType *int        `json:"folder_type"`
	Name       string      `json:"name"`
}

// Feature is one server-delivered feature flag.
type Feature struct {
	Key     string          `json:"key"`
	Name    string          `json:"name"`
	Data    json.RawMessage `json:"data"`
	Enabled bool            `json:"enabled"`
}

// Profile is a member profile. Grid endpoints return a subset of these fields;
// treat everything as optional.
type Profile struct {
	ID        json.Number `json:"id"`
	Name      string      `json:"name"`
	Email     string      `json:"email,omitempty"`
	LoggedIn  bool        `json:"logged_in"`
	Online    bool        `json:"online"`
	Recent    bool        `json:"recent"`
	LastLogin string      `json:"last_login"`
	// Distance in metres.
	Distance float64 `json:"dst"`
	// HasImage is a photo VERSION, not a boolean, despite the name.
	HasImage      int            `json:"has_image"`
	AlbumImages   int            `json:"album_images"`
	Flavors       []int          `json:"flavors"`
	FacePic       bool           `json:"face_pic"`
	Traveling     bool           `json:"traveling"`
	NewMember     bool           `json:"new_member"`
	HideDistance  bool           `json:"hide_distance"`
	DisableReadRx bool           `json:"disable_read_receipts"`
	AlbumSharedTo bool           `json:"album_shared_to"`
	AlbumSharedBy bool           `json:"album_shared_from"`
	Unread        int            `json:"unread"`
	ActionAt      string         `json:"action_at"`
	ProfilePhotos []ProfilePhoto `json:"profile_photos"`

	About    string   `json:"about,omitempty"`
	City     string   `json:"city,omitempty"`
	Age      int      `json:"age_in_years,omitempty"`
	Height   *float64 `json:"height,omitempty"`
	WeightKg *float64 `json:"weight_kg,omitempty"`
}

// ProfilePhoto is one photo on a profile. The keys are CDN path components.
type ProfilePhoto struct {
	ThumbnailKey    string  `json:"thumbnail_key"`
	FullsizeKey     string  `json:"fullsize_key"`
	Version         int     `json:"version"`
	CropSource      int     `json:"crop_source"`
	VerifiedStatus  int     `json:"verified_status"`
	ModerationState int     `json:"moderation_state"`
	Violation       int     `json:"violation"`
	CreatedAt       string  `json:"created_at"`
	XCenterOffset   float64 `json:"x_center_offset_pct"`
	YCenterOffset   float64 `json:"y_center_offset_pct"`
	HeightPct       float64 `json:"height_pct"`
}

// Conversation is an inbox thread.
//
// There is no conversation id: threads are keyed by the peer's profile id,
// which is what ID holds.
type Conversation struct {
	Profile
	// Messages is a preview of the latest one or two messages.
	Messages []Message `json:"messages"`
}

// PeerID returns the other participant's profile id.
func (c Conversation) PeerID() string { return c.ID.String() }

// Message is a chat message.
//
// Version, not ID, is the ordering key: it is a dense, gap-free per-conversation
// sequence starting at 1.
type Message struct {
	ID          json.Number `json:"id"`
	SenderID    json.Number `json:"sender_id"`
	RecipientID json.Number `json:"recipient_id"`
	// GUID is the dedup key. Outbound guids are UPPERCASE and inbound ones are
	// lowercase, so always compare case-insensitively.
	GUID        string `json:"guid"`
	Version     int64  `json:"version"`
	MessageType int    `json:"message_type"`
	CreatedAt   string `json:"created_at"`
	// Message is text for type 1 and a JSON blob for type 8; absent for media.
	Message json.RawMessage `json:"message"`
	Unread  bool            `json:"unread"`
	Reply   *Reply          `json:"reply"`

	FullsizeWidth  int         `json:"fullsize_width"`
	FullsizeHeight int         `json:"fullsize_height"`
	MediaBehavior  int         `json:"media_behavior"`
	AlbumID        json.Number `json:"album_id"`
	IsAlbumShared  bool        `json:"is_album_shared"`
	AlbumBlurhash  string      `json:"album_blurhash"`
	AlbumVersion   string      `json:"album_version"`

	// Sender is present only on realtime deliveries.
	Sender *Profile `json:"sender"`
}

// SameGUID reports whether two guids refer to the same message, accounting for
// the case difference between outbound and inbound copies.
func SameGUID(a, b string) bool { return strings.EqualFold(a, b) }

// Text returns the message body, handling both quoted-string and raw encodings.
func (m Message) Text() string {
	if len(m.Message) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(m.Message, &s); err == nil {
		return s
	}
	return string(m.Message)
}

// Created parses CreatedAt, which is an HTTP date rather than ISO 8601.
func (m Message) Created() time.Time {
	t, _ := parseTime(m.CreatedAt)
	return t
}

// Reaction decodes a type-8 reaction payload.
func (m Message) Reaction() (emoji, reactedTo string, ok bool) {
	if m.MessageType != MessageTypeReaction || len(m.Message) == 0 {
		return "", "", false
	}
	var p struct {
		ReactedTo string `json:"reacted_to"`
		Reaction  string `json:"reaction"`
	}
	// The payload is sometimes an object and sometimes a JSON-encoded string.
	if err := json.Unmarshal(m.Message, &p); err == nil && p.ReactedTo != "" {
		return p.Reaction, p.ReactedTo, true
	}
	var s string
	if err := json.Unmarshal(m.Message, &s); err == nil {
		if err := json.Unmarshal([]byte(s), &p); err == nil && p.ReactedTo != "" {
			return p.Reaction, p.ReactedTo, true
		}
	}
	return "", "", false
}

// Reply quotes either another message or a Moment.
type Reply struct {
	GUID         string      `json:"guid"`
	MomentID     json.Number `json:"moment_id"`
	MomentResult *struct {
		ImageURL  string `json:"image_url"`
		MediaType int    `json:"media_type"`
	} `json:"moment_result"`
}

// ChatPage is one page of message history.
type ChatPage struct {
	ProfileID json.Number `json:"profile_id"`
	// Results are ASCENDING by version.
	Results []Message `json:"results"`
	// MinVersion is 0 when the full history is available.
	MinVersion int64 `json:"min_version"`
	// MinVersionFree is the free-tier floor for this page.
	MinVersionFree int64 `json:"min_version_free"`
	// MaxReadVersion is the read watermark: a message is read by the peer iff
	// its version is at or below this.
	MaxReadVersion  int64 `json:"max_read_version"`
	DisableReadRcpt bool  `json:"disable_read_receipts"`
}

// GridPage is the envelope every profile grid returns.
type GridPage struct {
	Results []Profile `json:"results"`
	Max     int       `json:"max"`
	MaxFree int       `json:"max_free"`
	Offset  int       `json:"offset"`
	Count   int       `json:"count"`
	// BlockSize is the page size the SERVER chose. Advance offset by this, not
	// by whatever limit you requested.
	BlockSize int `json:"block_size"`
	// CacheID must be echoed on the next page to keep results stable.
	CacheID   string          `json:"cache_id"`
	Timestamp json.Number     `json:"timestamp"`
	Buckets   json.RawMessage `json:"buckets"`
}

// InboxPage is one page of the inbox.
type InboxPage struct {
	Results   []Conversation `json:"results"`
	Max       int            `json:"max"`
	MaxFree   int            `json:"max_free"`
	Offset    int            `json:"offset"`
	BlockSize int            `json:"block_size"`
	Count     int            `json:"count"`
	Timestamp json.Number    `json:"timestamp"`
}

// Album is a private album.
type Album struct {
	ID            json.Number `json:"id"`
	Name          string      `json:"name"`
	AlbumType     int         `json:"album_type"`
	Count         int         `json:"count"`
	IsAlbumShared bool        `json:"is_album_shared"`
	AlbumVersion  string      `json:"album_version"`
	IsDefault     bool        `json:"is_default"`
	Profile       *Profile    `json:"profile"`
}

// AlbumImage is one image in an album.
//
// The URLs are CloudFront-signed and expiring; use them promptly and re-resolve
// rather than caching.
type AlbumImage struct {
	ID             json.Number `json:"id"`
	GUID           string      `json:"guid"`
	AlbumID        json.Number `json:"album_id"`
	MediaType      int         `json:"media_type"`
	Caption        string      `json:"caption"`
	SortOrder      int         `json:"sort_order"`
	FullsizeURL    string      `json:"fullsize_url"`
	ThumbnailURL   string      `json:"thumbnail_url"`
	VideoURL       string      `json:"video_url"`
	ManifestURL    string      `json:"manifest_url"`
	FullsizeWidth  int         `json:"fullsize_width"`
	FullsizeHeight int         `json:"fullsize_height"`
	// ManifestCookies must be sent as cookies to fetch HLS segments.
	ManifestCookies map[string]string `json:"manifest_cookies"`
}

// AlbumContents is the album read response.
type AlbumContents struct {
	AlbumID json.Number  `json:"album_id"`
	Album   Album        `json:"album"`
	Results []AlbumImage `json:"results"`
}

// results is the wrapper many endpoints use.
type results[T any] struct {
	Results T `json:"results"`
}
