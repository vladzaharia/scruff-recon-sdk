package scruff

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/coder/websocket"
	"github.com/vladzaharia/scruff-recon-sdk/core"
)

// Realtime class codes. The protocol defines 87; these are the ones a
// messaging client needs. See docs/api/scruff-realtime.md for the full table.
const (
	ClassWoof                  = 1
	ClassAlbum                 = 2
	ClassMatch                 = 3
	ClassView                  = 5
	ClassChatMessageDelivered  = 200 // echo of your own send
	ClassChatMessageReceived   = 201 // inbound message
	ClassChatMessageUnsend     = 202
	ClassChatThreadDelete      = 203
	ClassChatInboxDelete       = 204
	ClassChatMessageUpdated    = 207
	ClassChatRecipientTyping   = 208 // typing, NOT a read receipt
	ClassChatMessageViewed     = 209 // read receipt
	ClassAccountRegisterNeeded = 418
	ClassMomentAvailable       = 1702
)

const realtimePingInterval = 5 * time.Second

// Events emitted by Realtime.
type (
	// MessageEvent is an inbound message (class 201) or the echo of your own
	// send (class 200).
	MessageEvent struct {
		core.EventMarker
		Message Message
		// PeerID is the other party, whichever direction the message went.
		PeerID string
		// Own reports that this is the echo of a message you sent.
		Own bool
		// RequestGUID correlates an echo to the send that caused it.
		RequestGUID string
	}

	// UnsendEvent reports that a message was retracted.
	UnsendEvent struct {
		core.EventMarker
		Message Message
		PeerID  string
	}

	// TypingEvent is a typing indicator from a peer.
	TypingEvent struct {
		core.EventMarker
		PeerID string
	}

	// ReadReceiptEvent is a watermark: every message at or below MaxReadVersion
	// has been read by the peer.
	ReadReceiptEvent struct {
		core.EventMarker
		PeerID         string
		MaxReadVersion int64
	}

	// SessionInvalidEvent reports class 418: the session must be re-registered.
	SessionInvalidEvent struct {
		core.EventMarker
	}

	// SocialEvent is a woof, view, match, album share, or moment.
	SocialEvent struct {
		core.EventMarker
		Class int
	}

	// UnknownEvent carries a class this package does not model, so callers can
	// log it rather than silently dropping it. Around 80 classes exist that a
	// messaging client has no use for.
	UnknownEvent struct {
		core.EventMarker
		Class   int
		Payload json.RawMessage
	}
)

// Realtime is the encrypted WebSocket push channel.
//
// It is server-to-client only. There is no backlog, no acknowledgement, and no
// resume token: treat every event as a trigger to reconcile over REST, which is
// authoritative.
type Realtime struct {
	c *Client

	// Backoff controls reconnect delay.
	Backoff core.Backoff

	key []byte
	iv  []byte
}

var _ core.EventSource = (*Realtime)(nil)

// Realtime returns the realtime channel for this client.
//
// It fails if the session has no AES material, which means the session was not
// produced by Login or ImportSession.
func (c *Client) Realtime() (*Realtime, error) {
	s := c.store.Get()
	if s.AES256Key == "" || s.AES256IV == "" {
		return nil, fmt.Errorf("scruff: session has no AES material; realtime is unavailable")
	}
	key, iv := deriveKeyIV(s.AES256Key, s.AES256IV)
	return &Realtime{
		c:       c,
		Backoff: core.Backoff{Min: 2 * time.Second, Max: time.Minute},
		key:     key, iv: iv,
	}, nil
}

// deriveKeyIV derives the stream cipher parameters.
//
//	K  = SHA-256(aes256_key as ASCII)
//	IV = MD5(aes256_iv as ASCII)
//
// Note both hash the 32-character hex STRING, not the bytes it encodes.
// Hex-decoding first produces the wrong key — an easy and silent mistake.
//
// The IV is fixed for the whole session: it is not prepended per frame and not
// rotated. That is weak (identical plaintext prefixes yield identical
// ciphertext prefixes) but it is what the protocol does.
func deriveKeyIV(aesKey, aesIV string) (key, iv []byte) {
	k := sha256.Sum256([]byte(aesKey))
	v := md5.Sum([]byte(aesIV))
	return k[:], v[:]
}

