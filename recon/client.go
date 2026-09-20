package recon

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/vladzaharia/scruff-recon-sdk/core"
)

// Client is a Recon API client. It is safe for concurrent use.
type Client struct {
	cfg   config
	store *core.Store[Session]
	auth  *authenticator
	tr    *core.Transport
}

// Compile-time conformance to the shared SDK contract.
var (
	_ core.Authenticator = (*authenticator)(nil)
	_ core.Downloader    = (*Client)(nil)
)

type config struct {
	http      core.Doer
	userAgent string
	baseURL   string
	limiter   *core.Limiter
}

// Option configures a Client.
type Option func(*config)

// WithHTTPClient supplies the HTTP client to use.
func WithHTTPClient(hc core.Doer) Option { return func(c *config) { c.http = hc } }

// WithUserAgent overrides the User-Agent. Recon records this as the device
// description and shows it in the account's session list, so prefer something
// that identifies your client honestly.
func WithUserAgent(ua string) Option { return func(c *config) { c.userAgent = ua } }

// WithBaseURL overrides the API base URL. Intended for tests.
func WithBaseURL(u string) Option { return func(c *config) { c.baseURL = u } }

// WithRateLimit overrides the client-side limiter. Recon was never observed
// rate-limiting, so the default is deliberately gentle rather than derived.
func WithRateLimit(l *core.Limiter) Option { return func(c *config) { c.limiter = l } }

func newConfig(opts ...Option) config {
	c := config{
		userAgent: DefaultUserAgent,
		baseURL:   BaseURL,
		limiter:   &core.Limiter{Default: core.Bucket{Capacity: 10, Refill: 5}},
	}
	for _, o := range opts {
		o(&c)
	}
	if c.http == nil {
		c.http = core.NewHTTPClient()
	}
	return c
}

// New returns a client for an existing session.
func New(s Session, opts ...Option) *Client {
	cfg := newConfig(opts...)
	// Normalise on the way in, so a session persisted by an older client that
	// stored the "Bearer " prefix still works.
	s.AccessToken = stripBearer(s.AccessToken)

	c := &Client{cfg: cfg, store: core.NewStore(s, nil)}
	c.auth = &authenticator{c: c}
	c.tr = &core.Transport{
		BaseURL:   cfg.baseURL,
		HTTP:      cfg.http,
		Auth:      c.auth,
		Limiter:   cfg.limiter,
		UserAgent: cfg.userAgent,
		Accept:    "application/json",
		Decorate:  addCulture,
	}
	return c
}

// Session returns the current session, including any refreshed tokens.
func (c *Client) Session() Session { return c.store.Get() }

// OnSessionUpdate registers a callback invoked whenever the session changes,
// which in practice means whenever the 15-minute access token is refreshed.
// Persist the session from here or you will have to log in again next start.
func (c *Client) OnSessionUpdate(fn core.SessionSaver[Session]) { c.store.OnUpdate(fn) }

// ProfileID returns the active profile id, which keys every messaging call.
func (c *Client) ProfileID() string { return c.store.Get().ProfileID }

// optionsForRefresh reconstructs this client's options so the unauthenticated
// refresh call reaches the same host the client was configured with.
func (c *Client) optionsForRefresh() []Option {
	return []Option{
		WithBaseURL(c.cfg.baseURL),
		WithHTTPClient(c.cfg.http),
		WithUserAgent(c.cfg.userAgent),
	}
}

// Ping verifies the session by forcing a token check and a cheap authenticated
// call. Useful as a smoke test.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.AccountAppSettings(ctx)
	return err
}

// Logout ends the server-side session. Discarding tokens locally leaves it
// alive, so call this when a user deliberately signs out.
func (c *Client) Logout(ctx context.Context) error {
	s := c.store.Get()
	if s.AccountID == "" || s.SessionID == "" {
		return nil
	}
	return c.tr.JSON(ctx, &core.Request{
		Method: http.MethodDelete,
		Path:   "/account/accounts/" + s.AccountID + "/sessions/" + s.SessionID,
	}, nil)
}

// Download fetches media. Recon serves avatars from an unauthenticated CDN but
// gates chat attachments behind the gateway, which needs the bearer token, so
// the token is attached either way.
func (c *Client) Download(ctx context.Context, ref core.MediaRef) (core.Blob, error) {
	tok, err := c.auth.token(ctx)
	if err != nil {
		return core.Blob{}, err
	}
	return core.DownloadMedia(ctx, c.cfg.http, ref, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+tok)
		r.Header.Set("User-Agent", c.cfg.userAgent)
	})
}

// parseTime parses Recon's ISO 8601 timestamps.
func parseTime(s string) (time.Time, error) { return core.ParseTime(s) }

// asError is errors.As with a friendlier name for the IsX helpers.
func asError[T error](err error, target *T) bool { return errors.As(err, target) }

// ParseTimestamp parses a Recon timestamp. Exported because callers routinely
// need to turn a Message.CreatedDate or Conversation.LastActivityDate into a
// time.Time, and the API's layouts are not all RFC 3339.
func ParseTimestamp(s string) (time.Time, error) { return core.ParseTime(s) }
