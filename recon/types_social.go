package recon

// Types for the social graph, media galleries and events. All [client] —
// read from Recon's own web bundle. See docs/api/recon.md sections 7, 9
// and 13.
//
// Payment and verification are documented in docs/api/recon.md but are
// deliberately not implemented: they are account-administration surface
// rather than things a member does day to day.

// FriendRequest is a pending friendship request in either direction.
type FriendRequest struct {
	RequestingProfileID  string `json:"requestingProfileId"`
	TargetProfileID      string `json:"targetProfileId"`
	RequestDate          string `json:"requestDate"`
	RequestingProfileURL string `json:"requestingProfileUrl"`
	TargetProfileURL     string `json:"targetProfileUrl"`
}

// RelationPreferences controls notification delivery for social events, and
// carries stealth mode.
//
// ShowVisits false IS stealth mode: with it off, your profile views are not
// recorded against the profiles you look at.
type RelationPreferences struct {
	ProfileID                     string `json:"profileId,omitempty"`
	ShowVisits                    bool   `json:"showVisits"`
	CruiseEmail                   bool   `json:"cruiseEmail"`
	CruisePushNotification        bool   `json:"cruisePushNotification"`
	FriendRequestEmail            bool   `json:"friendRequestEmail"`
	FriendRequestPushNotification bool   `json:"friendRequestPushNotification"`
	FollowerPushNotification      bool   `json:"followerPushNotification"`
}

// ListViewStats holds the "last viewed" watermarks for the cruise and visitor
// lists.
//
// Compare an entry's date against the matching watermark to decide whether it
// is unseen; a null watermark means nothing has been viewed yet. Notifications
// are not covered here — they have their own watermark on the feed service.
type ListViewStats struct {
	CruisedByLastViewedDate string `json:"cruisedByLastViewedDate"`
	VisitorsLastViewedDate  string `json:"visitorsLastViewedDate"`
}

// MediaFileProperties describes one uploaded image.
//
// ClassificationID 0 means the moderation pass is still pending; values >= 1
// are not resolvable from the client bundle.
type MediaFileProperties struct {
	ID                 string `json:"id"`
	Caption            string `json:"caption"`
	ClassificationID   int    `json:"classificationId"`
	ClassificationDate string `json:"classificationDate"`
	Dimensions         struct {
		WidthPixels  int `json:"widthPixels"`
		HeightPixels int `json:"heightPixels"`
	} `json:"dimensions"`
	FileSizeInBytes  int64       `json:"fileSizeInBytes"`
	FileType         string      `json:"fileType"`
	Files            []MediaFile `json:"files"`
	GalleryPositions []struct {
		GalleryID     string `json:"galleryId"`
		GalleryTypeID int    `json:"galleryTypeId"`
		Position      int    `json:"position"`
	} `json:"galleryPositions"`
	IsRestricted    bool   `json:"isRestricted"`
	UploadDate      string `json:"uploadDate"`
	RotationDegrees int    `json:"rotationDegrees"`
}

// Event is a listed event.
type Event struct {
	ID                  string      `json:"id"`
	Name                string      `json:"name"`
	Location            string      `json:"location"`
	StartDate           string      `json:"startDate"`
	EndDate             string      `json:"endDate"`
	ShowAttendance      bool        `json:"showAttendance"`
	ThumbnailImageFiles []MediaFile `json:"thumbnailImageFiles"`
	URL                 string      `json:"url"`
	IsSponsored         bool        `json:"isSponsored"`
	SponsorName         string      `json:"sponsorName"`
	Country             string      `json:"country"`
	PhotosBy            string      `json:"photosBy"`
	MediaCount          int         `json:"mediaCount"`
	Content             string      `json:"content"`
	Slug                string      `json:"slug"`
}
