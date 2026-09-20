package recon

// Tests for the surface beyond messaging and discovery.
//
// These endpoints are [client]-derived: read from Recon's own web bundle, never
// exercised against the live API. So what is asserted here is precisely what we
// actually know — the request we construct — rather than server behaviour we
// have not observed. Each test pins method, path and body shape against
// docs/api/recon.md; none of them performs a live write.

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

// capture records the one request a call makes.
type capture struct {
	Method string
	Path   string
	Query  url.Values
	Body   string
}

// recording returns a handler that captures the request and replies with body.
func recording(got *capture, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*got = capture{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Body: string(b)}
		if body == "" {
			body = "{}"
		}
		_, _ = w.Write([]byte(body))
	})
}

func TestAccountPreferencesRoundTrip(t *testing.T) {
	var got capture
	c, _ := newTestClient(t, recording(&got,
		`{"culture":"en-GB","showMetricDistance":false,"showMetricHeight":true,"gmtOffsetSeconds":-25200}`))

	p, err := c.AccountPreferences(context.Background())
	if err != nil {
		t.Fatalf("AccountPreferences: %v", err)
	}
	if got.Path != "/account/accounts/acct/preferences" {
		t.Errorf("path = %q", got.Path)
	}
	// The two metric flags are independent; a naive single "useMetric" would
	// collapse this case, which is the locale default in several countries.
	if p.ShowMetricDistance || !p.ShowMetricHeight {
		t.Errorf("metric flags = (%v,%v), want (false,true) decoded independently",
			p.ShowMetricDistance, p.ShowMetricHeight)
	}
	if p.GMTOffsetSeconds != -25200 {
		t.Errorf("gmtOffsetSeconds = %d", p.GMTOffsetSeconds)
	}
}

func TestSetAccountPreferencesIsPut(t *testing.T) {
	var got capture
	c, _ := newTestClient(t, recording(&got, ""))
	err := c.SetAccountPreferences(context.Background(), AccountPreferences{ShowMetricHeight: true})
	if err != nil {
		t.Fatalf("SetAccountPreferences: %v", err)
	}
	if got.Method != http.MethodPut {
		t.Errorf("method = %s, want PUT", got.Method)
	}
	if !strings.Contains(got.Body, `"showMetricHeight":true`) {
		t.Errorf("body = %s", got.Body)
	}
}

// null means "no filter" and is not the same as a zero value, so the filter DTO
// must round-trip absent fields as null rather than as 0/false.
func TestSearchFiltersEncodeNullNotZero(t *testing.T) {
	var got capture
	c, _ := newTestClient(t, recording(&got, ""))

	age := 70
	if err := c.SetSearchFilters(context.Background(), SearchFilters{AgeMax: &age}); err != nil {
		t.Fatalf("SetSearchFilters: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got.Body), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["ageMax"] != float64(70) {
		t.Errorf("ageMax = %v, want 70", body["ageMax"])
	}
	v, ok := body["ageMin"]
	if !ok {
		t.Fatal("ageMin missing entirely; it must serialise as explicit null")
	}
	if v != nil {
		t.Errorf("ageMin = %v, want null (an unset filter must not become 0)", v)
	}
}

func TestDeleteSearchFiltersUsesDelete(t *testing.T) {
	var got capture
	c, _ := newTestClient(t, recording(&got, ""))
	if err := c.DeleteSearchFilters(context.Background()); err != nil {
		t.Fatalf("DeleteSearchFilters: %v", err)
	}
	if got.Method != http.MethodDelete || got.Path != "/profileSearch/profiles/me/defaultFilters" {
		t.Errorf("%s %s", got.Method, got.Path)
	}
}

// The client only ever sends "replace", and /rowVersion must accompany a patch
// for optimistic concurrency. Dropping it is how concurrent edits clobber.
func TestPatchProfileSendsJSONPatchWithRowVersion(t *testing.T) {
	var got capture
	c, _ := newTestClient(t, recording(&got, `{"id":"p1","version":662}`))

	_, err := c.PatchProfile(context.Background(), "p1", []PatchOp{
		Replace("/shortText", "hello"),
		PatchRowVersion("abc123"),
	})
	if err != nil {
		t.Fatalf("PatchProfile: %v", err)
	}
	if got.Method != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", got.Method)
	}
	var ops []PatchOp
	if err := json.Unmarshal([]byte(got.Body), &ops); err != nil {
		t.Fatalf("body is not a JSON Patch array: %v (%s)", err, got.Body)
	}
	if len(ops) != 2 {
		t.Fatalf("ops = %d, want 2", len(ops))
	}
	var sawRowVersion bool
	for _, o := range ops {
		if o.Op != "replace" {
			t.Errorf("op = %q, want replace", o.Op)
		}
		if o.Path == "/rowVersion" {
			sawRowVersion = true
		}
	}
	if !sawRowVersion {
		t.Error("no /rowVersion op; optimistic concurrency would be lost")
	}
}

