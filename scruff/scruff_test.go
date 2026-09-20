package scruff

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"hash"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/vladzaharia/scruff-recon-sdk/core"
)

// The signature base is lon~lat~client_version~device_type. A widely repeated
// write-up says device_id in the third position; that produces a signature the
// server rejects.
func TestSignRequestUsesClientVersionNotDeviceID(t *testing.T) {
	const lat, lon, ts = "37.7749", "-122.4194", "1789866828.155000"

	got := signRequest(lat, lon, ts)

	// Recompute independently from the documented definition.
	base := lon + "~" + lat + "~" + ClientVersion + "~" + DeviceTypeAndroid
	want := hmacHex(base, base+":"+ts)
	if got != want {
		t.Fatalf("signature = %s, want %s", got, want)
	}

	// And assert the wrong-but-popular form differs, so a regression to it is
	// caught rather than silently shipped.
	wrongBase := lon + "~" + lat + "~" + "droid-deadbeef" + "~" + DeviceTypeAndroid
	if got == hmacHex(wrongBase, wrongBase+":"+ts) {
		t.Error("signature matched the device_id form; the base string is wrong")
	}

	if len(got) != 64 {
		t.Errorf("signature length = %d, want 64 hex chars", len(got))
	}
	if got != strings.ToLower(got) {
		t.Error("signature must be lowercase hex")
	}
}

func hmacHex(key, msg string) string {
	h := hmacNew(key)
	h.Write([]byte(msg))
	return hex.EncodeToString(h.Sum(nil))
}

// The timestamp is decimal seconds with six fraction digits, not an integer.
func TestSignatureTimestampFormat(t *testing.T) {
	ts := signatureTimestamp(time.Unix(1789866828, 155000000))
	if ts != "1789866828.155000" {
		t.Errorf("timestamp = %q, want 1789866828.155000", ts)
	}
	if !strings.Contains(ts, ".") {
		t.Error("timestamp must be a decimal, not an integer")
	}
	if parts := strings.Split(ts, "."); len(parts[1]) != 6 {
		t.Errorf("fraction digits = %d, want 6", len(parts[1]))
	}
}

// Unknown coordinates are sent as the string "0.0", not "null" and not omitted.
func TestLatLngStringsUnknownIsZeroPointZero(t *testing.T) {
	lat, lon, provider := latLngStrings(LatLng{})
	if lat != "0.0" || lon != "0.0" {
		t.Errorf("= (%q, %q), want (\"0.0\", \"0.0\")", lat, lon)
	}
	if provider != "unknown" {
		t.Errorf("provider = %q, want unknown", provider)
	}

	lat, lon, provider = latLngStrings(LatLng{Latitude: 37.7749, Longitude: -122.4194, Provider: "fused"})
	if lat != "37.7749" || lon != "-122.4194" || provider != "fused" {
		t.Errorf("= (%q, %q, %q)", lat, lon, provider)
	}
}

// Shapes are asserted against captured traffic from the real app. Getting them
// wrong does not fail loudly — the server accepts other lengths — it just makes
// our requests trivially distinguishable from the app's.
func TestIdentifierShapes(t *testing.T) {
	d := NewDeviceID()
	if !strings.HasPrefix(d, "droid-") || len(d) != 38 {
		t.Errorf("device id = %q (%d chars), want droid- plus 32 hex = 38", d, len(d))
	}
	// Case is not cosmetic: device_id was UPPERCASE in 203 of 203 captured
	// requests, while hardware_id was lowercase in 219 of 219. Emitting the
	// wrong case is a fingerprint, so both directions are asserted.
	if body := strings.TrimPrefix(d, "droid-"); body != strings.ToUpper(body) {
		t.Errorf("device id body = %q, want UPPERCASE hex", body)
	}
	h := NewHardwareID()
	if !strings.HasPrefix(h, "droid-") || len(h) != 22 {
		t.Errorf("hardware id = %q (%d chars), want droid- plus 16 hex = 22", h, len(h))
	}
	if body := strings.TrimPrefix(h, "droid-"); body != strings.ToLower(body) {
		t.Errorf("hardware id body = %q, want lowercase hex", body)
	}
	// Explicitly NOT a UUID — an earlier implementation used one.
	if strings.Contains(h, "-") != strings.HasPrefix(h, "droid-") || strings.Count(h, "-") != 1 {
		t.Errorf("hardware id = %q, want a single droid- prefix and no UUID dashes", h)
	}
	g := NewMessageGUID()
	if len(g) != 32 || g != strings.ToUpper(g) {
		t.Errorf("message guid = %q, want 32 UPPERCASE hex", g)
	}
	k, iv := NewAESMaterial()
	if len(k) != 32 || len(iv) != 32 {
		t.Errorf("aes material lengths = (%d, %d), want (32, 32)", len(k), len(iv))
	}
}

