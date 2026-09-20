package recon

// Tests for the social graph, media and event surface.
//
// These endpoints are [client]-derived and the writes have never been sent to
// the live API, so these tests assert the request we construct — the part we
// actually know — and never perform a live write.

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

// The bundle sends a literal null body for cruise/visit/friend/follow, not an
// empty object. Sending {} instead is a different request.
func TestRelationWritesSendNullBody(t *testing.T) {
	for _, tc := range []struct {
		name   string
		call   func(*Client) error
		method string
		path   string
	}{
		{"Cruise", func(c *Client) error { return c.Cruise(context.Background(), "them") },
			http.MethodPut, "/profileRelations/profiles/me/cruisedProfiles/them"},
		{"Uncruise", func(c *Client) error { return c.Uncruise(context.Background(), "them") },
			http.MethodDelete, "/profileRelations/profiles/me/cruisedProfiles/them"},
		{"Visit", func(c *Client) error { return c.Visit(context.Background(), "them") },
			http.MethodPut, "/profileRelations/profiles/me/visitedProfiles/them"},
		{"SendFriendRequest", func(c *Client) error { return c.SendFriendRequest(context.Background(), "them") },
			http.MethodPut, "/profileRelations/profiles/me/friendRequests/them"},
		{"Follow", func(c *Client) error { return c.Follow(context.Background(), "them") },
			http.MethodPut, "/profileRelations/profiles/me/followers/them"},
		{"Unfollow", func(c *Client) error { return c.Unfollow(context.Background(), "them") },
			http.MethodDelete, "/profileRelations/profiles/me/followers/them"},
		{"RemoveFriend", func(c *Client) error { return c.RemoveFriend(context.Background(), "them") },
			http.MethodDelete, "/profileRelations/profiles/me/friends/them"},
		{"Unblock", func(c *Client) error { return c.Unblock(context.Background(), "them") },
			http.MethodDelete, "/profileRelations/profiles/me/blocks/them"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got capture
			c, _ := newTestClient(t, recording(&got, ""))
			if err := tc.call(c); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if got.Method != tc.method {
				t.Errorf("method = %s, want %s", got.Method, tc.method)
			}
			if got.Path != tc.path {
				t.Errorf("path = %q, want %q", got.Path, tc.path)
			}
		})
	}
}

func TestAcceptRejectFriendRequestPaths(t *testing.T) {
	for verb, call := range map[string]func(*Client) error{
		"accept": func(c *Client) error { return c.AcceptFriendRequest(context.Background(), "them") },
		"reject": func(c *Client) error { return c.RejectFriendRequest(context.Background(), "them") },
	} {
		var got capture
		c, _ := newTestClient(t, recording(&got, ""))
		if err := call(c); err != nil {
			t.Fatalf("%s: %v", verb, err)
		}
		want := "/profileRelations/profiles/me/friendRequestResponse/them/" + verb
		if got.Method != http.MethodPost || got.Path != want {
			t.Errorf("%s: %s %s, want POST %s", verb, got.Method, got.Path, want)
		}
	}
}

// Stealth mode ON means showVisits FALSE. Getting the inversion wrong silently
// turns stealth off for someone who asked for it.
func TestSetStealthModeInvertsShowVisits(t *testing.T) {
	for _, tc := range []struct {
		stealth bool
		want    bool // expected /showVisits value
	}{{true, false}, {false, true}} {
		var got capture
		c, _ := newTestClient(t, recording(&got, ""))
		if err := c.SetStealthMode(context.Background(), tc.stealth); err != nil {
			t.Fatalf("SetStealthMode(%v): %v", tc.stealth, err)
		}
		var ops []PatchOp
		if err := json.Unmarshal([]byte(got.Body), &ops); err != nil {
			t.Fatalf("body not a patch array: %s", got.Body)
		}
		if len(ops) != 1 || ops[0].Path != "/showVisits" {
			t.Fatalf("ops = %+v", ops)
		}
		if ops[0].Value != tc.want {
			t.Errorf("stealth=%v sent showVisits=%v, want %v", tc.stealth, ops[0].Value, tc.want)
		}
		if got.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", got.Method)
		}
	}
}

// followers/count and following/count are singular on profileRelations, while
// the *lists* are plural on profileSearch. Mixing them up 404s.
func TestFollowCountsUseSingularFollowing(t *testing.T) {
	var got capture
	c, _ := newTestClient(t, recording(&got, `{"count":7}`))

	if _, err := c.FollowerCount(context.Background(), ""); err != nil {
		t.Fatalf("FollowerCount: %v", err)
	}
	if got.Path != "/profileRelations/profiles/me/followers/count" {
		t.Errorf("follower path = %q", got.Path)
	}

	if _, err := c.FollowingCount(context.Background(), ""); err != nil {
		t.Fatalf("FollowingCount: %v", err)
	}
	if got.Path != "/profileRelations/profiles/me/following/count" {
		t.Errorf("following path = %q, want the singular form", got.Path)
	}
}

