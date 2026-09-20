package recon

// Types for the surface beyond messaging and discovery.
//
// Provenance: everything here is [client] — read from Recon's own web bundle
// rather than seen on the wire. The shapes are authoritative (they are what the
// official client sends and decodes), but none of the write paths in this file
// has been exercised against the live API. See docs/api/recon.md.

// AccountPreferences is the account-scoped display preference set.
//
// Note the metric flags are independent: an account can show imperial distance
// and metric height at once, which is the default in several locales.
type AccountPreferences struct {
	Culture            string `json:"culture,omitempty"`
	Locale             string `json:"locale,omitempty"`
	ShowMetricDistance bool   `json:"showMetricDistance"`
	ShowMetricHeight   bool   `json:"showMetricHeight"`
	GMTOffsetSeconds   int    `json:"gmtOffsetSeconds"`
	CountryCode2       string `json:"countryCode2,omitempty"`
}

// MessagingPreferences controls message notification delivery.
type MessagingPreferences struct {
	ProfileID               string `json:"profileId,omitempty"`
	MessageEmail            bool   `json:"messageEmail"`
	MessagePushNotification bool   `json:"messagePushNotification"`
}

// SearchFilters is the saved default filter set for the discovery grid.
//
// Every scalar is a pointer because `null` is meaningful: it means "no filter",
// which is distinct from a zero value. An `AgeMin` of 0 and an absent `AgeMin`
// are different requests.
//
// DistanceSliderIndex is a client-only concern that the server nonetheless
// persists, so round-tripping it keeps the web UI's slider position intact.
type SearchFilters struct {
	DistanceSliderIndex *int  `json:"distanceSliderIndex"`
	AgeMin              *int  `json:"ageMin"`
	AgeMax              *int  `json:"ageMax"`
	IsPremium           *bool `json:"isPremium"`
	IsNewMember         *bool `json:"isNewMember"`
	IsOnlineNow         *bool `json:"isOnlineNow"`
	HasVisibleImages    *bool `json:"hasVisibleImages"`
	HeightCmMin         *int  `json:"heightCmMin"`
	HeightCmMax         *int  `json:"heightCmMax"`
	PositionIDs         []int `json:"positionIds"`
	InterestIDs         []int `json:"interestIds"`
	BodyTypeIDs         []int `json:"bodyTypeIds"`
	BodyHairIDs         []int `json:"bodyHairIds"`
	EthnicityIDs        []int `json:"ethnicityIds"`
	SafeSexIDs          []int `json:"safeSexIds"`
	RoleIDs             []int `json:"roleIds"`
}

// ProfileDetail is the long bio, split out from Profile so it can be cached and
// invalidated independently by version. The official client caches it for 15
// minutes.
type ProfileDetail struct {
	ID       string `json:"id"`
	LongText string `json:"longText"`
	Version  int    `json:"version"`
}

// Location resolves a location id to display names.
//
// TTLMinutes is the server telling you how long to cache this. When a profile
// has no locationUrl the official client shows nothing rather than falling back.
type Location struct {
	ShortLocationName string `json:"shortLocationName"`
	LongLocationName  string `json:"longLocationName"`
	TTLMinutes        int    `json:"ttlMinutes"`
}

// GeoLocation is the position publish body.
//
// AccuracyMetres is device-sourced; omit it when you have no real measurement
// rather than inventing a value.
type GeoLocation struct {
	AppInstallationID string  `json:"appInstallationId,omitempty"`
	Latitude          float64 `json:"latitude"`
	Longitude         float64 `json:"longitude"`
	ProfileID         string  `json:"profileId,omitempty"`
	RecordedDate      string  `json:"recordedDate,omitempty"`
	AccuracyMetres    *int    `json:"accuracyMetres,omitempty"`
	Lookup            *struct {
		LongLocationName  string `json:"longLocationName"`
		ShortLocationName string `json:"shortLocationName"`
	} `json:"lookup,omitempty"`
}

