package scruff

// Tests for the user-facing surface beyond messaging.
//
// These endpoints are largely [client]-derived and the writes have never been
// sent to the live API, so these tests assert the request we construct rather
// than server behaviour we have not seen. No test here performs a live write.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type scap struct {
	Method string
	Path   string
	Query  url.Values
	Form   url.Values
	Body   string
}

func srecording(got *scap, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(b))
		*got = scap{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(),
			Form: form, Body: string(b)}
		if body == "" {
			body = "{}"
		}
		_, _ = w.Write([]byte(body))
	})
}

func newSocialClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(Session{
		DeviceID:   "droid-000102030405060708090A0B0C0D0E0F10",
		HardwareID: "droid-0011223344556677",
		ProfileID:  "1001",
	}, WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
}

// Block and Hide are the same endpoint distinguished only by a boolean, and
// the polarity is counterintuitive: hide=true hides, hide=false BLOCKS.
// Inverting it would block someone the user only meant to hide.
func TestBlockAndHideSendOppositeFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*Client) error
		want string
	}{
		{"Block", func(c *Client) error { return c.Block(context.Background(), "2002") }, "false"},
		{"Hide", func(c *Client) error { return c.Hide(context.Background(), "2002") }, "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got scap
			c := newSocialClient(t, srecording(&got, ""))
			if err := tc.call(c); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if got.Method != http.MethodPost || got.Path != "/app/block" {
				t.Errorf("%s %s", got.Method, got.Path)
			}
			if got.Form.Get("hide") != tc.want {
				t.Errorf("%s sent hide=%q, want %q", tc.name, got.Form.Get("hide"), tc.want)
			}
			if got.Form.Get("recipient") != "2002" {
				t.Errorf("recipient = %q", got.Form.Get("recipient"))
			}
		})
	}
}

// DELETE /app/block with no parameters wipes every block on the account. An
// empty id must never reach the wire as "clear everything".
func TestUnblockRefusesEmptyIDRatherThanClearingAll(t *testing.T) {
	var got scap
	var called bool
	c := newSocialClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		srecording(&got, "").ServeHTTP(w, r)
	}))

	if err := c.Unblock(context.Background(), ""); err == nil {
		t.Fatal("Unblock(\"\") must fail, not clear every block")
	}
	if called {
		t.Fatal("Unblock(\"\") sent a request; that request clears ALL blocks")
	}

	// The explicit form is still available.
	if err := c.UnblockAll(context.Background()); err != nil {
		t.Fatalf("UnblockAll: %v", err)
	}
	if !called || got.Method != http.MethodDelete || got.Path != "/app/block" {
		t.Errorf("UnblockAll did not send the parameterless DELETE: %+v", got)
	}
	if len(got.Query) != 0 {
		// Identity params are injected by the authenticator; what matters is
		// that no target was scoped.
		if got.Query.Has("target_id") {
			t.Error("UnblockAll should not scope a target")
		}
	}
}

func TestUnblockScopesTarget(t *testing.T) {
	var got scap
	c := newSocialClient(t, srecording(&got, ""))
	if err := c.Unblock(context.Background(), "2002"); err != nil {
		t.Fatalf("Unblock: %v", err)
	}
	if got.Query.Get("target_id") != "2002" {
		t.Errorf("target_id = %q, want the scoped form", got.Query.Get("target_id"))
	}
}

func TestWoofBodyShape(t *testing.T) {
	var got scap
	c := newSocialClient(t, srecording(&got, ""))
	if err := c.Woof(context.Background(), "2002", ""); err != nil {
		t.Fatalf("Woof: %v", err)
	}
	if got.Method != http.MethodPost || got.Path != "/app/poke" {
		t.Errorf("%s %s, want POST /app/poke", got.Method, got.Path)
	}
	if got.Form.Get("recipient") != "2002" {
		t.Errorf("recipient = %q", got.Form.Get("recipient"))
	}
	// moment_id is optional and must be omitted, not sent empty.
	if got.Form.Has("moment_id") {
		t.Errorf("moment_id sent when not supplied: %s", got.Body)
	}

	if err := c.Woof(context.Background(), "2002", "m1"); err != nil {
		t.Fatalf("Woof(moment): %v", err)
	}
	if got.Form.Get("moment_id") != "m1" {
		t.Errorf("moment_id = %q", got.Form.Get("moment_id"))
	}
}

// A look is an explicit act; Profile() must not imply one. This pins the two
// as different endpoints so they cannot be conflated.
func TestRecordLookIsSeparateFromProfileFetch(t *testing.T) {
	var paths []string
	c := newSocialClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		_, _ = w.Write([]byte(`{"results":[{"id":2002}]}`))
	}))

	if _, err := c.Profile(context.Background(), "2002"); err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if len(paths) != 1 || !strings.HasPrefix(paths[0], "GET /app/profile") {
		t.Fatalf("Profile made unexpected calls: %v", paths)
	}
	if strings.Contains(paths[0], "/view") {
		t.Error("Profile() must not record a look")
	}

	if err := c.RecordLook(context.Background(), "2002"); err != nil {
		t.Fatalf("RecordLook: %v", err)
	}
	if paths[1] != "POST /app/profile/view" {
		t.Errorf("RecordLook = %q", paths[1])
	}
}

