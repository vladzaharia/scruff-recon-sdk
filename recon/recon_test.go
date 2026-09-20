package recon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/vladzaharia/scruff-recon-sdk/core"
)

// The API returns accessToken already prefixed with "Bearer ". Getting this
// wrong produces "Bearer Bearer", which the profile service accepts and the
// messaging and SignalR services reject with an empty-body 401 — so the bug
// hides until it matters.
func TestStripBearer(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"prefixed as the API returns it", "Bearer eyJhbGciOi", "eyJhbGciOi"},
		{"bare token untouched", "eyJhbGciOi", "eyJhbGciOi"},
		{"doubled prefix", "Bearer Bearer eyJhbGciOi", "Bearer eyJhbGciOi"},
		{"surrounding whitespace", "  Bearer eyJhbGciOi  ", "eyJhbGciOi"},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripBearer(tc.in); got != tc.want {
				t.Errorf("stripBearer(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestApplyTokensNormalisesPrefixAndExpiry(t *testing.T) {
	var s Session
	applyTokens(&s, &TokenSet{
		AccessToken:        "Bearer eyJtoken",
		AccessTokenExpiry:  "2026-09-20T01:33:45Z",
		RefreshToken:       "refresh-1",
		RefreshTokenExpiry: "2026-10-04T01:18:45Z",
		AccountID:          "acct",
		SessionID:          "sess",
		ProfileIDs:         []string{"profile-a", "profile-b"},
		MembershipLevelID:  1,
	})

	if s.AccessToken != "eyJtoken" {
		t.Errorf("AccessToken = %q, want the prefix stripped", s.AccessToken)
	}
	if s.ProfileID != "profile-a" {
		t.Errorf("ProfileID = %q, want the first of profileIds", s.ProfileID)
	}
	if s.AccessTokenExpiry.IsZero() || s.RefreshTokenExpiry.IsZero() {
		t.Error("expiries were not parsed")
	}
	if !s.Valid() {
		t.Error("session should be valid")
	}
}

// A refresh response omits profileIds; it must not clobber the active profile.
func TestApplyTokensKeepsExistingProfile(t *testing.T) {
	s := Session{ProfileID: "original"}
	applyTokens(&s, &TokenSet{AccessToken: "Bearer x", ProfileIDs: []string{"other"}})
	if s.ProfileID != "original" {
		t.Errorf("ProfileID = %q, want the existing profile preserved", s.ProfileID)
	}
}

// newTestClient wires a Client to a test server with a non-expiring token.
func newTestClient(t *testing.T, h http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(Session{
		AccessToken:       "test-token",
		AccessTokenExpiry: time.Now().Add(time.Hour),
		ProfileID:         "me",
		AccountID:         "acct",
		SessionID:         "sess",
	}, WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	return c, srv
}

func TestAuthHeaderHasExactlyOneBearer(t *testing.T) {
	var got string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[],"totalRecords":0}`))
	}))
	if _, err := c.Conversations(context.Background()); err != nil {
		t.Fatalf("Conversations: %v", err)
	}
	if got != "Bearer test-token" {
		t.Errorf("Authorization = %q, want exactly one Bearer prefix", got)
	}
}

// A session persisted by an older client may still carry the prefix.
func TestNewNormalisesStoredPrefix(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c := New(Session{
		AccessToken:       "Bearer stored-with-prefix",
		AccessTokenExpiry: time.Now().Add(time.Hour),
		ProfileID:         "me",
	}, WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))

	if _, err := c.Conversations(context.Background()); err != nil {
		t.Fatalf("Conversations: %v", err)
	}
	if got != "Bearer stored-with-prefix" {
		t.Errorf("Authorization = %q, want a single prefix", got)
	}
}

// culture is required on every call.
func TestCultureIsAlwaysSent(t *testing.T) {
	var paths []string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.String())
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	ctx := context.Background()
	_, _ = c.Conversations(ctx)
	_, _ = c.Messages(ctx, "conv", MessagesOptions{})
	_, _ = c.Profile(ctx, "peer")

	if len(paths) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(paths))
	}
	for _, p := range paths {
		if !strings.Contains(p, "culture=en") {
			t.Errorf("request %q is missing culture=en", p)
		}
	}
}

func TestMessagesUsesBeforeCursorNotOffset(t *testing.T) {
	var q url.Values
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		_, _ = w.Write([]byte(`{"data":[],"totalRecords":0}`))
	}))
	_, err := c.Messages(context.Background(), "conv", MessagesOptions{
		Before: "2026-09-13T18:06:37.76Z", Limit: 50,
	})
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if q.Get("before") != "2026-09-13T18:06:37.76Z" {
		t.Errorf("before = %q, want the ISO cursor", q.Get("before"))
	}
	if q.Get("take") != "50" {
		t.Errorf("take = %q, want 50", q.Get("take"))
	}
	for _, forbidden := range []string{"skip", "offset", "page"} {
		if q.Has(forbidden) {
			t.Errorf("must not send %q — this API has no offset paging", forbidden)
		}
	}
	if q.Get("imageSizes") != DefaultImageSizes {
		t.Errorf("imageSizes = %q, want %q", q.Get("imageSizes"), DefaultImageSizes)
	}
}

// The profile endpoint must request only real size codes. 661 is a profile
// version, not a rendition.
func TestProfileRequestsOnlyValidImageSizes(t *testing.T) {
	var q url.Values
	var path string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q, path = r.URL.Query(), r.URL.Path
		_, _ = w.Write([]byte(`{"id":"peer","version":661}`))
	}))
	if _, err := c.Profile(context.Background(), "peer"); err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if got := q.Get("imageSizes"); got != "102,104" {
		t.Errorf("imageSizes = %q, want 102,104", got)
	}
	if strings.Contains(q.Get("imageSizes"), "661") {
		t.Error("661 is a profile version, not an image size")
	}
	if !strings.HasSuffix(path, "/profile/profiles/peer/1") {
		t.Errorf("path = %q, want the version-1 redirect form", path)
	}
}

// Recon has no contentTypeId in the send body, and files/attachments must be
// present as empty arrays rather than omitted.
func TestSendTextBodyShape(t *testing.T) {
	var body map[string]any
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"id":"m1","text":"hi"}`))
	}))
	if _, err := c.SendText(context.Background(), "conv", "hi"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if body["text"] != "hi" {
		t.Errorf("text = %v, want hi", body["text"])
	}
	for _, k := range []string{"files", "attachments"} {
		v, ok := body[k]
		if !ok {
			t.Errorf("%q must be present even when empty", k)
			continue
		}
		if arr, ok := v.([]any); !ok || len(arr) != 0 {
			t.Errorf("%q = %v, want an empty array", k, v)
		}
	}
	if _, ok := body["contentTypeId"]; ok {
		t.Error("contentTypeId must not be sent — the server assigns it")
	}
}

