package recon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/vladzaharia/scruff-recon-sdk/core"
)

// SignalR framing and timing.
const (
	// recordSeparator terminates every SignalR frame.
	recordSeparator = 0x1e

	// pingInterval matches the web client. The server times out at 30s.
	pingInterval = 15 * time.Second

	// readLimit bounds a single frame.
	readLimit = 10 << 20
)

// SignalR message types.
const (
	sigInvocation = 1
	sigCompletion = 3
	sigPing       = 6
	sigClose      = 7
)

// Events emitted by Realtime. Callers type-switch on these.
type (
	// MessageEvent is an inbound message.
	//
	// The wire payload names the sender profileId and the body messageText,
	// which is the opposite of the REST message DTO. This type normalises to
	// the REST naming so both paths agree.
	MessageEvent struct {
		core.EventMarker
		ConversationID  string
		MessageID       string
		SenderProfileID string
		Text            string
		SentDate        time.Time
		AttachmentCount int
	}

	// ConversationCreatedEvent announces a new thread.
	//
	// You are NOT subscribed to it automatically. Realtime joins it for you;
	// without that join, every message in the thread is silently missed.
	ConversationCreatedEvent struct {
		core.EventMarker
		ConversationID string
	}

	// ReadReceiptEvent is a watermark: every message created at or before
	// ReadByEveryone has been read by the other participant.
	ReadReceiptEvent struct {
		core.EventMarker
		ConversationID string
		ReadByEveryone time.Time
	}

	// TypingEvent is a typing indicator from a peer.
	TypingEvent struct {
		core.EventMarker
		ConversationID string
		ProfileID      string
		IsTyping       bool
	}

	// UnreadCountEvent carries a counter. Kind is one of "conversations",
	// "cruises", "visits", "notifications".
	UnreadCountEvent struct {
		core.EventMarker
		Kind  string
		Count int
	}

	// ConversationsSyncEvent is the bulk conversation list the server pushes
	// immediately after joining — a free full sync that saves a REST round
	// trip. It may be empty.
	ConversationsSyncEvent struct {
		core.EventMarker
		Conversations []Conversation
	}
)

// Realtime is the SignalR push channel.
//
// It is receive-oriented: the hub exposes a send method but the web client never
// uses it, so send messages over REST and use this for inbound traffic, typing,
// read receipts, and unread counts. Typing is the one thing sent over the hub.
type Realtime struct {
	c *Client

	// Backoff controls reconnect delay.
	Backoff core.Backoff

	conn *websocket.Conn
}

var _ core.EventSource = (*Realtime)(nil)

// Realtime returns the realtime channel for this client.
func (c *Client) Realtime() *Realtime {
	return &Realtime{c: c, Backoff: core.Backoff{Min: 2 * time.Second, Max: time.Minute}}
}

// Run connects and delivers events until ctx is cancelled, reconnecting as
// needed.
//
// on is called from a single goroutine and must not block for long: a slow
// handler stalls the read loop and eventually the connection.
//
// There is no backlog and no resume — events that occur while disconnected are
// lost. Reconcile over REST after any reconnect.
func (r *Realtime) Run(ctx context.Context, on func(core.Event)) error {
	return core.Supervise(ctx, r.Backoff, 30*time.Second, func(ctx context.Context) error {
		return r.connectOnce(ctx, on)
	})
}

// negotiateResponse is the handshake that precedes the WebSocket upgrade.
type negotiateResponse struct {
	NegotiateVersion int    `json:"negotiateVersion"`
	ConnectionID     string `json:"connectionId"`
	ConnectionToken  string `json:"connectionToken"`
}

// negotiate performs the SignalR negotiation.
//
// The token goes in the query string with NO "Bearer " prefix and there is no
// Authorization header — the opposite of every REST call.
func (r *Realtime) negotiate(ctx context.Context, token string) (*negotiateResponse, error) {
	q := url.Values{
		"access_token":     {token},
		"ngsw-bypass":      {"true"},
		"negotiateVersion": {"1"},
	}
	u := r.c.cfg.baseURL + "/signalR/hubs/signalr/negotiate?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return nil, fmt.Errorf("recon: build negotiate: %w", err)
	}
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-SignalR-User-Agent", "Microsoft SignalR/8.0")
	req.Header.Set("User-Agent", r.c.cfg.userAgent)
	req.Header.Set("Content-Length", "0")

	resp, err := r.c.cfg.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("recon: negotiate: %w", err)
	}
	defer resp.Body.Close()

	var body bytes.Buffer
	if _, err := body.ReadFrom(resp.Body); err != nil {
		return nil, fmt.Errorf("recon: read negotiate: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &core.APIError{
			Method: http.MethodPost, URL: u,
			StatusCode: resp.StatusCode,
			Body:       core.TruncateBody(body.Bytes()),
			Header:     resp.Header,
		}
	}

	var out negotiateResponse
	if err := json.Unmarshal(body.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("recon: decode negotiate: %w", err)
	}
	if out.ConnectionToken == "" {
		// With negotiateVersion=1 the upgrade needs connectionToken, not
		// connectionId; without it the connection silently fails.
		return nil, fmt.Errorf("recon: negotiate returned no connectionToken")
	}
	return &out, nil
}