// Run connects and delivers events until ctx is cancelled, reconnecting as
// needed.
func (r *Realtime) Run(ctx context.Context, on func(core.Event)) error {
	return core.Supervise(ctx, r.Backoff, 30*time.Second, func(ctx context.Context) error {
		return r.connectOnce(ctx, on)
	})
}

func (r *Realtime) connectOnce(ctx context.Context, on func(core.Event)) error {
	s := r.c.store.Get()
	if s.SocketHost == "" {
		// Socket credentials come from register and rotate; fetch them.
		if _, err := r.c.Register(ctx); err != nil {
			return fmt.Errorf("scruff: register before realtime: %w", err)
		}
		s = r.c.store.Get()
	}
	if s.SocketHost == "" {
		return fmt.Errorf("scruff: no realtime socket host in session")
	}

	host := s.SocketHost
	if s.SocketPort != 0 && s.SocketPort != 443 {
		host += ":" + strconv.Itoa(s.SocketPort)
	}
	wsURL := "wss://" + host + "/"

	hc, _ := r.c.cfg.http.(*http.Client)
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient: hc,
		HTTPHeader: http.Header{
			// Authentication is entirely in these headers; there is no auth
			// frame after the upgrade.
			"X-Device-Id":  {s.DeviceID},
			"X-Token-Auth": {s.SocketPwd},
			"X-Request-Id": {core.RandHex(16)},
			"User-Agent":   {r.c.cfg.userAgent},
		},
	})
	if err != nil {
		return fmt.Errorf("scruff: realtime dial: %w", err)
	}
	conn.SetReadLimit(10 << 20)
	defer conn.CloseNow()

	pingCtx, stopPing := context.WithCancel(ctx)
	defer stopPing()
	go func() {
		t := time.NewTicker(realtimePingInterval)
		defer t.Stop()
		for {
			select {
			case <-pingCtx.Done():
				return
			case <-t.C:
				if err := conn.Ping(pingCtx); err != nil {
					return
				}
			}
		}
	}()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("scruff: realtime read: %w", err)
		}
		plain, err := r.decrypt(data)
		if err != nil {
			// A frame we cannot decrypt is not fatal on its own — skip it
			// rather than tearing down a working connection.
			continue
		}
		r.dispatch(plain, on)
	}
}

// decrypt turns a wire frame into plaintext JSON.
func (r *Realtime) decrypt(frame []byte) ([]byte, error) {
	// Frames are base64 text. Fall back to raw bytes if that fails, which is
	// what the app tolerates.
	raw := make([]byte, base64.StdEncoding.DecodedLen(len(frame)))
	n, err := base64.StdEncoding.Decode(raw, bytes.TrimSpace(frame))
	if err != nil {
		raw = frame
	} else {
		raw = raw[:n]
	}

	if len(raw) == 0 || len(raw)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("scruff: frame is not block-aligned (%d bytes)", len(raw))
	}
	block, err := aes.NewCipher(r.key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, r.iv).CryptBlocks(out, raw)
	return pkcs7Strip(out)
}

