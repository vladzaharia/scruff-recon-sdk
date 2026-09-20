package core

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestLimiterAllowsBurstThenThrottles(t *testing.T) {
	now := time.Unix(0, 0)
	l := &Limiter{
		Default: Bucket{Capacity: 3, Refill: 1},
		now:     func() time.Time { return now },
	}

	for i := range 3 {
		if d, ok := l.reserve("/x"); !ok {
			t.Fatalf("burst token %d denied, wait %v", i, d)
		}
	}
	d, ok := l.reserve("/x")
	if ok {
		t.Fatal("4th request should be throttled with a capacity of 3")
	}
	if d <= 0 || d > time.Second+time.Millisecond {
		t.Errorf("wait = %v, want ~1s at 1 token/s", d)
	}

	now = now.Add(2 * time.Second)
	if _, ok := l.reserve("/x"); !ok {
		t.Error("should have refilled after 2s")
	}
}

// SCRUFF limits per path, with distinct buckets for /app/profile, /app/location
// and /app/chat/media. Longest prefix must win.
func TestLimiterLongestPrefixWins(t *testing.T) {
	l := &Limiter{
		Buckets: map[string]Bucket{
			"/app/chat":       {Capacity: 5, Refill: 1},
			"/app/chat/media": {Capacity: 60, Refill: 1},
		},
		Default: Bucket{Capacity: 30, Refill: 2},
	}
	for _, tc := range []struct{ path, want string }{
		{"/app/chat/media?guid=x", "/app/chat/media"},
		{"/app/chat", "/app/chat"},
		{"/app/inbox/stream", ""},
	} {
		if got, _ := l.bucketFor(tc.path); got != tc.want {
			t.Errorf("bucketFor(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestNilLimiterIsUnlimited(t *testing.T) {
	var l *Limiter
	if err := l.Wait(context.Background(), "/x"); err != nil {
		t.Errorf("nil limiter should allow: %v", err)
	}
}

func TestLimiterWaitRespectsContext(t *testing.T) {
	l := &Limiter{Default: Bucket{Capacity: 1, Refill: 0.001}}
	ctx := context.Background()
	if err := l.Wait(ctx, "/x"); err != nil {
		t.Fatalf("first call should pass: %v", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := l.Wait(ctx, "/x"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
}

func TestRetryAfter(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
		ok    bool
	}{
		{"seconds", "90", 90 * time.Second, true},
		{"zero", "0", 0, true},
		{"absent", "", 0, false},
		{"garbage", "soon", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			if tc.value != "" {
				h.Set("Retry-After", tc.value)
			}
			got, ok := RetryAfter(h)
			if ok != tc.ok || (tc.ok && got != tc.want) {
				t.Errorf("= (%v, %v), want (%v, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestAPIErrorPredicates(t *testing.T) {
	tests := []struct {
		status                                  int
		unauth, forbidden, notFound, rl, server bool
	}{
		{401, true, false, false, false, false},
		{403, false, true, false, false, false},
		{404, false, false, true, false, false},
		{429, false, false, false, true, false},
		{503, false, false, false, false, true},
	}
	for _, tc := range tests {
		e := &APIError{StatusCode: tc.status}
		if e.IsUnauthorized() != tc.unauth || e.IsForbidden() != tc.forbidden ||
			e.IsNotFound() != tc.notFound || e.IsRateLimited() != tc.rl ||
			e.IsServerError() != tc.server {
			t.Errorf("status %d predicates mismatched", tc.status)
		}
		if want := tc.rl || tc.server; e.IsRetryable() != want {
			t.Errorf("status %d IsRetryable = %v, want %v", tc.status, e.IsRetryable(), want)
		}
	}
}

func TestAsAPIErrorUnwrapsChain(t *testing.T) {
	base := &APIError{StatusCode: 404, Method: "GET", URL: "https://x.test/y"}
	wrapped := errors.Join(errors.New("context"), base)
	got, ok := AsAPIError(wrapped)
	if !ok || got.StatusCode != 404 {
		t.Fatalf("AsAPIError = (%v, %v), want the wrapped 404", got, ok)
	}
	if StatusCode(wrapped) != 404 {
		t.Errorf("StatusCode = %d, want 404", StatusCode(wrapped))
	}
	if StatusCode(errors.New("plain")) != 0 {
		t.Error("StatusCode of a non-API error should be 0")
	}
}

// SCRUFF returns HTML error pages on some failures; an error string should stay
// readable on one line and bounded.
func TestTruncateBodyCollapsesAndBounds(t *testing.T) {
	if got := TruncateBody([]byte("a\n\n  b\tc  ")); got != "a b c" {
		t.Errorf("= %q, want %q", got, "a b c")
	}
	long := make([]byte, MaxErrorBodyBytes*2)
	for i := range long {
		long[i] = 'x'
	}
	got := TruncateBody(long)
	if len([]rune(got)) != MaxErrorBodyBytes+1 {
		t.Errorf("len = %d runes, want %d plus ellipsis", len([]rune(got)), MaxErrorBodyBytes)
	}
}

func TestParseTimeAcceptsBothAPIFormats(t *testing.T) {
	tests := []struct{ name, in string }{
		{"recon RFC3339", "2026-09-13T18:06:37.76Z"},
		{"recon RFC3339 offset", "2026-09-13T18:06:37+01:00"},
		{"scruff HTTP date", "Sun, 20 Sep 2026 01:18:45 GMT"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseTime(tc.in)
			if err != nil {
				t.Fatalf("ParseTime(%q): %v", tc.in, err)
			}
			if got.IsZero() {
				t.Error("got zero time")
			}
		})
	}
	if got, err := ParseTime(""); err != nil || !got.IsZero() {
		t.Errorf("empty = (%v, %v), want (zero, nil)", got, err)
	}
	if _, err := ParseTime("not a time"); err == nil {
		t.Error("want an error for unparseable input")
	}
}

func TestNewUUIDShape(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		u := NewUUID()
		if len(u) != 36 || u[14] != '4' {
			t.Fatalf("malformed v4 uuid: %q", u)
		}
		if v := u[19]; v != '8' && v != '9' && v != 'a' && v != 'b' {
			t.Fatalf("bad variant nibble %q in %q", v, u)
		}
		if seen[u] {
			t.Fatalf("duplicate uuid %q", u)
		}
		seen[u] = true
	}
}

func TestRandHexLength(t *testing.T) {
	for _, n := range []int{8, 16, 20} {
		if got := len(RandHex(n)); got != n*2 {
			t.Errorf("RandHex(%d) length = %d, want %d", n, got, n*2)
		}
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	b := Backoff{Min: time.Second, Max: 30 * time.Second, Factor: 2}
	// Full jitter means each delay is a draw from (0, ceiling]; assert the
	// ceiling rather than the value.
	for _, tc := range []struct {
		attempt int
		ceiling time.Duration
	}{{0, time.Second}, {1, 2 * time.Second}, {3, 8 * time.Second}, {10, 30 * time.Second}} {
		for range 50 {
			d := b.Delay(tc.attempt)
			if d <= 0 || d > tc.ceiling {
				t.Fatalf("attempt %d: delay %v outside (0, %v]", tc.attempt, d, tc.ceiling)
			}
		}
	}
}

func TestBackoffZeroValueHasDefaults(t *testing.T) {
	var b Backoff
	if d := b.Delay(0); d <= 0 || d > time.Second {
		t.Errorf("zero-value delay = %v, want within (0, 1s]", d)
	}
	if d := b.Delay(50); d > 2*time.Minute {
		t.Errorf("delay = %v, want capped at the 2m default", d)
	}
}

func TestSuperviseStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	err := Supervise(ctx, Backoff{Min: time.Millisecond, Max: 2 * time.Millisecond}, time.Hour,
		func(context.Context) error {
			calls++
			return errors.New("connection dropped")
		})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if calls == 0 {
		t.Error("connect was never attempted")
	}
}

func TestPaginateWalksCursors(t *testing.T) {
	pages := [][]int{{1, 2}, {3, 4}, {5}}
	page := func(_ context.Context, c int) ([]int, int, bool, error) {
		if c >= len(pages) {
			return nil, c, true, nil
		}
		return pages[c], c + 1, c == len(pages)-1, nil
	}
	got, err := Collect(context.Background(), 0, 0, page)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got) != 5 || got[0] != 1 || got[4] != 5 {
		t.Errorf("= %v, want 1..5", got)
	}
}

func TestPaginateRespectsMaxItems(t *testing.T) {
	page := func(_ context.Context, c int) ([]int, int, bool, error) {
		return []int{c, c + 1}, c + 2, false, nil // infinite
	}
	got, err := Collect(context.Background(), 0, 5, page)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got) != 5 {
		t.Errorf("len = %d, want 5", len(got))
	}
}

func TestPaginateStopsOnEmptyPage(t *testing.T) {
	calls := 0
	page := func(_ context.Context, c int) ([]int, int, bool, error) {
		calls++
		if calls == 1 {
			return []int{1}, c + 1, false, nil
		}
		return nil, c, false, nil // empty page, not flagged done
	}
	got, err := Collect(context.Background(), 0, 0, page)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got) != 1 || calls != 2 {
		t.Errorf("items=%v calls=%d, want 1 item and 2 calls", got, calls)
	}
}

func TestPaginateErrStopIsNotAnError(t *testing.T) {
	page := func(_ context.Context, c int) ([]int, int, bool, error) {
		return []int{1, 2, 3}, c + 1, false, nil
	}
	n := 0
	err := Paginate(context.Background(), 0, 0, page, func(int) error {
		n++
		if n == 2 {
			return ErrStopPagination
		}
		return nil
	})
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if n != 2 {
		t.Errorf("yielded %d, want 2", n)
	}
}
