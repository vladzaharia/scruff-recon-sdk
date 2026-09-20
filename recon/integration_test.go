//go:build integration

// Staged smoke test against the live Recon API using a real account.
//
//	go test -tags integration ./sdk/recon -v
//
// Gated twice: behind this build tag, and behind credentials being present. It
// never runs during `go test ./...`.
//
// Stages are separate subtests on purpose. When something breaks, the useful
// information is *which* call broke, not that the suite failed. Later stages
// reuse the session from Login, so a login failure skips the rest rather than
// producing a cascade of noise.
//
// Read-only unless SMOKE_ALLOW_WRITES=1. Nothing here prints a token.
package recon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vladzaharia/scruff-recon-sdk/core"
	"github.com/vladzaharia/scruff-recon-sdk/core/testenv"
)

func TestSmoke(t *testing.T) {
	creds := testenv.Require(t, "RECON")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var (
		client   *Client
		convos   []Conversation
		firstMsg *Message
		peerID   string
	)

	t.Run("01_app_settings_anonymous", func(t *testing.T) {
		st, err := AppSettingsAnonymous(ctx)
		if err != nil {
			t.Fatalf("appSettings: %v", err)
		}
		if st.Limits.MaxMessageLength == 0 {
			t.Error("maxMessageLength is 0 — the settings document looks wrong")
		}
		t.Logf("limits: message=%d chars, upload=%d bytes, attachments=%d",
			st.Limits.MaxMessageLength, st.Limits.MaxFileSizeBytes, st.Limits.MaxFileAttachments)
	})

	t.Run("02_login", func(t *testing.T) {
		sess, err := Login(ctx, Credentials{Email: creds.Email, Password: creds.Password})
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		if !sess.Valid() {
			t.Fatal("session is not valid after login")
		}
		client = New(sess)
		t.Logf("logged in: profile=%s, token expires %s",
			sess.ProfileID, sess.AccessTokenExpiry.Format(time.RFC3339))
	})

	requireClient := func(t *testing.T) {
		t.Helper()
		if client == nil {
			t.Skip("login did not succeed")
		}
	}

	t.Run("03_account_app_settings", func(t *testing.T) {
		requireClient(t)
		if _, err := client.AccountAppSettings(ctx); err != nil {
			t.Fatalf("account appSettings: %v", err)
		}
	})

	t.Run("04_conversations", func(t *testing.T) {
		requireClient(t)
		var err error
		convos, err = client.Conversations(ctx)
		if err != nil {
			t.Fatalf("conversations: %v", err)
		}
		t.Logf("%d conversations", len(convos))
		for _, c := range convos {
			if c.Peer() != "" {
				peerID = c.Peer()
				break
			}
		}
		if peerID == "" && len(convos) > 0 {
			t.Error("no conversation exposed a peer — participants should exclude self")
		}
	})

	t.Run("05_message_history", func(t *testing.T) {
		requireClient(t)
		if len(convos) == 0 {
			t.Skip("no conversations to read")
		}
		msgs, err := client.Messages(ctx, convos[0].ID, MessagesOptions{Limit: 5})
		if err != nil {
			t.Fatalf("messages: %v", err)
		}
		t.Logf("%d messages in the first page", len(msgs))
		if len(msgs) > 0 {
			firstMsg = &msgs[0]
			// Results are newest-first; assert that rather than assuming it.
			if len(msgs) > 1 && msgs[0].Created().Before(msgs[1].Created()) {
				t.Error("history is not newest-first")
			}
		}
	})

	t.Run("06_backfill_cursor", func(t *testing.T) {
		requireClient(t)
		if firstMsg == nil {
			t.Skip("no messages to page from")
		}
		// Exercise the before= cursor specifically — the most commonly
		// mis-implemented part of this API.
		older, err := client.Messages(ctx, convos[0].ID, MessagesOptions{
			Limit: 5, Before: firstMsg.CreatedDate,
		})
		if err != nil {
			t.Fatalf("backfill: %v", err)
		}
		for _, m := range older {
			if !m.Created().Before(firstMsg.Created()) {
				t.Errorf("before= returned a message at or after the cursor: %s", m.ID)
			}
		}
		t.Logf("%d older messages", len(older))
	})

	t.Run("07_peer_profile", func(t *testing.T) {
		requireClient(t)
		if peerID == "" {
			t.Skip("no peer to resolve")
		}
		p, err := client.Profile(ctx, peerID)
		if err != nil {
			t.Fatalf("profile: %v", err)
		}
		if p.ID == "" {
			t.Error("profile came back without an id")
		}
		t.Logf("peer profile version=%d, avatar=%t", p.Version, p.AvatarURL() != "")
	})

	t.Run("08_media_download", func(t *testing.T) {
		requireClient(t)
		if peerID == "" {
			t.Skip("no peer to resolve")
		}
		p, err := client.Profile(ctx, peerID)
		if err != nil || p.AvatarURL() == "" {
			t.Skip("peer has no avatar")
		}
		blob, err := client.Download(ctx, core.MediaRef{URL: p.AvatarURL()})
		if err != nil {
			t.Fatalf("download: %v", err)
		}
		if len(blob.Data) == 0 {
			t.Error("downloaded an empty avatar")
		}
		t.Logf("avatar: %d bytes, %s", len(blob.Data), blob.MimeType)
	})

	t.Run("09_error_envelope", func(t *testing.T) {
		requireClient(t)
		// Deliberately malformed id. This is the one call that is *expected* to
		// fail; it confirms the error envelope shape without touching anyone.
		_, err := client.Conversation(ctx, "not-a-uuid")
		if err == nil {
			t.Skip("server accepted a malformed id; nothing to assert")
		}
		apiErr, ok := core.AsAPIError(err)
		if !ok {
			t.Fatalf("err = %T, want *core.APIError", err)
		}
		t.Logf("error envelope: status=%d body=%s", apiErr.StatusCode, apiErr.Body)
	})

	t.Run("10_signalr", func(t *testing.T) {
		requireClient(t)
		// Connect, wait for the join burst, then disconnect. The server pushes
		// JoinedConversations and an unread count immediately after joining, so
		// a healthy connection produces at least one event quickly.
		runCtx, stop := context.WithTimeout(ctx, 30*time.Second)
		defer stop()

		events := make(chan core.Event, 16)
		done := make(chan error, 1)
		go func() {
			done <- client.Realtime().Run(runCtx, func(ev core.Event) {
				select {
				case events <- ev:
				default:
				}
			})
		}()

		select {
		case ev := <-events:
			t.Logf("first realtime event: %T", ev)
			stop()
		case <-runCtx.Done():
			t.Error("no realtime event within 30s — the join burst should be immediate")
		}

		if err := <-done; err != nil && !errors.Is(err, context.Canceled) &&
			!errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("realtime: %v", err)
		}
	})

	t.Run("11_token_refresh", func(t *testing.T) {
		requireClient(t)
		// The refresh endpoint is derived from the web client and has never
		// been seen on the wire, so this stage is the only real check of it.
		// Force it by expiring the in-memory token.
		s := client.Session()
		if s.RefreshToken == "" {
			t.Skip("no refresh token in session")
		}
		before := s.AccessToken
		s.AccessTokenExpiry = time.Now().Add(-time.Minute)
		forced := New(s)

		if _, err := forced.AccountAppSettings(ctx); err != nil {
			if IsAuthExpired(err) {
				t.Fatalf("refresh rejected — the inferred endpoint may be wrong: %v", err)
			}
			t.Fatalf("call after forced refresh: %v", err)
		}
		if forced.Session().AccessToken == before {
			t.Error("token did not change; refresh may not have happened")
		} else {
			t.Log("refresh succeeded — this path was previously unverified")
		}
	})

	t.Run("12_send", func(t *testing.T) {
		requireClient(t)
		testenv.RequireWrites(t)
		peer := testenv.Peer(t, "RECON")

		conv, err := client.FindConversation(ctx, peer)
		if err != nil {
			t.Fatalf("find conversation: %v", err)
		}
		if conv == nil {
			if conv, err = client.CreateConversation(ctx, peer); err != nil {
				t.Fatalf("create conversation: %v", err)
			}
		}
		msg, err := client.SendText(ctx, conv.ID, "smoke test "+time.Now().Format(time.RFC3339))
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		t.Logf("sent message %s", msg.ID)

		if err := client.MarkRead(ctx, conv.ID, time.Now()); err != nil {
			t.Errorf("mark read: %v", err)
		}
	})
}
