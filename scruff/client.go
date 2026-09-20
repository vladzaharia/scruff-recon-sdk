package scruff

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/vladzaharia/scruff-recon-sdk/core"
)

// Client is a SCRUFF API client. It is safe for concurrent use.
type Client struct {
	cfg   config
	store *core.Store[Session]
	tr    *core.Transport
}

// Compile-time conformance to the shared SDK contract.
var (
	_ core.Authenticator = (*authenticator)(nil)
	_ core.Downloader    = (*Client)(nil)
	_ core.MediaResolver = (*Client)(nil)
)

type config struct {
	http       core.Doer
	baseURL    string
	userAgent  string
	limiter    *core.Limiter
	location   LatLng
	locationFn func() LatLng
}

// loc resolves the coordinates to use for this request.
//
// A provider is consulted per call rather than per client, because callers
// whose position changes at runtime would otherwise have to rebuild the client
// — and the coordinates feed the request signature, so a stale value is worse
// than no value.
func (c config) loc() LatLng {
	if c.locationFn != nil {
		return c.locationFn()
	}
	return c.location
}

// Option configures a Client.
type Option func(*config)

// WithHTTPClient supplies the HTTP client to use.
func WithHTTPClient(hc core.Doer) Option { return func(c *config) { c.http = hc } }

// WithBaseURL overrides the API host. Intended for tests.
func WithBaseURL(u string) Option { return func(c *config) { c.baseURL = u } }

// WithUserAgent overrides the User-Agent. The default matches the app; changing
// it makes your traffic distinguishable.
func WithUserAgent(ua string) Option { return func(c *config) { c.userAgent = ua } }

// WithLocation supplies coordinates for the request signature and for
// location-aware endpoints. Without it, "0.0" is sent — which the app also does
// when it has no fix, but which makes several endpoints return 400.
func WithLocation(loc LatLng) Option { return func(c *config) { c.location = loc } }

// WithLocationProvider supplies coordinates dynamically, consulted on every
// request that needs them. Use this instead of WithLocation when the position
// can change while the client is alive.
func WithLocationProvider(fn func() LatLng) Option {
	return func(c *config) { c.locationFn = fn }
}

// WithRateLimit overrides the client-side limiter.
func WithRateLimit(l *core.Limiter) Option { return func(c *config) { c.limiter = l } }

// DefaultLimiter mirrors the per-path token buckets the app enforces on itself.
//
// The server was never observed rate-limiting — no 429, no RateLimit header, in
// any capture — so this is about behaving like the real client rather than
// obeying an advertised limit.
func DefaultLimiter() *core.Limiter {
	return &core.Limiter{
		Buckets: map[string]core.Bucket{
			"/app/profile":    {Capacity: 60, Refill: 1.0},
			"/app/location":   {Capacity: 20, Refill: 0.33},
			"/app/chat/media": {Capacity: 60, Refill: 1.0},
		},
		Default: core.Bucket{Capacity: 30, Refill: 2.0},
	}
}

// slowPaths get a longer timeout, matching the app. These grids are genuinely
// slow.
var slowPaths = map[string]time.Duration{
	"/app/location":             60 * time.Second,
	"/app/albums/received":      60 * time.Second,
	"/app/albums/permissions":   60 * time.Second,
	"/app/grid/matches_mutual":  60 * time.Second,
	"/app/favorite":             60 * time.Second,
	"/app/block":                60 * time.Second,
	"/app/events/rsvps":         60 * time.Second,
	"/app/explorer/herenow":     60 * time.Second,
	"/app/explorer/heresoon":    60 * time.Second,
	"/app/explorer/ambassadors": 60 * time.Second,
	"/app/viewers/incoming":     60 * time.Second,
	"/app/woofs/incoming":       60 * time.Second,
	"/app/inbox/recent":         60 * time.Second,
	"/app/inbox/unread":         60 * time.Second,
}