// The official client omits skip/take entirely unless take is set, which is a
// different request from skip=0&take=0.
func TestEventsOmitsPagingWhenTakeIsZero(t *testing.T) {
	var got capture
	c, _ := newTestClient(t, recording(&got, `{"data":[],"totalRecords":0}`))

	if _, err := c.Events(context.Background(), EventsOptions{}); err != nil {
		t.Fatalf("Events: %v", err)
	}
	if got.Query.Has("take") || got.Query.Has("skip") {
		t.Errorf("paging sent when unset: %v", got.Query)
	}

	if _, err := c.Events(context.Background(), EventsOptions{Skip: 24, Take: 24, IsPast: true}); err != nil {
		t.Fatalf("Events(paged): %v", err)
	}
	if got.Query.Get("take") != "24" || got.Query.Get("skip") != "24" {
		t.Errorf("paging = %v", got.Query)
	}
	if got.Query.Get("isPast") != "true" {
		t.Errorf("isPast not sent: %v", got.Query)
	}
}

// Part names are fixed by the server: file, galleryId, galleryTypeId.
func TestUploadPhotoMultipartShape(t *testing.T) {
	var (
		gotParts  = map[string]string{}
		gotFile   string
		gotCT     string
		gotMethod string
	)
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		_, params, err := mime.ParseMediaType(gotCT)
		if err != nil {
			t.Errorf("content type: %v", err)
			return
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			if p.FileName() != "" {
				gotFile = p.FormName() + ":" + p.FileName()
			} else {
				gotParts[p.FormName()] = string(b)
			}
		}
		_, _ = w.Write([]byte(`{"id":"f1"}`))
	}))

	_, err := c.UploadPhoto(context.Background(), "g1", 1,
		OutgoingFile{Name: "pic.jpg", MimeType: "image/jpeg", Data: []byte("bytes")})
	if err != nil {
		t.Fatalf("UploadPhoto: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s", gotMethod)
	}
	if !strings.HasPrefix(gotCT, "multipart/form-data") {
		t.Errorf("content type = %q", gotCT)
	}
	if gotFile != "file:pic.jpg" {
		t.Errorf("file part = %q, want the part named \"file\"", gotFile)
	}
	if gotParts["galleryId"] != "g1" || gotParts["galleryTypeId"] != "1" {
		t.Errorf("parts = %v", gotParts)
	}
}

// A file with no name still needs one on the wire.
func TestUploadPhotoDefaultsFilename(t *testing.T) {
	var gotFile string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			if p.FileName() != "" {
				gotFile = p.FileName()
			}
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	if _, err := c.UploadPhoto(context.Background(), "", 1, OutgoingFile{Data: []byte("x")}); err != nil {
		t.Fatalf("UploadPhoto: %v", err)
	}
	if gotFile == "" {
		t.Error("no filename sent for an unnamed file")
	}
}

// Reading your own media bypasses the cache; reading someone else's does not.
func TestMediaNoCacheOnlyForSelf(t *testing.T) {
	var got capture
	c, _ := newTestClient(t, recording(&got, `{"data":[],"totalRecords":0}`))

	if _, err := c.ProfileFiles(context.Background(), "me"); err != nil {
		t.Fatalf("ProfileFiles(self): %v", err)
	}
	if got.Query.Get("noCache") != "true" {
		t.Errorf("self read should set noCache, got %v", got.Query)
	}

	if _, err := c.ProfileFiles(context.Background(), "them"); err != nil {
		t.Fatalf("ProfileFiles(other): %v", err)
	}
	if got.Query.Has("noCache") {
		t.Errorf("other read should not set noCache, got %v", got.Query)
	}
}

func TestSetPhotoPositionBodyIsValueWrapper(t *testing.T) {
	var got capture
	c, _ := newTestClient(t, recording(&got, ""))
	if err := c.SetPhotoPosition(context.Background(), "g1", "f1", 1); err != nil {
		t.Fatalf("SetPhotoPosition: %v", err)
	}
	if got.Method != http.MethodPut {
		t.Errorf("method = %s", got.Method)
	}
	if !strings.Contains(got.Body, `"value":1`) {
		t.Errorf("body = %s, want a {value:n} wrapper", got.Body)
	}
}