// pkcs7Strip removes PKCS#7 padding.
func pkcs7Strip(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("scruff: empty plaintext")
	}
	pad := int(b[len(b)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(b) {
		return nil, fmt.Errorf("scruff: bad padding byte %d", pad)
	}
	for _, c := range b[len(b)-pad:] {
		if int(c) != pad {
			return nil, fmt.Errorf("scruff: inconsistent padding")
		}
	}
	return b[:len(b)-pad], nil
}

// envelope is the shared frame wrapper.
type envelope struct {
	Class       int             `json:"class"`
	ID          string          `json:"id"`
	RequestGUID *string         `json:"request_guid"`
	Timestamp   int64           `json:"timestamp"`
	Backlog     int             `json:"backlog"`
	Results     json.RawMessage `json:"results"`
	// Some deliveries nest the payload under "message" instead of "results".
	Message json.RawMessage `json:"message"`
}

// payload returns whichever field carries the body.
func (e envelope) payload() json.RawMessage {
	if len(e.Results) > 0 && !bytes.Equal(e.Results, []byte("null")) {
		return e.Results
	}
	return e.Message
}

// dispatch decodes one frame and emits an event.
func (r *Realtime) dispatch(plain []byte, on func(core.Event)) {
	var env envelope
	if err := json.Unmarshal(plain, &env); err != nil {
		return
	}
	body := env.payload()
	me := r.c.ProfileID()

	reqGUID := ""
	if env.RequestGUID != nil {
		reqGUID = *env.RequestGUID
	}

	switch env.Class {
	case ClassChatMessageReceived, ClassChatMessageDelivered:
		m, ok := decodeMessage(body)
		if !ok {
			return
		}
		own := env.Class == ClassChatMessageDelivered
		peer := m.SenderID.String()
		if own || peer == me {
			peer = m.RecipientID.String()
		}
		on(MessageEvent{Message: m, PeerID: peer, Own: own, RequestGUID: reqGUID})

	case ClassChatMessageUnsend:
		m, ok := decodeMessage(body)
		if !ok {
			return
		}
		peer := m.SenderID.String()
		if peer == me {
			peer = m.RecipientID.String()
		}
		on(UnsendEvent{Message: m, PeerID: peer})

	case ClassChatRecipientTyping:
		var p struct {
			ProfileID json.Number `json:"profile_id"`
			TargetID  json.Number `json:"target_id"`
		}
		if json.Unmarshal(body, &p) != nil {
			return
		}
		peer := p.ProfileID.String()
		if peer == "" || peer == me {
			peer = p.TargetID.String()
		}
		on(TypingEvent{PeerID: peer})

	case ClassChatMessageViewed:
		var p struct {
			TargetID       json.Number `json:"target_id"`
			MaxReadVersion int64       `json:"max_read_version"`
		}
		if json.Unmarshal(body, &p) != nil {
			return
		}
		on(ReadReceiptEvent{PeerID: p.TargetID.String(), MaxReadVersion: p.MaxReadVersion})

	case ClassAccountRegisterNeeded:
		on(SessionInvalidEvent{})

	case ClassWoof, ClassAlbum, ClassMatch, ClassView, ClassMomentAvailable:
		on(SocialEvent{Class: env.Class})

	default:
		on(UnknownEvent{Class: env.Class, Payload: body})
	}
}

// decodeMessage unwraps a message payload, which may be the message object
// itself or an object with a "message" member.
func decodeMessage(body json.RawMessage) (Message, bool) {
	var wrapper struct {
		Message *Message `json:"message"`
	}
	if err := json.Unmarshal(body, &wrapper); err == nil && wrapper.Message != nil {
		return *wrapper.Message, true
	}
	var m Message
	if err := json.Unmarshal(body, &m); err == nil && m.GUID != "" {
		return m, true
	}
	return Message{}, false
}

// Poll fetches events by request guid over HTTP.
//
// This is the catch-up path when the WebSocket is unavailable. It returns only
// events YOU caused — unsolicited traffic such as inbound messages has no
// request guid and never appears here, so it is not a substitute for the socket
// or for polling the inbox.
func (c *Client) Poll(ctx context.Context, requestGUIDs []string) ([]json.RawMessage, error) {
	if len(requestGUIDs) == 0 {
		return nil, nil
	}
	// The parameter is a JSON array literal in a single query value.
	encoded, err := json.Marshal(requestGUIDs)
	if err != nil {
		return nil, err
	}
	var out results[[]json.RawMessage]
	err = c.tr.JSON(ctx, &core.Request{
		Method: http.MethodGet, Path: "/app/socket/poll",
		Query: map[string][]string{"request_guids": {string(encoded)}},
	}, &out)
	return out.Results, err
}
