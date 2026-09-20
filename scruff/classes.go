package scruff

// Realtime class codes.
//
// The socket multiplexes 87 distinct event classes onto one channel. Only a
// handful matter to a messaging client, but the rest are what confirm the
// writes this SDK performs -- creating an album, posting a moment, saving a
// trip -- so they are named here rather than left as magic numbers.
//
// Each class carries a kind telling you which HTTP verb it corresponds to, so a
// cache layer can invalidate the right thing. Standard means unsolicited: there
// was no originating request of yours.
//
// Generated from the table in docs/api/scruff-realtime.md section 4. [client]

// Class kinds.
const (
	ClassKindStandard = "Standard" // unsolicited; no request of yours
	ClassKindPost     = "POST"
	ClassKindPut      = "PUT"
	ClassKindDelete   = "DELETE"
)

// Class codes not already declared in realtime.go.
const (
	ClassUnknown                           = 0
	ClassAlbumCreate                       = 100
	ClassAlbumRename                       = 101
	ClassAlbumDelete                       = 102
	ClassAlbumPermissionGrant              = 103
	ClassAlbumPermissionRevoke             = 104
	ClassAlbumImageCreate                  = 105
	ClassAlbumImageDelete                  = 106
	ClassAlbumImageMove                    = 107
	ClassAlbumImageCaption                 = 108
	ClassAlbumImageChatArchive             = 109
	ClassAlbumImageSortOrder               = 110
	ClassAlbumImageCrossUserArchive        = 111
	ClassAlbumVideoDownloadReady           = 112
	ClassChatThreadMigrated                = 206
	ClassFavoriteFolderCreate              = 300
	ClassFavoriteFolderDelete              = 301
	ClassFavoriteFolderUpdate              = 302
	ClassAccountLogin                      = 400
	ClassAccountLogout                     = 401
	ClassAccountConnect                    = 402
	ClassAccountRegister                   = 403
	ClassAccountDeleteDevice               = 404
	ClassProfileSaved                      = 405
	ClassProfileDisable                    = 406
	ClassProfileEnable                     = 407
	ClassProfileDelete                     = 408
	ClassProfilePhotoRated                 = 409
	ClassProfilePhotoUploaded              = 410
	ClassProfilePhotoDeleted               = 412
	ClassProfilePhotoProgress              = 413
	ClassProfilePhotoSwapped               = 414
	ClassProfileHashtagCreated             = 415
	ClassProfileHashtagDeleted             = 416
	ClassProfileSmsDelivered               = 417
	ClassProfilePhotoVerified              = 419
	ClassStripeSyncCompleted               = 420
	ClassProfileAgeVerified                = 421
	ClassFaceLivenessCompleted             = 422
	ClassExplicitContentSettingsUrlCreated = 423
	ClassUnblock                           = 500
	ClassTransactionCreated                = 600
	ClassSubscriptionDeactivated           = 601
	ClassTransactionFailed                 = 602
	ClassSubscriptionUpdated               = 603
	ClassSubscriptionUpdateFailed          = 604
	ClassTripCreated                       = 800
	ClassTripDelete                        = 801
	ClassTripUpdate                        = 802
	ClassAmbassadorCreate                  = 803
	ClassAmbassadorDelete                  = 804
	ClassEventRsvpCreate                   = 901
	ClassEventRsvpDelete                   = 902
	ClassServerAlertAvailable              = 1000
	ClassTicketCreate                      = 1100
	ClassTicketDelete                      = 1101
	ClassTicketUpdate                      = 1102
	ClassTicketWebhookUpdate               = 1103
	ClassAdminAlert                        = 1200
	ClassAdminActivatePro                  = 1201
	ClassAdminDeactivatePro                = 1202
	ClassActivateFreeTrial                 = 1203
	ClassAdminActivateBetaFeatures         = 1204
	ClassAdminNotice                       = 1205
	ClassActivateBoostSuccess              = 1300
	ClassBuildABearContentAvailable        = 1402
	ClassBuildABearSearchDeleted           = 1403
	ClassBuildABearProgress                = 1404
	ClassVideoChatCallReceived             = 1500
	ClassVideoChatCallUpdate               = 1501
	ClassMomentsCreate                     = 1700
	ClassMomentsDelete                     = 1701
	ClassMomentsMuteCreate                 = 1703
	ClassMomentsMuteDelete                 = 1704
	ClassMomentsExclusionsUpdated          = 1705
	ClassMomentsCreateDuplicated           = 1706
)

