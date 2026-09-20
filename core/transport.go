package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Doer is the subset of *http.Client the transport needs.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Authenticator decorates a Request with whatever credentials the network
// expects.
//
// It takes a *Request rather than an *http.Request so that implementations can
// write into the query string, the form body, or a multipart field as
// appropriate — see the Request docs for why that matters.
type Authenticator interface {
	Authorize(ctx context.Context, r *Request) error
}

// AuthenticatorFunc adapts a function to Authenticator.
type AuthenticatorFunc func(ctx context.Context, r *Request) error

// Authorize implements Authenticator.
func (f AuthenticatorFunc) Authorize(ctx context.Context, r *Request) error { return f(ctx, r) }

// Transport turns Requests into responses: applies rate limiting, lets an
// Authenticator decorate the request, sends it, and maps non-2xx to *APIError.
//
// It does not log, retry, or refresh. Token refresh belongs to the
// Authenticator, which is the only component that knows what a token is.
type Transport struct {
	// BaseURL is prepended to relative request paths.
	BaseURL string
	// HTTP defaults to a client from NewHTTPClient.
	HTTP Doer
	// Auth is applied to every request unless the Request opts out.
	Auth Authenticator
	// Limiter throttles by request path. May be nil.
	Limiter *Limiter
	// UserAgent is set on every request when non-empty.
	UserAgent string
	// Accept is set on every request when non-empty.
	Accept string
	// Timeouts overrides the client timeout for matching path prefixes.
	// SCRUFF's own client raises several grid endpoints to 60s.
	Timeouts map[string]time.Duration

	// Decorate runs on every Request before Auth, for network-wide parameters
	// such as Recon's mandatory culture=en.
	Decorate func(r *Request)
}

// Do sends a request and returns the response body. The response is always
// closed. A non-2xx status yields an *APIError.
func (t *Transport) Do(ctx context.Context, r *Request) ([]byte, *http.Response, error) {
	if t.Decorate != nil {
		t.Decorate(r)
	}
	if t.Auth != nil {
		if err := t.Auth.Authorize(ctx, r); err != nil {
			return nil, nil, err
		}
	}
	if err := t.Limiter.Wait(ctx, r.Path); err != nil {
		return nil, nil, err
	}

	if r.Header == nil {
		r.Header = http.Header{}
	}
	if t.UserAgent != "" && r.Header.Get("User-Agent") == "" {
		r.Header.Set("User-Agent", t.UserAgent)
	}
	if t.Accept != "" && r.Header.Get("Accept") == "" {
		r.Header.Set("Accept", t.Accept)
	}

	if d, ok := t.timeoutFor(r.Path); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}

	req, err := r.Build(ctx, t.BaseURL)
	if err != nil {
		return nil, nil, err
	}

	hc := t.HTTP
	if hc == nil {
		hc = NewHTTPClient()
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("%s %s: %w", r.Method, req.URL.Redacted(), err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp, fmt.Errorf("%s %s: read body: %w", r.Method, req.URL.Redacted(), err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return body, resp, &APIError{
			Method:     r.Method,
			URL:        req.URL.Redacted(),
			StatusCode: resp.StatusCode,
			Body:       TruncateBody(body),
			Header:     resp.Header,
		}
	}
	return body, resp, nil
}

// JSON sends a request and unmarshals a 2xx body into out. A nil out discards
// the body, which suits the endpoints that answer 204 or 200-with-no-body.
func (t *Transport) JSON(ctx context.Context, r *Request, out any) error {
	body, _, err := t.Do(ctx, r)
	if err != nil {
		return err
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s %s: decode response: %w", r.Method, r.Path, err)
	}
	return nil
}

func (t *Transport) timeoutFor(path string) (time.Duration, bool) {
	best, bestLen := time.Duration(0), -1
	for prefix, d := range t.Timeouts {
		if len(prefix) > bestLen && hasPrefix(path, prefix) {
			best, bestLen = d, len(prefix)
		}
	}
	return best, bestLen >= 0
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