// Outbound guids are uppercase and inbound ones lowercase; comparing them
// case-sensitively duplicates your own sent messages.
func TestSameGUIDIsCaseInsensitive(t *testing.T) {
	up := "ABCDEF0123456789ABCDEF0123456789"
	if !SameGUID(up, strings.ToLower(up)) {
		t.Error("guids differing only in case must compare equal")
	}
	if SameGUID(up, "0123456789ABCDEF0123456789ABCDEF") {
		t.Error("different guids must not compare equal")
	}
}

func newTestClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(Session{
		DeviceID:   "droid-" + strings.Repeat("a", 40),
		HardwareID: "hw-1",
		ProfileID:  "1001",
	}, WithBaseURL(srv.URL), WithHTTPClient(srv.Client()),
		// Disable throttling so tests are not slow.
		WithRateLimit(nil))
}

// Identity goes into the query string for GET and the body for POST. This is
// the whole reason core.Authenticator takes a *core.Request.
func TestIdentityParamsRouteByMethod(t *testing.T) {
	t.Run("GET uses the query string", func(t *testing.T) {
		var q url.Values
		c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q = r.URL.Query()
			_, _ = w.Write([]byte(`{"results":[]}`))
		}))
		if _, err := c.Inbox(context.Background(), InboxOptions{}); err != nil {
			t.Fatalf("Inbox: %v", err)
		}
		for k, want := range map[string]string{
			"device_id":      "droid-" + strings.Repeat("a", 40),
			"client_version": ClientVersion, "client_semver": ClientSemver,
			"flavor": FlavorScruff, "device_type": DeviceTypeAndroid,
			"hardware_id": "hw-1",
		} {
			if q.Get(k) != want {
				t.Errorf("query %s = %q, want %q", k, q.Get(k), want)
			}
		}
		if q.Has("signature") {
			t.Error("a request carrying device_id must not also be signed")
		}
	})

	t.Run("POST uses the form body", func(t *testing.T) {
		var form url.Values
		c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			form = r.PostForm
			w.WriteHeader(http.StatusOK)
		}))
		if err := c.MarkInboxViewed(context.Background(), time.Unix(1789866828, 0)); err != nil {
			t.Fatalf("MarkInboxViewed: %v", err)
		}
		if form.Get("device_id") == "" {
			t.Errorf("form = %v, want device_id in the body", form)
		}
		if form.Get("request_guid") == "" {
			t.Error("every non-GET must carry a request_guid")
		}
	})
}

// With no device id there is no choice but to sign.
func TestUnboundSessionIsSigned(t *testing.T) {
	var q url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	c := New(Session{HardwareID: "hw-1"},
		WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithRateLimit(nil))
	_, _ = c.Inbox(context.Background(), InboxOptions{})

	if q.Get("signature") == "" || q.Get("timestamp") == "" {
		t.Errorf("query = %v, want a signature and timestamp", q)
	}
	if q.Has("device_id") {
		t.Error("must not send an empty device_id")
	}
}

