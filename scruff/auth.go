package scruff

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vladzaharia/scruff-recon-sdk/core"
)

// Protocol constants identifying the client to SCRUFF.
const (
	// BaseURL is the API host.
	BaseURL = "https://cdn-api.scruffapp.com"

	// DefaultProfileCDN serves profile photos, unsigned. Prefer the value the
	// server returns in the register response over this constant.
	DefaultProfileCDN = "https://cdn-profilemedia.scruffapp.com"

	// ClientVersion is the packed form of ClientSemver: sprintf("%d.%02d%02d").
	// So 8.16.0 becomes 8.1600.
	ClientVersion = "8.1600"
	ClientSemver  = "8.16.0"
	// Build is the app build number.
	Build = "169074"

	// FlavorScruff is the app-family id (2 = Jack'd, 3 = GROWLr).
	FlavorScruff = "1"
	// DeviceTypeAndroid is the platform id.
	DeviceTypeAndroid = "2"

	// UserAgent matches the app's HTTP library.
	UserAgent = "okhttp/5.3.2"

	// locationUnknown is what the app sends when it has no fix. Note it is the
	// string "0.0", not "null" and not an omitted parameter.
	locationUnknown = "0.0"
)

// devicePrefix prefixes a generated device id.
const devicePrefix = "droid-"

// NewDeviceID generates a device id: "droid-" plus 40 hex characters.
//
// This is client-generated, not server-issued. Generate it once, bind it with
// Connect, and persist it — it is the entire credential and never expires.
func NewDeviceID() string { return devicePrefix + core.RandHex(20) }

// NewHardwareID generates a per-install identity.
func NewHardwareID() string { return core.NewUUID() }

// NewAESMaterial generates the realtime key and IV, 32 hex characters each.
//
// These are registered with the server at login and cannot be changed
// afterwards without re-registering, so persist them with the session.
func NewAESMaterial() (key, iv string) { return core.RandHex(16), core.RandHex(16) }

// NewMessageGUID generates an outbound message guid.
//
// Outbound guids are UPPERCASE; the server echoes them back lowercase. Always
// compare with SameGUID.
func NewMessageGUID() string {
	return strings.ToUpper(core.RandHex(16))
}

// signRequest computes the request signature.
//
//	base      = "<longitude>~<latitude>~<client_version>~<device_type>"
//	key       = UTF-8 bytes of base
//	message   = base + ":" + timestamp
//	signature = lowercase hex of HMAC-SHA256(key, message)
//
// The key and the message prefix being the same string is unusual but correct.
//
// The third component is client_version. A widely repeated write-up says
// device_id there; that is wrong and yields a signature the server rejects.
func signRequest(lat, lon, timestamp string) string {
	base := lon + "~" + lat + "~" + ClientVersion + "~" + DeviceTypeAndroid
	mac := hmac.New(sha256.New, []byte(base))
	mac.Write([]byte(base + ":" + timestamp))
	return hex.EncodeToString(mac.Sum(nil))
}

// signatureTimestamp formats a timestamp for signing: decimal seconds with six
// fraction digits, not an integer.
func signatureTimestamp(t time.Time) string {
	return strconv.FormatFloat(float64(t.UnixNano())/1e9, 'f', 6, 64)
}

// latLngStrings renders coordinates for the wire, substituting "0.0" when
// unknown.
func latLngStrings(loc LatLng) (lat, lon, provider string) {
	lat, lon = locationUnknown, locationUnknown
	if loc.Latitude != 0 || loc.Longitude != 0 {
		lat = strconv.FormatFloat(loc.Latitude, 'f', -1, 64)
		lon = strconv.FormatFloat(loc.Longitude, 'f', -1, 64)
	}
	provider = loc.Provider
	if provider == "" {
		provider = "unknown"
	}
	return
}

// authenticator injects the identity parameters every authenticated call needs.
//
// It writes through Request.SetParam rather than setting a header, because
// SCRUFF expects these in the query string for GET and in the body for POST —
// which is precisely why core.Authenticator takes a *core.Request.
type authenticator struct{ c *Client }

// Authorize implements core.Authenticator.
func (a *authenticator) Authorize(ctx context.Context, r *core.Request) error {
	s := a.c.store.Get()
	r.SetParam("hardware_id", s.HardwareID)
	r.SetParam("client_version", ClientVersion)
	r.SetParam("client_semver", ClientSemver)
	r.SetParam("flavor", FlavorScruff)
	r.SetParam("device_type", DeviceTypeAndroid)
	if s.DeviceID != "" {
		r.SetParam("device_id", s.DeviceID)
	} else {
		// No device id yet: sign instead. A request carries one or the other,
		// never both — except account/connect, which forces both explicitly.
		signInto(r, a.c.cfg.loc(), time.Now())
	}
	// Every non-GET carries an idempotency key.
	switch r.Method {
	case http.MethodGet, http.MethodHead:
	default:
		if !r.HasParam("request_guid") {
			r.SetParam("request_guid", core.RandHex(16))
		}
	}
	return nil
}

// signInto adds the signature block to a request.
func signInto(r *core.Request, loc LatLng, now time.Time) {
	lat, lon, provider := latLngStrings(loc)
	ts := signatureTimestamp(now)
	r.SetParam("timestamp", ts)
	r.SetParam("latitude", lat)
	r.SetParam("longitude", lon)
	r.SetParam("location_provider", provider)
	r.SetParam("signature", signRequest(lat, lon, ts))
}

