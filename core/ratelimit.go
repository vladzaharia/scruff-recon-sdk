package core

import (
	"context"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Bucket is a token-bucket rate limit: Capacity tokens, refilled at Refill
// tokens per second.
type Bucket struct {
	Capacity float64
	Refill   float64 // tokens per second
}

// Limiter applies per-path token buckets, falling back to Default.
//
// The shape mirrors what SCRUFF's own client does — it self-throttles per
// endpoint before a request is ever dispatched, rather than reacting to server
// pushback. Neither network was observed returning 429 or any RateLimit header,
// so this is about behaving like a well-mannered client rather than obeying an
// advertised limit.
//
// A zero Limiter allows everything.
type Limiter struct {
	// Buckets is keyed by path prefix. The longest matching prefix wins.
	Buckets map[string]Bucket
	// Default applies when no prefix matches. A zero Default is unlimited.
	Default Bucket

	// now is swappable for tests.
	now func() time.Time

	mu    sync.Mutex
	state map[string]*bucketState
}

type bucketState struct {
	tokens float64
	last   time.Time
}

// Wait blocks until the bucket covering path has a token, or ctx is done.
func (l *Limiter) Wait(ctx context.Context, path string) error {
	if l == nil {
		return nil
	}
	for {
		delay, ok := l.reserve(path)
		if ok {
			return nil
		}
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// reserve takes a token if one is available, otherwise reports how long to wait
// before trying again.
func (l *Limiter) reserve(path string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	key, b := l.bucketFor(path)
	if b.Capacity <= 0 || b.Refill <= 0 {
		return 0, true // unlimited
	}

	nowFn := l.now
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn()

	if l.state == nil {
		l.state = map[string]*bucketState{}
	}
	st, ok := l.state[key]
	if !ok {
		st = &bucketState{tokens: b.Capacity, last: now}
		l.state[key] = st
	}

	if elapsed := now.Sub(st.last).Seconds(); elapsed > 0 {
		st.tokens = math.Min(b.Capacity, st.tokens+elapsed*b.Refill)
		st.last = now
	}

	if st.tokens >= 1 {
		st.tokens--
		return 0, true
	}
	need := (1 - st.tokens) / b.Refill
	return time.Duration(need * float64(time.Second)), false
}

// bucketFor picks the longest matching path prefix.
func (l *Limiter) bucketFor(path string) (string, Bucket) {
	best, bestLen := "", -1
	for prefix := range l.Buckets {
		if strings.HasPrefix(path, prefix) && len(prefix) > bestLen {
			best, bestLen = prefix, len(prefix)
		}
	}
	if bestLen >= 0 {
		return best, l.Buckets[best]
	}
	return "", l.Default
}

// RetryAfter reads a Retry-After header, in seconds or as an HTTP date.
// It returns false when the header is absent or unparseable.
func RetryAfter(h http.Header) (time.Duration, bool) {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0, false
	}
	if secs, err := time.ParseDuration(v + "s"); err == nil && secs >= 0 {
		return secs, true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d, true
		}
		return 0, true
	}
	return 0, false
}