func (r *Realtime) connectOnce(ctx context.Context, on func(core.Event)) error {
	token, err := r.c.auth.token(ctx)
	if err != nil {
		return err
	}
	neg, err := r.negotiate(ctx, token)
	if err != nil {
		return err
	}

	q := url.Values{
		"access_token": {token},
		"ngsw-bypass":  {"true"},
		"id":           {neg.ConnectionToken},
	}
	wsURL := strings.Replace(r.c.cfg.baseURL, "https://", "wss://", 1) +
		"/signalR/hubs/signalr?" + q.Encode()

	hc, _ := r.c.cfg.http.(*http.Client)
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPClient: hc,
		HTTPHeader: http.Header{"User-Agent": {r.c.cfg.userAgent}},
	})
	if err != nil {
		return fmt.Errorf("recon: signalr dial: %w", err)
	}
	conn.SetReadLimit(readLimit)
	defer conn.CloseNow()
	r.conn = conn

	// Protocol handshake, then join. The web client does not wait for the
	// handshake ack before invoking, and neither do we.
	if err := writeFrame(ctx, conn, map[string]any{"protocol": "json", "version": 1}); err != nil {
		return err
	}
	if err := r.invoke(ctx, conn, "JoinConversations", map[string]any{
		"profileId": r.c.ProfileID(),
		// The hub argument DOES take the prefix, unlike the query parameter
		// above. Both forms appear on one connection.
		"accessToken": "Bearer " + token,
	}); err != nil {
		return err
	}

	pingCtx, stopPing := context.WithCancel(ctx)
	defer stopPing()
	go r.pingLoop(pingCtx, conn)

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("recon: signalr read: %w", err)
		}
		for _, frame := range splitFrames(data) {
			if err := r.handleFrame(ctx, conn, frame, on); err != nil {
				return err
			}
		}
	}
}

func (r *Realtime) pingLoop(ctx context.Context, conn *websocket.Conn) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := writeFrame(ctx, conn, map[string]any{"type": sigPing}); err != nil {
				return
			}
		}
	}
}

// handleFrame dispatches one decoded frame.
func (r *Realtime) handleFrame(ctx context.Context, conn *websocket.Conn, frame []byte, on func(core.Event)) error {
	frame = bytes.TrimSpace(frame)
	if len(frame) == 0 || bytes.Equal(frame, []byte("{}")) {
		return nil // handshake acknowledgement
	}

	var env struct {
		Type      int               `json:"type"`
		Target    string            `json:"target"`
		Arguments []json.RawMessage `json:"arguments"`
		Error     string            `json:"error"`
	}
	if err := json.Unmarshal(frame, &env); err != nil {
		return nil // unparseable frames are ignored rather than fatal
	}
	if env.Error != "" {
		return fmt.Errorf("recon: signalr handshake rejected: %s", env.Error)
	}

	switch env.Type {
	case sigPing:
		return writeFrame(ctx, conn, map[string]any{"type": sigPing})
	case sigClose:
		return fmt.Errorf("recon: signalr closed by server")
	case sigCompletion:
		return nil
	case sigInvocation:
		r.handleInvocation(ctx, conn, env.Target, env.Arguments, on)
	}
	return nil
}