// MembershipStatus describes the account's subscription.
//
// MembershipLevelID is 0 for free and official profiles and >= 1 for premium.
// The official client only ever tests > 0, so higher tiers may exist without
// distinct behaviour; use IsPremium rather than comparing to a specific tier.
type MembershipStatus struct {
	MembershipLevelID int    `json:"membershipLevelId"`
	ProfileID         string `json:"profileId"`
	StartDate         string `json:"startDate"`
	ExpiryDate        string `json:"expiryDate"`
	IsRecurring       bool   `json:"isRecurring"`
}

// IsPremium reports a paid membership.
func (m MembershipStatus) IsPremium() bool { return m.MembershipLevelID > 0 }

// BulkMessage is a broadcast or sponsored message from the dvrt service. These
// are the "official" threads that appear in the inbox alongside real ones.
//
// SenderURL is a discriminated pointer: when IsOfficial is false it targets
// /api/dvrt/dvrtsrs/{id}, and when true it targets
// /api/profile/officialProfiles/{id}. Resolve it with Advertiser or Profile
// accordingly rather than assuming one service.
//
// Message is markdown-ish: it carries \n, **bold** and [link](url).
type BulkMessage struct {
	ID           string `json:"id"`
	Message      string `json:"message"`
	MediaURL     string `json:"mediaUrl"`
	DeviceTypeID int    `json:"deviceTypeId"`
	Priority     int    `json:"priority"`
	LanguageID   int    `json:"languageId"`
	IsAdult      bool   `json:"isAdult"`
	StartDate    string `json:"startDate"`
	IsRead       bool   `json:"isRead"`
	IsOfficial   bool   `json:"isOfficial"`
	SenderURL    string `json:"senderUrl"`
	LinkURL      string `json:"linkUrl"`
	Subject      string `json:"subject"`
}

// Advertiser is the sender behind a non-official BulkMessage.
type Advertiser struct {
	ID                 string      `json:"id"`
	Name               string      `json:"name"`
	IsOfficial         bool        `json:"isOfficial"`
	OfficialProfileURL string      `json:"officialProfileUrl"`
	MediaFiles         []MediaFile `json:"mediaFiles"`
}

// ReportCategory is a reason a profile or message can be reported under.
//
// Only id 13 ("Other") is hardcoded in the official client; fetch the rest
// rather than assuming the numbering is stable.
type ReportCategory struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Priority int    `json:"priority"`
	Order    int    `json:"order"`
	Detail   string `json:"detail"`
}

// Report is a content report against a profile.
//
// MediaMetaDataIDs has a capital D, inconsistently with mediaMetadataId
// elsewhere in the API. That is the wire spelling, not a typo here.
type Report struct {
	ConversationMessageIDs []string `json:"conversationMessageIds,omitempty"`
	IsProfileContent       bool     `json:"isProfileContent"`
	MediaMetaDataIDs       []string `json:"mediaMetaDataIds,omitempty"`
	ReportCategoryID       int      `json:"reportCategoryId"`
	ReportReason           string   `json:"reportReason,omitempty"`
	ReportedByAccountID    string   `json:"reportedByAccountId,omitempty"`
}

// NameCheck is the anti-abuse verdict on a candidate profile name.
type NameCheck struct {
	IsValid bool     `json:"isValid"`
	Errors  []string `json:"errors,omitempty"`
}

// SocialList is the friends/followers/followings shape, which splits entries by
// whether the relationship is reciprocated.
type SocialList struct {
	Mutual    []RelationEntry `json:"mutual"`
	NonMutual []RelationEntry `json:"nonMutual"`
}

// SocialCounts accompanies SocialList.
type SocialCounts struct {
	MutualCount int `json:"mutualCount"`
	OtherCount  int `json:"otherCount"`
}

// PatchOp is one RFC 6902 JSON Patch operation.
//
// The official client only ever sends "replace", and always includes
// /rowVersion for optimistic concurrency. The server also accepts add, copy,
// remove and test.
type PatchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
	From  string `json:"from,omitempty"`
}