// EditProfile always stamps request_guid, which the endpoint requires.
func TestEditProfileAddsRequestGUID(t *testing.T) {
	var got scap
	c := newSocialClient(t, srecording(&got, ""))
	if err := c.EditProfile(context.Background(), map[string]any{"about": "hi"}); err != nil {
		t.Fatalf("EditProfile: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got.Body), &body); err != nil {
		t.Fatalf("body is not JSON: %s", got.Body)
	}
	if body["about"] != "hi" {
		t.Errorf("about = %v", body["about"])
	}
	if body["request_guid"] == nil || body["request_guid"] == "" {
		t.Error("request_guid missing; the endpoint requires it")
	}
}

// A zero value is a meaningful edit (e.g. clearing an enum), so the caller's
// map must be passed through verbatim rather than filtered by omitempty.
func TestEditProfileKeepsZeroValues(t *testing.T) {
	var got scap
	c := newSocialClient(t, srecording(&got, ""))
	err := c.EditProfile(context.Background(), map[string]any{
		"hide_age":  false,
		"ethnicity": 0,
		"about":     "",
	})
	if err != nil {
		t.Fatalf("EditProfile: %v", err)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(got.Body), &body)
	for _, k := range []string{"hide_age", "ethnicity", "about"} {
		if _, ok := body[k]; !ok {
			t.Errorf("%s dropped; a zero value is a deliberate edit", k)
		}
	}
}

// EditProfile must not mutate the caller's map (it adds request_guid).
func TestEditProfileDoesNotMutateCallerMap(t *testing.T) {
	var got scap
	c := newSocialClient(t, srecording(&got, ""))
	in := map[string]any{"about": "hi"}
	if err := c.EditProfile(context.Background(), in); err != nil {
		t.Fatalf("EditProfile: %v", err)
	}
	if _, ok := in["request_guid"]; ok {
		t.Error("EditProfile mutated the caller's map")
	}
}

func TestFavoriteFolderLifecyclePaths(t *testing.T) {
	for _, tc := range []struct {
		name   string
		call   func(*Client) error
		method string
	}{
		{"create", func(c *Client) error { return c.CreateFavoriteFolder(context.Background(), "n") }, http.MethodPost},
		{"rename", func(c *Client) error { return c.RenameFavoriteFolder(context.Background(), "1", "n") }, http.MethodPut},
		{"delete", func(c *Client) error { return c.DeleteFavoriteFolder(context.Background(), "1") }, http.MethodDelete},
	} {
		var got scap
		c := newSocialClient(t, srecording(&got, ""))
		if err := tc.call(c); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got.Method != tc.method || got.Path != "/app/favorite/folders" {
			t.Errorf("%s: %s %s, want %s /app/favorite/folders", tc.name, got.Method, got.Path, tc.method)
		}
	}
}

// SaveTrip is create-or-update: POST without an id, PUT with one.
func TestSaveTripSwitchesMethodOnID(t *testing.T) {
	var got scap
	c := newSocialClient(t, srecording(&got, ""))

	if err := c.SaveTrip(context.Background(), TripOptions{Location: "Berlin"}); err != nil {
		t.Fatalf("SaveTrip(create): %v", err)
	}
	if got.Method != http.MethodPost {
		t.Errorf("create used %s, want POST", got.Method)
	}
	if got.Form.Has("id") {
		t.Error("create should not send an id")
	}

	if err := c.SaveTrip(context.Background(), TripOptions{ID: "7", Location: "Berlin"}); err != nil {
		t.Fatalf("SaveTrip(update): %v", err)
	}
	if got.Method != http.MethodPut {
		t.Errorf("update used %s, want PUT", got.Method)
	}
	if got.Form.Get("id") != "7" {
		t.Errorf("id = %q", got.Form.Get("id"))
	}
}

// Sharing happens through a chat message; only the unshare direction is a
// permissions call in this build.
func TestUnshareAlbumIsDelete(t *testing.T) {
	var got scap
	c := newSocialClient(t, srecording(&got, ""))
	if err := c.UnshareAlbum(context.Background(), "5", []string{"2002", "3003"}); err != nil {
		t.Fatalf("UnshareAlbum: %v", err)
	}
	if got.Method != http.MethodDelete || got.Path != "/app/albums/permissions" {
		t.Errorf("%s %s", got.Method, got.Path)
	}
	if got.Query.Get("album_id") != "5" {
		t.Errorf("album_id = %q", got.Query.Get("album_id"))
	}
	if len(got.Query["target_ids[]"]) != 2 {
		t.Errorf("target_ids[] = %v, want two repeated entries", got.Query["target_ids[]"])
	}
}

func TestGridHelpersUseDocumentedPaths(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*Client) error
		want string
	}{
		{"WoofsReceived", func(c *Client) error { _, e := c.WoofsReceived(context.Background(), GridOptions{}); return e }, "/app/woofs/incoming"},
		{"WoofsSent", func(c *Client) error { _, e := c.WoofsSent(context.Background(), GridOptions{}); return e }, "/app/woofs/outgoing"},
		{"ViewedYou", func(c *Client) error { _, e := c.ViewedYou(context.Background(), GridOptions{}); return e }, "/app/viewers/incoming"},
		{"YouViewed", func(c *Client) error { _, e := c.YouViewed(context.Background(), GridOptions{}); return e }, "/app/viewers/outgoing"},
		{"Favorites", func(c *Client) error { _, e := c.Favorites(context.Background(), GridOptions{}); return e }, "/app/favorite"},
		{"Blocks", func(c *Client) error { _, e := c.Blocks(context.Background(), GridOptions{}); return e }, "/app/block"},
		{"AlbumsReceived", func(c *Client) error { _, e := c.AlbumsReceived(context.Background(), GridOptions{}); return e }, "/app/albums/received"},
		{"RSVPs", func(c *Client) error { _, e := c.RSVPs(context.Background(), GridOptions{}); return e }, "/app/events/rsvps"},
	} {
		var got scap
		c := newSocialClient(t, srecording(&got, `{"results":[]}`))
		if err := tc.call(c); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got.Path != tc.want {
			t.Errorf("%s path = %q, want %q", tc.name, got.Path, tc.want)
		}
	}
}