// classInfo is the generated name and kind for every known class.
var classInfo = map[int]struct {
	Name string
	Kind string
}{
	0:    {"Unknown", ClassKindStandard},
	1:    {"Woof", ClassKindStandard},
	2:    {"Album", ClassKindStandard},
	3:    {"Match", ClassKindStandard},
	5:    {"View", ClassKindStandard},
	100:  {"AlbumCreate", ClassKindPost},
	101:  {"AlbumRename", ClassKindPut},
	102:  {"AlbumDelete", ClassKindDelete},
	103:  {"AlbumPermissionGrant", ClassKindPost},
	104:  {"AlbumPermissionRevoke", ClassKindDelete},
	105:  {"AlbumImageCreate", ClassKindPost},
	106:  {"AlbumImageDelete", ClassKindDelete},
	107:  {"AlbumImageMove", ClassKindPut},
	108:  {"AlbumImageCaption", ClassKindPut},
	109:  {"AlbumImageChatArchive", ClassKindPost},
	110:  {"AlbumImageSortOrder", ClassKindPut},
	111:  {"AlbumImageCrossUserArchive", ClassKindPost},
	112:  {"AlbumVideoDownloadReady", ClassKindPut},
	200:  {"ChatMessageDelivered", ClassKindPost},
	201:  {"ChatMessageReceived", ClassKindStandard},
	202:  {"ChatMessageUnsend", ClassKindPut},
	203:  {"ChatThreadDelete", ClassKindDelete},
	204:  {"ChatInboxDelete", ClassKindDelete},
	206:  {"ChatThreadMigrated", ClassKindStandard},
	207:  {"ChatMessageUpdated", ClassKindPut},
	208:  {"ChatRecipientTyping", ClassKindStandard},
	209:  {"ChatMessageViewed", ClassKindStandard},
	300:  {"FavoriteFolderCreate", ClassKindPost},
	301:  {"FavoriteFolderDelete", ClassKindDelete},
	302:  {"FavoriteFolderUpdate", ClassKindPut},
	400:  {"AccountLogin", ClassKindPost},
	401:  {"AccountLogout", ClassKindPost},
	402:  {"AccountConnect", ClassKindPost},
	403:  {"AccountRegister", ClassKindPost},
	404:  {"AccountDeleteDevice", ClassKindDelete},
	405:  {"ProfileSaved", ClassKindPost},
	406:  {"ProfileDisable", ClassKindPost},
	407:  {"ProfileEnable", ClassKindDelete},
	408:  {"ProfileDelete", ClassKindDelete},
	409:  {"ProfilePhotoRated", ClassKindStandard},
	410:  {"ProfilePhotoUploaded", ClassKindPost},
	412:  {"ProfilePhotoDeleted", ClassKindDelete},
	413:  {"ProfilePhotoProgress", ClassKindPost},
	414:  {"ProfilePhotoSwapped", ClassKindPut},
	415:  {"ProfileHashtagCreated", ClassKindPost},
	416:  {"ProfileHashtagDeleted", ClassKindDelete},
	417:  {"ProfileSmsDelivered", ClassKindPost},
	418:  {"AccountRegisterNeeded", ClassKindStandard},
	419:  {"ProfilePhotoVerified", ClassKindStandard},
	420:  {"StripeSyncCompleted", ClassKindPost},
	421:  {"ProfileAgeVerified", ClassKindPost},
	422:  {"FaceLivenessCompleted", ClassKindPost},
	423:  {"ExplicitContentSettingsUrlCreated", ClassKindPost},
	500:  {"Unblock", ClassKindDelete},
	600:  {"TransactionCreated", ClassKindPost},
	601:  {"SubscriptionDeactivated", ClassKindPut},
	602:  {"TransactionFailed", ClassKindPost},
	603:  {"SubscriptionUpdated", ClassKindPut},
	604:  {"SubscriptionUpdateFailed", ClassKindPut},
	800:  {"TripCreated", ClassKindPost},
	801:  {"TripDelete", ClassKindDelete},
	802:  {"TripUpdate", ClassKindPut},
	803:  {"AmbassadorCreate", ClassKindPost},
	804:  {"AmbassadorDelete", ClassKindDelete},
	901:  {"EventRsvpCreate", ClassKindPost},
	902:  {"EventRsvpDelete", ClassKindDelete},
	1000: {"ServerAlertAvailable", ClassKindStandard},
	1100: {"TicketCreate", ClassKindPost},
	1101: {"TicketDelete", ClassKindDelete},
	1102: {"TicketUpdate", ClassKindPut},
	1103: {"TicketWebhookUpdate", ClassKindStandard},
	1200: {"AdminAlert", ClassKindStandard},
	1201: {"AdminActivatePro", ClassKindPost},
	1202: {"AdminDeactivatePro", ClassKindDelete},
	1203: {"ActivateFreeTrial", ClassKindPost},
	1204: {"AdminActivateBetaFeatures", ClassKindStandard},
	1205: {"AdminNotice", ClassKindStandard},
	1300: {"ActivateBoostSuccess", ClassKindPut},
	1402: {"BuildABearContentAvailable", ClassKindStandard},
	1403: {"BuildABearSearchDeleted", ClassKindStandard},
	1404: {"BuildABearProgress", ClassKindStandard},
	1500: {"VideoChatCallReceived", ClassKindStandard},
	1501: {"VideoChatCallUpdate", ClassKindStandard},
	1700: {"MomentsCreate", ClassKindPost},
	1701: {"MomentsDelete", ClassKindDelete},
	1702: {"MomentAvailable", ClassKindStandard},
	1703: {"MomentsMuteCreate", ClassKindPost},
	1704: {"MomentsMuteDelete", ClassKindDelete},
	1705: {"MomentsExclusionsUpdated", ClassKindPost},
	1706: {"MomentsCreateDuplicated", ClassKindPost},
}

// ClassName returns the documented name for a realtime class, or "" if the
// class is not one this SDK knows about. An unknown class is not an error: the
// server adds them, and UnknownEvent carries the raw payload so you can look.
func ClassName(class int) string { return classInfo[class].Name }

// ClassKind reports which HTTP verb a class corresponds to -- ClassKindPost,
// Put, Delete, or ClassKindStandard for unsolicited events. Returns "" for an
// unknown class.
//
// Callback classes (anything other than Standard) are confirmations of a
// request, and carry the request_guid you sent, so AckEvent can correlate them
// with the call that caused them.
func ClassKind(class int) string { return classInfo[class].Kind }