// The endpoint is not symmetric: ids go lower-first in the path.
func TestDistanceSortsLocationIDsLowerFirst(t *testing.T) {
	var got capture
	c, _ := newTestClient(t, recording(&got, `{"distanceMetres":4200}`))

	d, err := c.Distance(context.Background(), "900", "100")
	if err != nil {
		t.Fatalf("Distance: %v", err)
	}
	if d != 4200 {
		t.Errorf("distance = %d", d)
	}
	want := "/location/granularLocations/100/distances/900"
	if got.Path != want {
		t.Errorf("path = %q, want %q (ids sorted lower-first)", got.Path, want)
	}
}

// Sorting is numeric, not lexical: "100" < "900" either way, but "1000" is
// lexically less than "900" while being numerically greater.
func TestDistanceSortsNumericallyNotLexically(t *testing.T) {
	var got capture
	c, _ := newTestClient(t, recording(&got, `{"distanceMetres":1}`))
	if _, err := c.Distance(context.Background(), "1000", "900"); err != nil {
		t.Fatalf("Distance: %v", err)
	}
	want := "/location/granularLocations/900/distances/1000"
	if got.Path != want {
		t.Errorf("path = %q, want %q", got.Path, want)
	}
}

// The trailing slash is part of the path; without it the endpoint 404s.
func TestReportCategoriesKeepsTrailingSlash(t *testing.T) {
	var got capture
	c, _ := newTestClient(t, recording(&got, `{"data":[{"id":13,"name":"Other"}]}`))

	cats, err := c.ReportCategories(context.Background())
	if err != nil {
		t.Fatalf("ReportCategories: %v", err)
	}
	if got.Path != "/contentReview/reportCategories/" {
		t.Errorf("path = %q, want a trailing slash", got.Path)
	}
	if len(cats) != 1 || cats[0].ID != 13 {
		t.Errorf("categories = %+v", cats)
	}
}

// reportedByAccountId defaults from the session; the capital D in
// mediaMetaDataIds is the wire spelling and must not be "corrected".
func TestReportProfileBodyShape(t *testing.T) {
	var got capture
	c, _ := newTestClient(t, recording(&got, ""))

	err := c.ReportProfile(context.Background(), "them", Report{
		ReportCategoryID: 13,
		ReportReason:     "spam",
		MediaMetaDataIDs: []string{"m1"},
	})
	if err != nil {
		t.Fatalf("ReportProfile: %v", err)
	}
	if got.Path != "/contentReview/profiles/them/report" {
		t.Errorf("path = %q", got.Path)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got.Body), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["reportedByAccountId"] != "acct" {
		t.Errorf("reportedByAccountId = %v, want it defaulted from the session", body["reportedByAccountId"])
	}
	if _, ok := body["mediaMetaDataIds"]; !ok {
		t.Errorf("mediaMetaDataIds missing (capital D is the wire spelling): %s", got.Body)
	}
}