// PascalCase here is correct and load-bearing.
func TestMarkReadUsesPascalCaseKey(t *testing.T) {
	var body map[string]any
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusNoContent)
	}))
	when := time.Date(2026, 9, 13, 18, 6, 37, 0, time.UTC)
	if err := c.MarkRead(context.Background(), "conv", when); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if _, ok := body["LastMessageReadDate"]; !ok {
		t.Errorf("body = %v, want the PascalCase LastMessageReadDate key", body)
	}
}

func TestCreateConversationUsesPascalCaseParticipants(t *testing.T) {
	var body map[string]any
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"id":"conv"}`))
	}))
	if _, err := c.CreateConversation(context.Background(), "peer"); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	ps, ok := body["participants"].([]any)
	if !ok || len(ps) != 2 {
		t.Fatalf("participants = %v, want two entries", body["participants"])
	}
	first := ps[0].(map[string]any)
	if _, ok := first["ProfileId"]; !ok {
		t.Errorf("participant = %v, want the PascalCase ProfileId key", first)
	}
}

// An existing thread comes back as a conflict plus a Location header.
func TestCreateConversationFollowsLocationOnConflict(t *testing.T) {
	var followed string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Header().Set("Location", "/api/messaging/profiles/me/conversations/existing")
			w.WriteHeader(http.StatusConflict)
			return
		}
		followed = r.URL.Path
		_, _ = w.Write([]byte(`{"id":"existing"}`))
	}))
	conv, err := c.CreateConversation(context.Background(), "peer")
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	if conv.ID != "existing" {
		t.Errorf("id = %q, want the existing conversation", conv.ID)
	}
	if !strings.HasSuffix(followed, "/conversations/existing") {
		t.Errorf("followed %q, want the Location target", followed)
	}
}

func TestSendMediaMultipartShape(t *testing.T) {
	var names []string
	var attachments string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ParseMultipartForm: %v", err)
		}
		for k := range r.MultipartForm.Value {
			names = append(names, k)
		}
		if v := r.MultipartForm.Value["attachments"]; len(v) > 0 {
			attachments = v[0]
		}
		if len(r.MultipartForm.File["files"]) != 2 {
			t.Errorf("files parts = %d, want 2 (repeated part name)",
				len(r.MultipartForm.File["files"]))
		}
		_, _ = w.Write([]byte(`{"id":"m1"}`))
	}))
	_, err := c.SendMedia(context.Background(), "conv", "caption", []OutgoingFile{
		{Name: "a.jpg", MimeType: "image/jpeg", Data: []byte{1}},
		{Name: "b.png", MimeType: "image/png", Data: []byte{2}},
	})
	if err != nil {
		t.Fatalf("SendMedia: %v", err)
	}
	if attachments != "[]" {
		t.Errorf("attachments = %q, want the JSON string %q", attachments, "[]")
	}
	if !contains(names, "text") {
		t.Errorf("fields = %v, want a text part", names)
	}
}

func TestConversationPeerExcludesSelf(t *testing.T) {
	c := Conversation{
		ParticipantCount: 2,
		Participants:     []Participant{{ProfileID: "peer-1"}},
	}
	if got := c.Peer(); got != "peer-1" {
		t.Errorf("Peer() = %q, want peer-1", got)
	}
	if got := (Conversation{}).Peer(); got != "" {
		t.Errorf("Peer() on empty = %q, want empty", got)
	}
}

func TestAttachmentLargestURLPrefersLastThenDownload(t *testing.T) {
	a := Attachment{Files: []MediaFile{
		{ImageSize: 100, URL: "small"},
		{ImageSize: 104, URL: "large"},
	}}
	if got := a.LargestURL(); got != "large" {
		t.Errorf("= %q, want large (files are ordered small to large)", got)
	}
	b := Attachment{DownloadURL: "fallback"}
	if got := b.LargestURL(); got != "fallback" {
		t.Errorf("= %q, want the DownloadURL fallback", got)
	}
}

func TestProfileAvatarURLPicksLargest(t *testing.T) {
	p := Profile{PrimaryImageFiles: []MediaFile{
		{ImageSize: 100, URL: "a"},
		{ImageSize: 104, URL: "b"},
		{ImageSize: 102, URL: "c"},
	}}
	if got := p.AvatarURL(); got != "b" {
		t.Errorf("= %q, want the 104 rendition", got)
	}
	if got := (Profile{}).AvatarURL(); got != "" {
		t.Errorf("= %q, want empty when there is no avatar", got)
	}
}

func TestErrorsAreAPIErrors(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized) // empty body, as the real API does
	}))
	_, err := c.Conversations(context.Background())
	if err == nil {
		t.Fatal("want an error")
	}
	apiErr, ok := core.AsAPIError(err)
	if !ok {
		t.Fatalf("err = %T, want *core.APIError", err)
	}
	if !apiErr.IsUnauthorized() {
		t.Errorf("status = %d, want 401", apiErr.StatusCode)
	}
}

// An expired access token with no refresh token must fail distinguishably, so
// callers can prompt for a fresh login rather than retrying forever.
func TestExpiredSessionWithoutRefreshTokenIsAuthExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should be made without a usable token")
	}))
	defer srv.Close()

	c := New(Session{
		AccessToken:       "stale",
		AccessTokenExpiry: time.Now().Add(-time.Hour),
		ProfileID:         "me",
	}, WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))

	_, err := c.Conversations(context.Background())
	if !IsAuthExpired(err) {
		t.Fatalf("err = %v, want AuthExpiredError", err)
	}
}

func TestRefreshIsAttemptedAndPersisted(t *testing.T) {
	var refreshCalls, authHeaderOnRefresh int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "refreshTokens") {
			refreshCalls++
			if r.Header.Get("Authorization") != "" {
				authHeaderOnRefresh++
			}
			_, _ = w.Write([]byte(`{
				"accessToken":"Bearer fresh","accessTokenExpiry":"2099-01-01T00:00:00Z",
				"refreshToken":"r2","refreshTokenExpiry":"2099-01-01T00:00:00Z",
				"accountId":"acct","sessionId":"sess"}`))
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fresh" {
			t.Errorf("Authorization = %q, want the refreshed token", got)
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	c := New(Session{
		AccessToken:        "stale",
		AccessTokenExpiry:  time.Now().Add(-time.Hour),
		RefreshToken:       "r1",
		RefreshTokenExpiry: time.Now().Add(24 * time.Hour),
		ProfileID:          "me", AccountID: "acct", SessionID: "sess",
	}, WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))

	saved := 0
	c.OnSessionUpdate(func(context.Context, Session) error { saved++; return nil })

	if _, err := c.Conversations(context.Background()); err != nil {
		t.Fatalf("Conversations: %v", err)
	}
	if refreshCalls != 1 {
		t.Errorf("refresh calls = %d, want 1", refreshCalls)
	}
	if authHeaderOnRefresh != 0 {
		t.Error("refreshTokens must be sent WITHOUT an Authorization header")
	}
	if saved != 1 {
		t.Errorf("OnSessionUpdate fired %d times, want 1", saved)
	}
	if c.Session().AccessToken != "fresh" {
		t.Errorf("stored token = %q, want the prefix stripped", c.Session().AccessToken)
	}
}

func TestExpiredRefreshTokenIsAuthExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not call the network with an expired refresh token")
	}))
	defer srv.Close()

	c := New(Session{
		AccessToken:        "stale",
		AccessTokenExpiry:  time.Now().Add(-time.Hour),
		RefreshToken:       "r1",
		RefreshTokenExpiry: time.Now().Add(-time.Hour),
		ProfileID:          "me", AccountID: "a", SessionID: "s",
	}, WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))

	if _, err := c.Conversations(context.Background()); !IsAuthExpired(err) {
		t.Fatalf("err = %v, want AuthExpiredError", err)
	}
}

func TestSplitFramesHandlesBatchedFrames(t *testing.T) {
	data := []byte("{\"a\":1}\x1e{\"b\":2}\x1e")
	got := splitFrames(data)
	if len(got) != 2 {
		t.Fatalf("frames = %d, want 2 — one message may carry several frames", len(got))
	}
	if string(got[0]) != `{"a":1}` || string(got[1]) != `{"b":2}` {
		t.Errorf("frames = %q", got)
	}
	if n := len(splitFrames([]byte("\x1e\x1e"))); n != 0 {
		t.Errorf("empty frames = %d, want 0", n)
	}
}

// Realtime and REST disagree on field names for the same concepts.
func TestReceiveMessageUsesWireFieldNames(t *testing.T) {
	payload := []byte(`{
		"conversationId":"c1","messageId":"m1",
		"profileId":"sender-1","messageText":"hello",
		"sentDate":"2026-09-13T18:06:37Z","attachmentCount":2}`)

	var ev struct {
		ConversationID  string `json:"conversationId"`
		MessageID       string `json:"messageId"`
		ProfileID       string `json:"profileId"`
		MessageText     string `json:"messageText"`
		SentDate        string `json:"sentDate"`
		AttachmentCount int    `json:"attachmentCount"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ev.ProfileID != "sender-1" {
		t.Error("the realtime sender field is profileId, not senderProfileId")
	}
	if ev.MessageText != "hello" {
		t.Error("the realtime body field is messageText, not text")
	}

	// And the REST DTO uses the other spelling for the same two concepts.
	var m Message
	if err := json.Unmarshal([]byte(
		`{"senderProfileId":"sender-1","text":"hello","contentTypeId":1}`), &m); err != nil {
		t.Fatalf("unmarshal REST message: %v", err)
	}
	if m.SenderProfileID != "sender-1" || m.Text != "hello" || m.ContentTypeID != 1 {
		t.Errorf("REST message decoded wrong: %+v", m)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
