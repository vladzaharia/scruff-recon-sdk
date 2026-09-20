package recon

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/vladzaharia/scruff-recon-sdk/core"
)

// Protocol constants. These identify the client to Recon.
const (
	// BaseURL is the API gateway.
	BaseURL = "https://www.recon.com/api"

	// AppVersion is the web client version this package mirrors.
	AppVersion = "1.7.31"

	// DeviceTypeWeb is the deviceTypeId for a web client.
	DeviceTypeWeb = 1

	// ApplicationIDWeb is the applicationId enum for the Recon web client.
	//
	// This MUST be the integer 3. Sending a UUID — an easy assumption, since
	// most ids in this API are UUIDs — makes the server reject the entire
	// request model with "apiModel required".
	ApplicationIDWeb = 3

	// Culture is appended to every request; the API expects it universally.
	Culture = "en"

	// refreshGrace is how long before expiry a token is proactively refreshed.
	// The web client uses a 3-minute grace window.
	refreshGrace = 3 * time.Minute
)

// DefaultUserAgent identifies this client honestly. Recon records it as the
// device description, so it is visible in the account's session list.
var DefaultUserAgent = "recon-sdk-go/0.1 (+https://github.com/vladzaharia/scruff-recon-sdk)"

// stripBearer removes a redundant "Bearer " prefix.
//
// This exists because of the single worst trap in the Recon API: authenticate
// and refreshTokens return accessToken ALREADY prefixed with "Bearer ". Callers
// naturally add their own prefix, producing "Bearer Bearer eyJ…". The profile
// service tolerates that, so a first test passes — but the messaging and
// SignalR services reject it with an empty-body 401.
//
// Applied both when storing a token and when reading one back, so a session
// restored from older storage is also normalised.
func stripBearer(s string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "Bearer "))
}

// CreateAppInstallation registers this client install. Unauthenticated.
//
// The returned id is required by Authenticate.
func CreateAppInstallation(ctx context.Context, opts ...Option) (*AppInstallation, error) {
	cfg := newConfig(opts...)
	t := unauthTransport(cfg)
	var out AppInstallation
	err := t.JSON(ctx, &core.Request{
		Method: http.MethodPost,
		Path:   "/account/appInstallations",
		JSON: map[string]any{
			"applicationId":  ApplicationIDWeb,
			"deviceTypeId":   DeviceTypeWeb,
			"appVersion":     AppVersion,
			"userAgent":      cfg.userAgent,
			"description":    "scruff-recon-sdk",
			"isInstalledPwa": false,
		},
	}, &out)
	if err != nil {
		return nil, err
	}
	if out.ID == "" {
		return nil, fmt.Errorf("recon: appInstallations returned no id")
	}
	return &out, nil
}

// Authenticate exchanges credentials for tokens. Unauthenticated.
//
// Most callers want Login, which also creates the app installation.
func Authenticate(ctx context.Context, creds Credentials, appInstallationID string, opts ...Option) (*TokenSet, error) {
	cfg := newConfig(opts...)
	t := unauthTransport(cfg)
	var out TokenSet
	err := t.JSON(ctx, &core.Request{
		Method: http.MethodPost,
		Path:   "/account/accounts/authenticate",
		JSON: map[string]any{
			"emailAddress":      creds.Email,
			"password":          creds.Password,
			"deviceTypeId":      DeviceTypeWeb,
			"appVersion":        AppVersion,
			"userAgent":         cfg.userAgent,
			"appInstallationId": appInstallationID,
		},
	}, &out)
	if err != nil {
		return nil, err
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("recon: authenticate returned no access token")
	}
	return &out, nil
}

// RefreshTokens exchanges a refresh token for a new token set.
//
// This call is sent WITHOUT an Authorization header — the web client's auth
// interceptor explicitly skips any URL containing /refreshTokens.
//
// Derived from the web client rather than from observed traffic: the captured
// session re-authenticated instead of refreshing, so this path has never been
// seen on the wire. If it 404s, fall back to a full Login.
func RefreshTokens(ctx context.Context, accountID, sessionID, refreshToken string, opts ...Option) (*TokenSet, error) {
	cfg := newConfig(opts...)
	t := unauthTransport(cfg)
	var out TokenSet
	err := t.JSON(ctx, &core.Request{
		Method: http.MethodPost,
		Path:   fmt.Sprintf("/account/accounts/%s/sessions/%s/refreshTokens", accountID, sessionID),
		JSON:   map[string]any{"refreshToken": refreshToken},
	}, &out)
	if err != nil {
		return nil, err
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("recon: refreshTokens returned no access token")
	}
	return &out, nil
}

// Login performs the full two-step sign-in: register an app installation, then
// authenticate.
func Login(ctx context.Context, creds Credentials, opts ...Option) (Session, error) {
	install, err := CreateAppInstallation(ctx, opts...)
	if err != nil {
		return Session{}, err
	}
	tok, err := Authenticate(ctx, creds, install.ID, opts...)
	if err != nil {
		return Session{}, err
	}
	if len(tok.ProfileIDs) == 0 {
		return Session{}, fmt.Errorf("recon: account has no messaging profile")
	}

	s := Session{Email: creds.Email, AppInstallationID: install.ID}
	applyTokens(&s, tok)
	return s, nil
}

