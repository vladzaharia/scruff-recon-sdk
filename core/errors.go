package core

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// APIError is returned for every non-2xx response from either network.
//
// Body is truncated — see MaxErrorBodyBytes — because neither API returns
// anything useful in a large error body, and unbounded capture of a response
// that may contain personal data is not worth it.
type APIError struct {
	Method     string
	URL        string
	StatusCode int
	Body       string
	// Header retains a few response headers useful for diagnosis, notably
	// Retry-After.
	Header http.Header
}

// MaxErrorBodyBytes bounds how much of an error body is retained.
const MaxErrorBodyBytes = 512

func (e *APIError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("%s %s: http %d", e.Method, e.URL, e.StatusCode)
	}
	return fmt.Sprintf("%s %s: http %d: %s", e.Method, e.URL, e.StatusCode, e.Body)
}

// IsUnauthorized reports whether the call failed authentication.
//
// Note for Recon: a malformed Authorization header (the "Bearer Bearer" case)
// produces a 401 with an empty body from the messaging and SignalR services,
// but is tolerated by the profile service. A 401 here is therefore as likely to
// mean "your header is wrong" as "your token expired".
func (e *APIError) IsUnauthorized() bool { return e.StatusCode == http.StatusUnauthorized }

// IsForbidden reports a 403. On SCRUFF this is heavily overloaded and its
// meaning is endpoint-specific — blocked by the peer, a Pro cap, an invalid
// password — so callers should interpret it per call site.
func (e *APIError) IsForbidden() bool { return e.StatusCode == http.StatusForbidden }

// IsNotFound reports a 404. On SCRUFF's GET /app/chat this is the normal
// end-of-history signal rather than an error.
func (e *APIError) IsNotFound() bool { return e.StatusCode == http.StatusNotFound }

// IsRateLimited reports a 429. Neither network produced one during capture, so
// this is defensive.
func (e *APIError) IsRateLimited() bool { return e.StatusCode == http.StatusTooManyRequests }

// IsServerError reports a 5xx.
func (e *APIError) IsServerError() bool { return e.StatusCode >= 500 }

// IsRetryable reports whether retrying the same request could plausibly
// succeed: rate limits and server errors, but not client errors.
func (e *APIError) IsRetryable() bool { return e.IsRateLimited() || e.IsServerError() }

// AsAPIError extracts an *APIError from an error chain.
func AsAPIError(err error) (*APIError, bool) {
	var e *APIError
	ok := errors.As(err, &e)
	return e, ok
}

// StatusCode returns the HTTP status from an error chain, or 0.
func StatusCode(err error) int {
	if e, ok := AsAPIError(err); ok {
		return e.StatusCode
	}
	return 0
}

// TruncateBody shortens an error body for inclusion in an APIError and collapses
// whitespace so a stray HTML error page stays readable on one line.
func TruncateBody(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if len(s) <= MaxErrorBodyBytes {
		return s
	}
	return s[:MaxErrorBodyBytes] + "…"
}

// Sentinel errors for conditions that are not HTTP failures.
var (
	// ErrNotFound reports that a lookup returned an empty result where one
	// item was expected.
	ErrNotFound = errors.New("core: not found")
	// ErrNoMedia reports that a media reference could not be resolved to a
	// fetchable URL.
	ErrNoMedia = errors.New("core: no media URL")
)