// connect is the single exception: it carries BOTH a device id and a signature.
func TestConnectForcesSignatureAlongsideDeviceID(t *testing.T) {
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm
		w.WriteHeader(http.StatusOK) // 200 with an empty body, as the API does
	}))
	defer srv.Close()

	cfg := newConfig(WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithRateLimit(nil))
	s := Session{DeviceID: NewDeviceID(), HardwareID: "hw-1", AES256Key: "k", AES256IV: "v"}
	if err := connect(context.Background(), cfg, s, Credentials{Email: "a@b.c", Password: "pw"}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	if form.Get("device_id") == "" {
		t.Error("connect must carry a device_id")
	}
	if form.Get("signature") == "" {
		t.Error("connect must ALSO carry a signature — this is the one exception")
	}
	if form.Get("email") != "a@b.c" || form.Get("password") != "pw" {
		t.Error("credentials missing from the connect body")
	}
	if form.Get("refresh_token") != "false" {
		t.Errorf("refresh_token = %q, want the literal false", form.Get("refresh_token"))
	}
	for _, k := range []string{"aes256_key", "aes256_iv", "build", "user_agent", "locale"} {
		if form.Get(k) == "" {
			t.Errorf("device descriptor field %q missing", k)
		}
	}
}

// Chat pages by version, and a 404 is the normal end-of-history signal.
func TestChatUsesVersionCursorAndTolerates404(t *testing.T) {
	var q url.Values
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		if q.Get("max_version") == "5" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"profile_id":2002,"results":[],"max_read_version":42}`))
	}))
	ctx := context.Background()

	p, err := c.Chat(ctx, "2002", ChatOptions{})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if p.MaxReadVersion != 42 {
		t.Errorf("max_read_version = %d, want 42", p.MaxReadVersion)
	}
	if q.Get("inbox_style") != "2" {
		t.Errorf("inbox_style = %q, want 2", q.Get("inbox_style"))
	}
	if q.Get("free_features") != "[]" {
		t.Errorf("free_features = %q, want []", q.Get("free_features"))
	}

	// A 404 means "no more messages", not failure.
	if _, err := c.Chat(ctx, "2002", ChatOptions{MaxVersion: 5}); err != nil {
		t.Errorf("a 404 should terminate backfill cleanly, got %v", err)
	}
}

// The app emits parts in a fixed order; matching it keeps our requests
// indistinguishable from the real client.
func TestSendMultipartOrderAndContentType(t *testing.T) {
	var order []string
	var ct string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct = r.Header.Get("Content-Type")
		mr, err := r.MultipartReader()
		if err != nil {
			t.Errorf("MultipartReader: %v", err)
			return
		}
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			order = append(order, p.FormName())
		}
		_, _ = w.Write([]byte(`{"results":{"guid":"ABC"}}`))
	}))

	_, err := c.Send(context.Background(), "2002", SendOptions{
		Text: "hi", Image: []byte{0xff, 0xd8}, ImageMime: "image/jpg",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if !strings.HasPrefix(ct, "multipart/mixed") {
		t.Errorf("Content-Type = %q, want multipart/mixed", ct)
	}
	// image precedes the scalar fields, and recipient/guid/request_guid/
	// message_type precede message.
	want := []string{"image", "recipient", "guid", "request_guid", "message_type", "message"}
	for i, w := range want {
		if i >= len(order) || order[i] != w {
			t.Fatalf("part order = %v, want it to begin %v", order, want)
		}
	}
}

func TestSendUsesGUIDAsRequestGUID(t *testing.T) {
	var guid, reqGUID string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := readMixedParts(t, r)
		guid, reqGUID = parts["guid"], parts["request_guid"]
		_, _ = w.Write([]byte(`{"results":{"guid":"` + guid + `"}}`))
	}))
	got, err := c.SendText(context.Background(), "2002", "hi")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if guid != reqGUID {
		t.Errorf("guid=%q request_guid=%q, want them equal on a send", guid, reqGUID)
	}
	if got != guid {
		t.Errorf("returned guid = %q, want %q", got, guid)
	}
	if guid != strings.ToUpper(guid) {
		t.Errorf("outbound guid %q must be uppercase", guid)
	}
}

func TestReactionEncoding(t *testing.T) {
	var parts map[string]string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts = readMixedParts(t, r)
		_, _ = w.Write([]byte(`{"results":{"guid":"X"}}`))
	}))
	if _, err := c.React(context.Background(), "2002", "TARGETGUID", "🔥"); err != nil {
		t.Fatalf("React: %v", err)
	}
	if parts["message_type"] != "8" {
		t.Errorf("message_type = %s, want 8", parts["message_type"])
	}
	if parts["reaction"] != "🔥" || parts["reacted_to"] != "TARGETGUID" {
		t.Errorf("reaction parts = %v", parts)
	}
}

func TestMessageTextAndReactionDecoding(t *testing.T) {
	t.Run("plain text", func(t *testing.T) {
		var m Message
		if err := json.Unmarshal([]byte(`{"guid":"a","message":"hello"}`), &m); err != nil {
			t.Fatal(err)
		}
		if m.Text() != "hello" {
			t.Errorf("Text() = %q, want hello", m.Text())
		}
	})

	t.Run("reaction as an object", func(t *testing.T) {
		var m Message
		err := json.Unmarshal([]byte(
			`{"guid":"a","message_type":8,"message":{"reacted_to":"T1","reaction":"🔥"}}`), &m)
		if err != nil {
			t.Fatal(err)
		}
		emoji, target, ok := m.Reaction()
		if !ok || emoji != "🔥" || target != "T1" {
			t.Errorf("Reaction() = (%q, %q, %v)", emoji, target, ok)
		}
	})

	t.Run("reaction as an encoded string", func(t *testing.T) {
		var m Message
		err := json.Unmarshal([]byte(
			`{"guid":"a","message_type":8,"message":"{\"reacted_to\":\"T1\",\"reaction\":\"🔥\"}"}`), &m)
		if err != nil {
			t.Fatal(err)
		}
		emoji, target, ok := m.Reaction()
		if !ok || emoji != "🔥" || target != "T1" {
			t.Errorf("Reaction() = (%q, %q, %v)", emoji, target, ok)
		}
	})

	t.Run("non-reaction returns false", func(t *testing.T) {
		m := Message{MessageType: MessageTypeText}
		if _, _, ok := m.Reaction(); ok {
			t.Error("a text message is not a reaction")
		}
	})
}

// A conversation has no id of its own; its ID is the peer's profile id.
func TestConversationPeerID(t *testing.T) {
	var c Conversation
	if err := json.Unmarshal([]byte(`{"id":2002,"name":"peer"}`), &c); err != nil {
		t.Fatal(err)
	}
	if c.PeerID() != "2002" {
		t.Errorf("PeerID() = %q, want 2002", c.PeerID())
	}
}

// K = SHA-256(key string), IV = MD5(iv string) — of the hex STRINGS, not the
// bytes they encode.
func TestDeriveKeyIVHashesTheStringNotTheBytes(t *testing.T) {
	const k, v = "0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210"
	key, iv := deriveKeyIV(k, v)

	wantKey := sha256.Sum256([]byte(k))
	wantIV := md5.Sum([]byte(v))
	if string(key) != string(wantKey[:]) {
		t.Error("key must be SHA-256 of the hex string")
	}
	if string(iv) != string(wantIV[:]) {
		t.Error("iv must be MD5 of the hex string")
	}

	// The wrong-but-plausible approach: hex-decode first.
	raw, _ := hex.DecodeString(k)
	if wrong := sha256.Sum256(raw); string(key) == string(wrong[:]) {
		t.Error("key matched the hex-decoded form; that would be wrong")
	}
	if len(key) != 32 || len(iv) != 16 {
		t.Errorf("sizes = (%d, %d), want (32, 16)", len(key), len(iv))
	}
}

// Round-trip a frame encrypted exactly as the server would.
func TestDecryptRoundTrip(t *testing.T) {
	const aesKey, aesIV = "0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210"
	r := &Realtime{}
	r.key, r.iv = deriveKeyIV(aesKey, aesIV)

	plain := []byte(`{"class":201,"id":"e1","results":{"message":{"guid":"abc"}}}`)
	frame := encryptFrame(t, r.key, r.iv, plain)

	got, err := r.decrypt(frame)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(got) != string(plain) {
		t.Errorf("= %q, want %q", got, plain)
	}
}

func TestDecryptRejectsMisalignedFrames(t *testing.T) {
	r := &Realtime{}
	r.key, r.iv = deriveKeyIV("k", "v")
	// 7 raw bytes: not a multiple of the AES block size.
	if _, err := r.decrypt([]byte(base64.StdEncoding.EncodeToString([]byte("1234567")))); err == nil {
		t.Error("want an error for a non-block-aligned frame")
	}
}

func TestPKCS7Strip(t *testing.T) {
	if got, err := pkcs7Strip([]byte{'a', 'b', 2, 2}); err != nil || string(got) != "ab" {
		t.Errorf("= (%q, %v), want (ab, nil)", got, err)
	}
	for _, bad := range [][]byte{
		{},
		{'a', 'b', 0},    // zero pad byte
		{'a', 'b', 9},    // pad longer than the buffer
		{'a', 'b', 2, 3}, // inconsistent
	} {
		if _, err := pkcs7Strip(bad); err == nil {
			t.Errorf("pkcs7Strip(%v) should fail", bad)
		}
	}
}

// Class 208 is typing, and 209 is the read receipt — a pairing that several
// public write-ups get backwards.
func TestDispatchClassRouting(t *testing.T) {
	c := New(Session{ProfileID: "1001"})
	r := &Realtime{c: c}

	tests := []struct {
		name  string
		frame string
		check func(t *testing.T, ev core.Event)
	}{
		{
			name:  "201 inbound message",
			frame: `{"class":201,"results":{"message":{"guid":"g1","sender_id":2002,"recipient_id":1001,"version":7}}}`,
			check: func(t *testing.T, ev core.Event) {
				m, ok := ev.(MessageEvent)
				if !ok {
					t.Fatalf("event = %T, want MessageEvent", ev)
				}
				if m.Own {
					t.Error("class 201 is inbound, not an echo")
				}
				if m.PeerID != "2002" {
					t.Errorf("peer = %q, want the sender 2002", m.PeerID)
				}
			},
		},
		{
			name:  "200 own echo",
			frame: `{"class":200,"request_guid":"RG1","results":{"message":{"guid":"g2","sender_id":1001,"recipient_id":2002}}}`,
			check: func(t *testing.T, ev core.Event) {
				m, ok := ev.(MessageEvent)
				if !ok {
					t.Fatalf("event = %T, want MessageEvent", ev)
				}
				if !m.Own {
					t.Error("class 200 is the echo of your own send")
				}
				if m.PeerID != "2002" {
					t.Errorf("peer = %q, want the recipient 2002", m.PeerID)
				}
				if m.RequestGUID != "RG1" {
					t.Errorf("request_guid = %q, want RG1", m.RequestGUID)
				}
			},
		},
		{
			name:  "208 is typing",
			frame: `{"class":208,"results":{"profile_id":2002,"target_id":1001}}`,
			check: func(t *testing.T, ev core.Event) {
				if _, ok := ev.(TypingEvent); !ok {
					t.Fatalf("event = %T, want TypingEvent — 208 is typing, not a read receipt", ev)
				}
			},
		},
		{
			name:  "209 is the read receipt",
			frame: `{"class":209,"results":{"target_id":2002,"max_read_version":87}}`,
			check: func(t *testing.T, ev core.Event) {
				rr, ok := ev.(ReadReceiptEvent)
				if !ok {
					t.Fatalf("event = %T, want ReadReceiptEvent", ev)
				}
				if rr.MaxReadVersion != 87 || rr.PeerID != "2002" {
					t.Errorf("= %+v", rr)
				}
			},
		},
		{
			name:  "202 unsend",
			frame: `{"class":202,"results":{"message":{"guid":"g3","sender_id":2002,"recipient_id":1001}}}`,
			check: func(t *testing.T, ev core.Event) {
				if _, ok := ev.(UnsendEvent); !ok {
					t.Fatalf("event = %T, want UnsendEvent", ev)
				}
			},
		},
		{
			name:  "418 session invalid",
			frame: `{"class":418,"results":{}}`,
			check: func(t *testing.T, ev core.Event) {
				if _, ok := ev.(SessionInvalidEvent); !ok {
					t.Fatalf("event = %T, want SessionInvalidEvent", ev)
				}
			},
		},
		{
			// A class the server invented and this SDK has never heard of must
			// still reach the caller with its payload intact, rather than being
			// dropped on the floor.
			name:  "unknown class surfaces rather than vanishing",
			frame: `{"class":9999,"results":{"a":1}}`,
			check: func(t *testing.T, ev core.Event) {
				u, ok := ev.(UnknownEvent)
				if !ok {
					t.Fatalf("event = %T, want UnknownEvent", ev)
				}
				if u.Class != 9999 {
					t.Errorf("class = %d, want 9999", u.Class)
				}
				if len(u.Payload) == 0 {
					t.Error("payload dropped; it is the only thing we know about this class")
				}
			},
		},
		{
			// A callback class confirms a request of yours and echoes its guid.
			// Surfacing that as an ack is what lets a caller tell its own album
			// creation apart from someone else's.
			name:  "callback class becomes an ack carrying the request guid",
			frame: `{"class":100,"request_guid":"g-1","results":{}}`,
			check: func(t *testing.T, ev core.Event) {
				a, ok := ev.(AckEvent)
				if !ok {
					t.Fatalf("event = %T, want AckEvent", ev)
				}
				if a.Class != ClassAlbumCreate {
					t.Errorf("class = %d, want %d", a.Class, ClassAlbumCreate)
				}
				if a.Name != "AlbumCreate" {
					t.Errorf("name = %q, want AlbumCreate", a.Name)
				}
				if a.Kind != ClassKindPost {
					t.Errorf("kind = %q, want %q", a.Kind, ClassKindPost)
				}
				if a.RequestGUID != "g-1" {
					t.Errorf("request guid = %q; without it an ack cannot be correlated", a.RequestGUID)
				}
			},
		},
		{
			// Standard classes have no originating request, so an ack would be
			// meaningless for them.
			name:  "standard class does not become an ack",
			frame: `{"class":1205,"results":{}}`,
			check: func(t *testing.T, ev core.Event) {
				if _, ok := ev.(AckEvent); ok {
					t.Fatalf("Standard class %d became an AckEvent", 1205)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got core.Event
			r.dispatch([]byte(tc.frame), func(ev core.Event) { got = ev })
			if got == nil {
				t.Fatal("no event emitted")
			}
			tc.check(t, got)
		})
	}
}

// The poll parameter is a JSON array literal in one query value, not a
// comma-separated list.
func TestPollEncodesGUIDsAsJSONArray(t *testing.T) {
	var raw string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.Query().Get("request_guids")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	if _, err := c.Poll(context.Background(), []string{"g1", "g2"}); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if raw != `["g1","g2"]` {
		t.Errorf("request_guids = %q, want a JSON array literal", raw)
	}
}

func TestAvatarURLPrefersServerCDN(t *testing.T) {
	c := New(Session{ProfileCDN: "https://cdn.example.test/"})
	p := Profile{ProfilePhotos: []ProfilePhoto{{FullsizeKey: "abc-full", ThumbnailKey: "abc-thumb"}}}
	if got := c.AvatarURL(p); got != "https://cdn.example.test/abc-full" {
		t.Errorf("= %q, want the server CDN and the fullsize key", got)
	}
	if got := New(Session{}).AvatarURL(Profile{}); got != "" {
		t.Errorf("= %q, want empty when there is no photo", got)
	}
}

func TestRealtimeRequiresAESMaterial(t *testing.T) {
	if _, err := New(Session{ProfileID: "1"}).Realtime(); err == nil {
		t.Error("want an error when the session has no AES material")
	}
	if _, err := New(Session{ProfileID: "1", AES256Key: "k", AES256IV: "v"}).Realtime(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestErrorsAreAPIErrors(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// SCRUFF's 400s are empty-bodied with an HTML content type.
		w.Header().Set("Content-Type", "text/html;charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
	}))
	_, err := c.Inbox(context.Background(), InboxOptions{})
	apiErr, ok := core.AsAPIError(err)
	if !ok {
		t.Fatalf("err = %T, want *core.APIError", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", apiErr.StatusCode)
	}
}

func TestGridSendsLocationAndSort(t *testing.T) {
	var q url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		_, _ = w.Write([]byte(`{"results":[],"block_size":25,"cache_id":"c1"}`))
	}))
	defer srv.Close()

	c := New(Session{DeviceID: "droid-x", HardwareID: "hw", ProfileID: "1"},
		WithBaseURL(srv.URL), WithHTTPClient(srv.Client()), WithRateLimit(nil),
		WithLocation(LatLng{Latitude: 37.7749, Longitude: -122.4194, Provider: "fused"}))

	page, err := c.Nearby(context.Background(), GridOptions{Sort: SortDistance, Offset: 0})
	if err != nil {
		t.Fatalf("Nearby: %v", err)
	}
	if q.Get("latitude") != "37.7749" || q.Get("location_provider") != "fused" {
		t.Errorf("query = %v, want the configured location", q)
	}
	if q.Get("query_sort_type") != "0" {
		t.Errorf("query_sort_type = %q, want 0", q.Get("query_sort_type"))
	}
	// block_size is the server's stride, not our requested limit.
	if page.BlockSize != 25 || page.CacheID != "c1" {
		t.Errorf("page = %+v, want block_size 25 and cache_id c1", page)
	}
}

// --- helpers ---------------------------------------------------------------

func encryptFrame(t *testing.T, key, iv, plain []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	pad := aes.BlockSize - len(plain)%aes.BlockSize
	padded := append(append([]byte{}, plain...), bytesRepeat(byte(pad), pad)...)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return []byte(base64.StdEncoding.EncodeToString(out))
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

// readMixedParts reads a multipart/mixed body.
//
// net/http's ParseMultipartForm only understands multipart/form-data, so a
// server receiving SCRUFF's multipart/mixed send must read the parts itself.
func readMixedParts(t *testing.T, r *http.Request) map[string]string {
	t.Helper()
	out := map[string]string{}
	mr, err := r.MultipartReader()
	if err != nil {
		t.Fatalf("MultipartReader: %v", err)
	}
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(p)
		out[p.FormName()] = buf.String()
	}
	return out
}

// hmacNew keeps the test's HMAC construction independent of the implementation.
func hmacNew(key string) hash.Hash { return hmac.New(sha256.New, []byte(key)) }

// The class table is the SDK's copy of docs/api/scruff-realtime.md section 4.
// These pin the handful whose meaning is most often reported wrongly.
func TestClassNameAndKind(t *testing.T) {
	for _, tc := range []struct {
		class int
		name  string
		kind  string
	}{
		// 208 and 209 are transposed in several public write-ups.
		{ClassChatRecipientTyping, "ChatRecipientTyping", ClassKindStandard},
		{ClassChatMessageViewed, "ChatMessageViewed", ClassKindStandard},
		{ClassAlbumCreate, "AlbumCreate", ClassKindPost},
		{ClassAlbumPermissionRevoke, "AlbumPermissionRevoke", ClassKindDelete},
		{ClassWoof, "Woof", ClassKindStandard},
	} {
		if got := ClassName(tc.class); got != tc.name {
			t.Errorf("ClassName(%d) = %q, want %q", tc.class, got, tc.name)
		}
		if got := ClassKind(tc.class); got != tc.kind {
			t.Errorf("ClassKind(%d) = %q, want %q", tc.class, got, tc.kind)
		}
	}
	if ClassName(9999) != "" || ClassKind(9999) != "" {
		t.Error("an unknown class should report empty name and kind, not a guess")
	}
}