// applyTokens folds a TokenSet into a Session, normalising the token prefix and
// parsing expiries.
func applyTokens(s *Session, tok *TokenSet) {
	s.AccessToken = stripBearer(tok.AccessToken)
	if tok.RefreshToken != "" {
		s.RefreshToken = tok.RefreshToken
	}
	if t, err := parseTime(tok.AccessTokenExpiry); err == nil && !t.IsZero() {
		s.AccessTokenExpiry = t
	}
	if t, err := parseTime(tok.RefreshTokenExpiry); err == nil && !t.IsZero() {
		s.RefreshTokenExpiry = t
	}
	if tok.AccountID != "" {
		s.AccountID = tok.AccountID
	}
	if tok.SessionID != "" {
		s.SessionID = tok.SessionID
	}
	if len(tok.ProfileIDs) > 0 && s.ProfileID == "" {
		// One account may own several profiles; exactly one is active. The web
		// client takes the first and offers no picker.
		s.ProfileID = tok.ProfileIDs[0]
	}
	if tok.MembershipLevelID != 0 {
		s.MembershipLevelID = tok.MembershipLevelID
	}
}

// authenticator attaches the bearer token, refreshing it when it is close to
// expiry.
type authenticator struct {
	c  *Client
	mu sync.Mutex
}

// Authorize implements core.Authenticator.
func (a *authenticator) Authorize(ctx context.Context, r *core.Request) error {
	if r.Header != nil && r.Header.Get("X-Recon-No-Auth") != "" {
		r.Header.Del("X-Recon-No-Auth")
		return nil
	}
	tok, err := a.token(ctx)
	if err != nil {
		return err
	}
	if r.Header == nil {
		r.Header = http.Header{}
	}
	// Exactly one "Bearer ". See stripBearer.
	r.Header.Set("Authorization", "Bearer "+tok)
	return nil
}

// token returns a valid access token, refreshing first if necessary.
func (a *authenticator) token(ctx context.Context) (string, error) {
	a.mu.Lock()
	s := a.c.store.Get()
	if s.AccessToken != "" && time.Now().Before(s.AccessTokenExpiry.Add(-refreshGrace)) {
		a.mu.Unlock()
		return stripBearer(s.AccessToken), nil
	}
	a.mu.Unlock()

	return a.refresh(ctx)
}

// refresh obtains a new token set. The network round-trip happens outside the
// lock so concurrent callers are not serialised behind it; the double check
// after re-acquiring means only one refresh's result is kept.
func (a *authenticator) refresh(ctx context.Context) (string, error) {
	a.mu.Lock()
	s := a.c.store.Get()
	if s.AccessToken != "" && time.Now().Before(s.AccessTokenExpiry.Add(-refreshGrace)) {
		a.mu.Unlock()
		return stripBearer(s.AccessToken), nil
	}
	if s.RefreshToken == "" {
		a.mu.Unlock()
		return "", &AuthExpiredError{Reason: "no refresh token stored"}
	}
	if !s.RefreshTokenExpiry.IsZero() && time.Now().After(s.RefreshTokenExpiry) {
		a.mu.Unlock()
		return "", &AuthExpiredError{Reason: "refresh token expired"}
	}
	accountID, sessionID, refreshTok := s.AccountID, s.SessionID, s.RefreshToken
	a.mu.Unlock()

	tok, err := RefreshTokens(ctx, accountID, sessionID, refreshTok, a.c.optionsForRefresh()...)
	if err != nil {
		if e, ok := core.AsAPIError(err); ok && (e.IsUnauthorized() || e.IsNotFound()) {
			return "", &AuthExpiredError{Reason: "refresh rejected", Err: err}
		}
		return "", err
	}

	a.mu.Lock()
	cur := a.c.store.Get()
	applyTokens(&cur, tok)
	saveErr := a.c.store.Set(ctx, cur)
	out := stripBearer(cur.AccessToken)
	a.mu.Unlock()

	if saveErr != nil {
		return out, fmt.Errorf("recon: refreshed token could not be persisted: %w", saveErr)
	}
	return out, nil
}

// AuthExpiredError reports that the session can no longer be refreshed and the
// user must log in again.
//
// Recon does not let us re-authenticate silently: the password is deliberately
// not persisted, so there is nothing to retry with.
type AuthExpiredError struct {
	Reason string
	Err    error
}

func (e *AuthExpiredError) Error() string {
	if e.Err != nil {
		return "recon: session expired (" + e.Reason + "): " + e.Err.Error()
	}
	return "recon: session expired (" + e.Reason + ")"
}

func (e *AuthExpiredError) Unwrap() error { return e.Err }

// IsAuthExpired reports whether err means the user must log in again.
func IsAuthExpired(err error) bool {
	var e *AuthExpiredError
	return asError(err, &e)
}

// unauthTransport builds a transport with no Authenticator, for the three calls
// that are made before a token exists (and for refresh, which must not carry an
// Authorization header).
//
// It takes the whole config rather than loose arguments so that WithBaseURL is
// honoured. An earlier version hardcoded the production BaseURL here, which
// meant a test pointed at httptest still dialled recon.com for real — exactly
// the kind of mistake this package must not make.
func unauthTransport(cfg config) *core.Transport {
	return &core.Transport{
		BaseURL:   cfg.baseURL,
		HTTP:      cfg.http,
		UserAgent: cfg.userAgent,
		Accept:    "application/json",
		Decorate:  addCulture,
	}
}

// addCulture appends the culture parameter Recon expects on every call.
func addCulture(r *core.Request) {
	if r.Query == nil || r.Query.Get("culture") == "" {
		r.SetQuery("culture", Culture)
	}
}