func TestPublishLocationDefaultsProfileID(t *testing.T) {
	var got capture
	c, _ := newTestClient(t, recording(&got, ""))

	err := c.PublishLocation(context.Background(), GeoLocation{Latitude: 51.5, Longitude: -0.1})
	if err != nil {
		t.Fatalf("PublishLocation: %v", err)
	}
	if got.Method != http.MethodPut {
		t.Errorf("method = %s, want PUT", got.Method)
	}
	if got.Path != "/location/profiles/me/geolocation" {
		t.Errorf("path = %q", got.Path)
	}
	// Omitted rather than sent as 0: accuracy is device-sourced and inventing
	// a value would misreport precision.
	if strings.Contains(got.Body, "accuracyMetres") {
		t.Errorf("accuracyMetres should be omitted when unset: %s", got.Body)
	}
}

func TestBulkMessagesDecodesDiscriminatedSender(t *testing.T) {
	var got capture
	c, _ := newTestClient(t, recording(&got, `{"data":[
	  {"id":"a","isOfficial":false,"senderUrl":"https://x/api/dvrt/dvrtsrs/1"},
	  {"id":"b","isOfficial":true,"senderUrl":"https://x/api/profile/officialProfiles/2"}],"totalRecords":2}`))

	msgs, err := c.BulkMessages(context.Background())
	if err != nil {
		t.Fatalf("BulkMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages = %d", len(msgs))
	}
	if msgs[0].IsOfficial || !msgs[1].IsOfficial {
		t.Errorf("isOfficial flags did not decode: %+v", msgs)
	}
	if got.Query["deviceTypeId"] == nil {
		t.Error("deviceTypeId not sent")
	}
}

// Reading your own lists bypasses the cache, because you may have just changed
// them; reading someone else's does not.
func TestSocialListNoCacheOnlyForSelf(t *testing.T) {
	var got capture
	c, _ := newTestClient(t, recording(&got, `{"data":{"mutual":[],"nonMutual":[]}}`))

	if _, err := c.Friends(context.Background(), ""); err != nil {
		t.Fatalf("Friends(self): %v", err)
	}
	if got.Query.Get("noCache") != "true" {
		t.Errorf("self read should send noCache=true, query=%v", got.Query)
	}
	if got.Path != "/profileSearch/profiles/me/friends" {
		t.Errorf("path = %q", got.Path)
	}

	if _, err := c.Followers(context.Background(), "them"); err != nil {
		t.Fatalf("Followers(other): %v", err)
	}
	if got.Query.Get("noCache") == "true" {
		t.Error("reading another profile should not force noCache")
	}
	if got.Query.Get("myProfileId") != "me" {
		t.Errorf("myProfileId = %q, want the caller's id", got.Query.Get("myProfileId"))
	}
}

func TestMembershipStatusIsPremium(t *testing.T) {
	var got capture
	c, _ := newTestClient(t, recording(&got, `{"membershipLevelId":2,"profileId":"me"}`))
	m, err := c.MembershipStatus(context.Background(), "")
	if err != nil {
		t.Fatalf("MembershipStatus: %v", err)
	}
	if !m.IsPremium() {
		t.Error("level 2 should be premium")
	}
	if got.Path != "/membership/profiles/me/membershipStatus" {
		t.Errorf("path = %q", got.Path)
	}
	// Level 0 is free and official profiles alike.
	if (MembershipStatus{MembershipLevelID: 0}).IsPremium() {
		t.Error("level 0 must not be premium")
	}
}

// Every unauthenticated helper must honour WithBaseURL. A helper that hardcodes
// the production host once caused a unit test to dial the real API.
func TestCheckProfileNameAnonymousHonoursBaseURL(t *testing.T) {
	var got capture
	srv := httptest.NewServer(recording(&got, `{"isValid":true}`))
	t.Cleanup(srv.Close)

	out, err := CheckProfileNameAnonymous(context.Background(), "someone",
		WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("CheckProfileNameAnonymous: %v", err)
	}
	if !out.IsValid {
		t.Error("expected valid")
	}
	if got.Path != "/antiAbuse/profileNameChecks" {
		t.Errorf("path = %q", got.Path)
	}
}

func TestFeedByNameEscapesSegment(t *testing.T) {
	var got capture
	c, _ := newTestClient(t, recording(&got, `{"data":[],"totalRecords":0}`))
	if _, err := c.Feed(context.Background(), "home"); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if got.Path != "/feed/profiles/me/feeds/home" {
		t.Errorf("path = %q", got.Path)
	}
}