// addRegisterParams adds the device descriptor that register, connect, and
// forgot all expect.
func addRegisterParams(r *core.Request, s Session, loc LatLng) {
	lat, lon, provider := latLngStrings(loc)
	set := func(k, v string) { r.SetParam(k, v) }

	set("aes256_key", s.AES256Key)
	set("aes256_iv", s.AES256IV)
	set("device_name", "beeper-connectors")
	set("system_name", "Android")
	set("device_os_version", "34")
	set("system_version", "34")
	set("locale", "en-US")
	set("build", Build)
	set("user_agent", dalvikUserAgent)
	set("register_count", "1")
	set("register_count_for_version", "1")
	set("latitude", lat)
	set("longitude", lon)
	set("location_provider", provider)
}

// dalvikUserAgent is sent as a form FIELD (not the HTTP header) on register and
// connect, describing the device.
const dalvikUserAgent = "Dalvik/2.1.0 (Linux; U; Android 14; sdk_gphone64_arm64 Build/UE1A.230829.050)"

// BootstrapRegister fetches the anonymous configuration.
//
// This is the call that returns 401 with a usable body: socket endpoint, CDN
// hosts, and feature flags for a caller with no device id. It is OPTIONAL —
// Connect plus Register is sufficient to obtain a session — but it is the
// documented first step and is useful for discovering the socket host before
// logging in.
//
// The 401 is expected and is not treated as an error.
func BootstrapRegister(ctx context.Context, opts ...Option) (*RegisterResponse, error) {
	cfg := newConfig(opts...)
	c := newClientWith(Session{HardwareID: NewHardwareID()}, cfg)

	var out RegisterResponse
	err := c.tr.JSON(ctx, registerRequest(Session{}, cfg.loc()), &out)
	if err != nil {
		// A 401 here carries the anonymous payload rather than an error.
		if e, ok := core.AsAPIError(err); ok && e.IsUnauthorized() && out.Socket.Host != "" {
			return &out, nil
		}
		return nil, err
	}
	return &out, nil
}

// registerRequest builds the register call.
func registerRequest(s Session, loc LatLng) *core.Request {
	r := &core.Request{Method: http.MethodPost, Path: "/app/account/register"}
	r.SetForm("debug", "false")
	r.SetForm("remote_configs", "{}")
	addRegisterParams(r, s, loc)
	return r
}

// Login binds a fresh device id to the account and loads the session.
//
// The sequence is exactly two calls that matter:
//
//  1. POST /app/account/connect — email, password, a client-generated device
//     id, AND a forced signature.
//  2. POST /app/account/register — device id only, unsigned.
//
// Step 1 is the one that trips people up: it breaks the otherwise universal
// "device id XOR signature" rule by carrying both.
func Login(ctx context.Context, creds Credentials, opts ...Option) (Session, error) {
	cfg := newConfig(opts...)

	key, iv := NewAESMaterial()
	s := Session{
		Email:      creds.Email,
		DeviceID:   NewDeviceID(),
		HardwareID: NewHardwareID(),
		AES256Key:  key,
		AES256IV:   iv,
	}

	if err := connect(ctx, cfg, s, creds); err != nil {
		return Session{}, err
	}

	c := newClientWith(s, cfg)
	reg, err := c.Register(ctx)
	if err != nil {
		return Session{}, fmt.Errorf("scruff: register after connect: %w", err)
	}
	if id := reg.Profile.ID.String(); id == "" || id == "0" {
		return Session{}, fmt.Errorf("scruff: register returned no profile; the device was not bound")
	}
	return c.Session(), nil
}

// connect performs the email/password gate.
//
// Success is a 200 with an EMPTY body — there is no token and nothing to parse.
// The caller keeps the device id it generated.
func connect(ctx context.Context, cfg config, s Session, creds Credentials) error {
	c := newClientWith(s, cfg)

	r := &core.Request{Method: http.MethodPost, Path: "/app/account/connect"}
	r.SetForm("email", creds.Email)
	r.SetForm("password", creds.Password)
	r.SetForm("refresh_token", "false")
	addRegisterParams(r, s, cfg.loc())
	// Force the signature even though a device id is present. Omitting it here
	// fails; this is the single exception to the XOR rule.
	signInto(r, cfg.location, time.Now())

	if err := c.tr.JSON(ctx, r, nil); err != nil {
		if e, ok := core.AsAPIError(err); ok && e.IsForbidden() {
			return fmt.Errorf("scruff: connect rejected, check the password: %w", err)
		}
		return err
	}
	return nil
}

// ImportSession resumes from device credentials captured elsewhere, skipping
// the email/password gate entirely.
//
// This is the reliable path when fresh-device login is blocked: take the device
// id, hardware id, and AES material from a working install and register with
// them.
func ImportSession(ctx context.Context, s Session, opts ...Option) (Session, error) {
	if s.DeviceID == "" {
		return Session{}, fmt.Errorf("scruff: ImportSession requires a device id")
	}
	if s.HardwareID == "" {
		s.HardwareID = NewHardwareID()
	}
	if s.AES256Key == "" || s.AES256IV == "" {
		s.AES256Key, s.AES256IV = NewAESMaterial()
	}

	c := newClientWith(s, newConfig(opts...))
	reg, err := c.Register(ctx)
	if err != nil {
		return Session{}, err
	}
	if !reg.Profile.LoggedIn {
		return Session{}, fmt.Errorf("scruff: device is not bound to an account")
	}
	return c.Session(), nil
}

// SignRequest computes the request signature.
//
// Exported so that callers replacing their own implementation can assert the
// base string has not drifted — getting the third component wrong (device_id
// instead of client_version) yields a signature the server rejects, and the
// failure mode is an opaque empty-bodied rejection.
func SignRequest(lat, lon, timestamp string) string { return signRequest(lat, lon, timestamp) }