func (r *Realtime) handleInvocation(ctx context.Context, conn *websocket.Conn, target string, args []json.RawMessage, on func(core.Event)) {
	arg := func() []byte {
		if len(args) == 0 {
			return []byte("{}")
		}
		return args[0]
	}

	switch target {
	case "ReceiveMessage":
		var ev struct {
			ConversationID string `json:"conversationId"`
			MessageID      string `json:"messageId"`
			// The wire field is profileId, NOT senderProfileId. The web client
			// renames it internally, which is the source of much confusion.
			ProfileID string `json:"profileId"`
			// Likewise messageText, not text.
			MessageText     string `json:"messageText"`
			SentDate        string `json:"sentDate"`
			AttachmentCount int    `json:"attachmentCount"`
		}
		if json.Unmarshal(arg(), &ev) != nil {
			return
		}
		sent, _ := parseTime(ev.SentDate)
		on(MessageEvent{
			ConversationID:  ev.ConversationID,
			MessageID:       ev.MessageID,
			SenderProfileID: ev.ProfileID,
			Text:            ev.MessageText,
			SentDate:        sent,
			AttachmentCount: ev.AttachmentCount,
		})

	case "NewConversationCreated":
		var ev struct {
			ConversationID string `json:"conversationId"`
		}
		if json.Unmarshal(arg(), &ev) != nil || ev.ConversationID == "" {
			return
		}
		// Join it, or every message in this thread is missed.
		token, err := r.c.auth.token(ctx)
		if err == nil {
			_ = r.invoke(ctx, conn, "JoinConversation", map[string]any{
				"profileId":      r.c.ProfileID(),
				"conversationId": ev.ConversationID,
				"accessToken":    "Bearer " + token,
			})
		}
		on(ConversationCreatedEvent{ConversationID: ev.ConversationID})

	case "ReceiveReadReceipt":
		var ev struct {
			ConversationID     string `json:"conversationId"`
			ReadByEveryoneDate string `json:"readByEveryoneDate"`
		}
		if json.Unmarshal(arg(), &ev) != nil {
			return
		}
		at, _ := parseTime(ev.ReadByEveryoneDate)
		on(ReadReceiptEvent{ConversationID: ev.ConversationID, ReadByEveryone: at})

	case "ReceiveTypingStatus":
		var ev struct {
			ConversationID string `json:"conversationId"`
			ProfileID      string `json:"profileId"`
			IsTyping       bool   `json:"isTyping"`
		}
		if json.Unmarshal(arg(), &ev) != nil {
			return
		}
		on(TypingEvent{ConversationID: ev.ConversationID, ProfileID: ev.ProfileID, IsTyping: ev.IsTyping})

	case "JoinedConversations":
		var ev envelope[Conversation]
		if json.Unmarshal(arg(), &ev) != nil {
			return
		}
		on(ConversationsSyncEvent{Conversations: ev.Data})

	case "ReceiveUnreadConversationCount",
		"ReceiveUnreadCruisesCount",
		"ReceiveUnreadVisitsCount",
		"ReceiveUnreadNotificationsCount":
		var ev struct {
			Count int `json:"count"`
		}
		if json.Unmarshal(arg(), &ev) != nil {
			return
		}
		kind := map[string]string{
			"ReceiveUnreadConversationCount":  "conversations",
			"ReceiveUnreadCruisesCount":       "cruises",
			"ReceiveUnreadVisitsCount":        "visits",
			"ReceiveUnreadNotificationsCount": "notifications",
		}[target]
		on(UnreadCountEvent{Kind: kind, Count: ev.Count})
	}
}

// SendTyping publishes a typing indicator. This is the one thing the web client
// sends over the hub rather than over REST.
func (r *Realtime) SendTyping(ctx context.Context, conversationID string, isTyping bool) error {
	conn := r.conn
	if conn == nil {
		return fmt.Errorf("recon: realtime not connected")
	}
	return r.invoke(ctx, conn, "SendTypingStatus", map[string]any{
		"profileId":      r.c.ProfileID(),
		"conversationId": conversationID,
		"isTyping":       isTyping,
	})
}

// invoke sends a hub invocation with a single object argument, which is the
// calling convention every method this package uses expects.
func (r *Realtime) invoke(ctx context.Context, conn *websocket.Conn, target string, arg any) error {
	raw, err := json.Marshal(arg)
	if err != nil {
		return fmt.Errorf("recon: marshal %s argument: %w", target, err)
	}
	return writeFrame(ctx, conn, map[string]any{
		"type":      sigInvocation,
		"target":    target,
		"arguments": []json.RawMessage{raw},
	})
}

// writeFrame marshals a frame and appends the record separator.
func writeFrame(ctx context.Context, conn *websocket.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("recon: marshal frame: %w", err)
	}
	b = append(b, recordSeparator)
	if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
		return fmt.Errorf("recon: write frame: %w", err)
	}
	return nil
}

// splitFrames splits a payload on the record separator.
//
// One WebSocket message may carry several frames, so this cannot assume one
// frame per read.
func splitFrames(data []byte) [][]byte {
	parts := bytes.Split(data, []byte{recordSeparator})
	out := make([][]byte, 0, len(parts))
	for _, p := range parts {
		if len(bytes.TrimSpace(p)) > 0 {
			out = append(out, p)
		}
	}
	return out
}