func newConfig(opts ...Option) config {
	c := config{
		baseURL:   BaseURL,
		userAgent: UserAgent,
		limiter:   DefaultLimiter(),
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
func New(s Session, opts ...Option) *Client { return newClientWith(s, newConfig(opts...)) }

func newClientWith(s Session, cfg config) *Client {
	c := &Client{cfg: cfg, store: core.NewStore(s, nil)}
	c.tr = &core.Transport{
		BaseURL:   cfg.baseURL,
		HTTP:      cfg.http,
		Auth:      &authenticator{c: c},
		Limiter:   cfg.limiter,
		UserAgent: cfg.userAgent,
		Accept:    "application/json",
		Timeouts:  slowPaths,
	}
	return c
}

// Session returns the current session.
func (c *Client) Session() Session { return c.store.Get() }

// OnSessionUpdate registers a callback invoked when the session changes, which
// for SCRUFF means when Register rotates the socket credentials. Nothing here
// expires, so this fires far less often than its Recon counterpart.
func (c *Client) OnSessionUpdate(fn core.SessionSaver[Session]) { c.store.OnUpdate(fn) }

// ProfileID returns your own profile id.
func (c *Client) ProfileID() string { return c.store.Get().ProfileID }

// Register loads or refreshes the session.
//
// Call it after Connect, and again whenever you need fresh socket credentials —
// the app does this on every foreground. It rotates socket.pwd, so the session
// is saved through OnSessionUpdate.
func (c *Client) Register(ctx context.Context) (*RegisterResponse, error) {
	var out RegisterResponse
	if err := c.tr.JSON(ctx, registerRequest(c.store.Get(), c.cfg.loc()), &out); err != nil {
		return nil, err
	}

	s := c.store.Get()
	if id := out.Profile.ID.String(); id != "" && id != "0" {
		s.ProfileID = id
	}
	if out.Profile.Name != "" {
		s.ProfileName = out.Profile.Name
	}
	s.SocketHost, s.SocketPort, s.SocketPwd = out.Socket.Host, out.Socket.Port, out.Socket.Pwd
	// Prefer the server's CDN hosts over the constants.
	if out.ProfileCDN != "" {
		s.ProfileCDN = out.ProfileCDN
	}
	if out.AlbumImageCDN != "" {
		s.AlbumCDN = out.AlbumImageCDN
	}
	if out.AppCDN != "" {
		s.AppCDN = out.AppCDN
	}
	if out.CDN != "" {
		s.ProfilesCDN = out.CDN
	}
	if out.AccountTier.Tier != "" {
		s.AccountTier = out.AccountTier.Tier
	}
	if err := c.store.Set(ctx, s); err != nil {
		return &out, err
	}
	return &out, nil
}

// Logout ends the server-side session.
//
// Clearing local state without calling this leaves the device session alive.
func (c *Client) Logout(ctx context.Context) error {
	return c.tr.JSON(ctx, &core.Request{Method: http.MethodPost, Path: "/app/logout"}, nil)
}

// Download fetches media.
//
// No authentication is applied: profile media is public and album and chat
// media are CloudFront-signed, so an Authorization header would be meaningless.
func (c *Client) Download(ctx context.Context, ref core.MediaRef) (core.Blob, error) {
	return core.DownloadMedia(ctx, c.cfg.http, ref, func(r *http.Request) {
		r.Header.Set("User-Agent", c.cfg.userAgent)
	})
}

// profileCDN returns the profile media host, preferring the server's value.
func (c *Client) profileCDN() string {
	if v := c.store.Get().ProfileCDN; v != "" {
		return strings.TrimSuffix(v, "/")
	}
	return DefaultProfileCDN
}

// AvatarURL builds the avatar URL for a profile, or "" when it has no photo.
//
// Profile media is served unsigned from the profile CDN, so unlike chat and
// album media this URL is stable and safe to cache.
func (c *Client) AvatarURL(p Profile) string {
	for _, ph := range p.ProfilePhotos {
		key := ph.FullsizeKey
		if key == "" {
			key = ph.ThumbnailKey
		}
		if key != "" {
			return c.profileCDN() + "/" + key
		}
	}
	return ""
}

// parseTime parses SCRUFF's HTTP-date timestamps.
func parseTime(s string) (time.Time, error) { return core.ParseTime(s) }

// ParseTimestamp parses a Scruff timestamp. Exported because callers routinely
// need to turn a Message.CreatedAt or Conversation.ActionAt into a time.Time,
// and these are RFC 1123 HTTP dates rather than ISO 8601.
func ParseTimestamp(s string) (time.Time, error) { return core.ParseTime(s) }
