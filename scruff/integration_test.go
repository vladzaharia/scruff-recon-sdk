//go:build integration

// Staged smoke test against the live SCRUFF API using a real account.
//
//	go test -tags integration ./sdk/scruff -v
//
// Gated twice: behind this build tag, and behind credentials being present.
//
// EXPECT FRESH-DEVICE LOGIN TO FAIL. Registering a brand-new device id is the
// least reliable part of this protocol and may be refused outright. That is why
// stage 02 reports the failure and the suite then falls back to an imported
// device session (SCRUFF_DEVICE_ID) rather than aborting: the rest of the API
// is perfectly testable with a device that is already bound.
//
// To use an existing device session instead of logging in:
//
//	SCRUFF_DEVICE_ID=droid-…      (required)
//	SCRUFF_HARDWARE_ID=…          (optional)
//	SCRUFF_AES_KEY=… SCRUFF_AES_IV=…  (optional; needed for realtime)
//
// Read-only unless SMOKE_ALLOW_WRITES=1. Nothing here prints a credential.
package scruff

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vladzaharia/scruff-recon-sdk/core"
	"github.com/vladzaharia/scruff-recon-sdk/core/testenv"
)

func TestSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	var (
		client *Client
		inbox  *InboxPage
		peerID string
	)

	t.Run("01_bootstrap_register", func(t *testing.T) {
		// The anonymous call answers 401 with a usable configuration payload.
		// It is optional for login but confirms reachability and the signing
		// scheme without any credentials at all.
		reg, err := BootstrapRegister(ctx)
		if err != nil {
			t.Fatalf("bootstrap register: %v", err)
		}
		if reg.Socket.Host == "" {
			t.Error("no socket host in the anonymous payload")
		}
		t.Logf("anonymous config: socket=%s cdns=%t",
			reg.Socket.Host, reg.ProfileCDN != "" || reg.CDN != "")
		if reg.SuggestedEmail != "" {
			// Present but deliberately not logged — it is a real address.
			t.Log("anonymous payload included a suggested_email (not logged)")
		}
	})

	t.Run("02_fresh_device_login", func(t *testing.T) {
		creds := testenv.Require(t, "SCRUFF")
		sess, err := Login(ctx, Credentials{Email: creds.Email, Password: creds.Password})
		if err != nil {
			// Expected to be flaky or blocked. Report clearly and let the
			// import path carry the rest of the suite.
			t.Errorf("fresh-device login failed (this is the known-fragile path): %v", err)
			return
		}
		client = New(sess)
		t.Logf("logged in: profile=%s", sess.ProfileID)
	})

	t.Run("03_import_device_session", func(t *testing.T) {
		if client != nil {
			t.Skip("fresh-device login already succeeded")
		}
		deviceID := testenv.Get("SCRUFF_DEVICE_ID")
		if deviceID == "" {
			t.Skip("set SCRUFF_DEVICE_ID to test with an existing device session")
		}
		sess, err := ImportSession(ctx, Session{
			DeviceID:   deviceID,
			HardwareID: testenv.Get("SCRUFF_HARDWARE_ID"),
			AES256Key:  testenv.Get("SCRUFF_AES_KEY"),
			AES256IV:   testenv.Get("SCRUFF_AES_IV"),
		})
		if err != nil {
			t.Fatalf("import session: %v", err)
		}
		client = New(sess)
		t.Logf("imported session: profile=%s", sess.ProfileID)
	})

	requireClient := func(t *testing.T) {
		t.Helper()
		if client == nil {
			t.Skip("no session: fresh-device login failed and no SCRUFF_DEVICE_ID was supplied")
		}
	}

	t.Run("04_register", func(t *testing.T) {
		requireClient(t)
		reg, err := client.Register(ctx)
		if err != nil {
			t.Fatalf("register: %v", err)
		}
		t.Logf("tier=%s features=%d socket=%s",
			reg.AccountTier.Tier, len(reg.Features), reg.Socket.Host)
		if reg.Socket.Pwd == "" {
			t.Error("register returned no socket password; realtime will not connect")
		}
		if reg.DeviceSettings != "" {
			// A JSON-encoded string, not an object — worth asserting because
			// it is an easy modelling mistake.
			if reg.DeviceSettings[0] != '{' {
				t.Errorf("device_settings should be a JSON string, got %q", reg.DeviceSettings[:1])
			}
		}
	})

	t.Run("05_inbox", func(t *testing.T) {
		requireClient(t)
		var err error
		inbox, err = client.Inbox(ctx, InboxOptions{})
		if err != nil {
			t.Fatalf("inbox: %v", err)
		}
		t.Logf("%d conversations (count=%d, block_size=%d)",
			len(inbox.Results), inbox.Count, inbox.BlockSize)
		if len(inbox.Results) > 0 {
			peerID = inbox.Results[0].PeerID()
			if peerID == "" {
				t.Error("conversation has no id — it should be the peer's profile id")
			}
		}
	})

	t.Run("06_chat_history", func(t *testing.T) {
		requireClient(t)
		if peerID == "" {
			t.Skip("no conversation to read")
		}
		page, err := client.Chat(ctx, peerID, ChatOptions{})
		if err != nil {
			t.Fatalf("chat: %v", err)
		}
		t.Logf("%d messages, min_version=%d max_read_version=%d",
			len(page.Results), page.MinVersion, page.MaxReadVersion)

		// Results must ascend by version — the sync algorithm depends on it.
		for i := 1; i < len(page.Results); i++ {
			if page.Results[i].Version < page.Results[i-1].Version {
				t.Errorf("results are not ascending by version at index %d", i)
				break
			}
		}
	})

	t.Run("07_backfill_by_version", func(t *testing.T) {
		requireClient(t)
		if peerID == "" {
			t.Skip("no conversation to page")
		}
		page, err := client.Chat(ctx, peerID, ChatOptions{})
		if err != nil || len(page.Results) == 0 {
			t.Skip("no messages to page from")
		}
		lowest := page.Results[0].Version
		if lowest <= 1 {
			t.Skip("already at the start of history")
		}
		older, err := client.Chat(ctx, peerID, ChatOptions{MaxVersion: lowest})
		if err != nil {
			t.Fatalf("backfill: %v", err)
		}
		for _, m := range older.Results {
			if m.Version >= lowest {
				t.Errorf("max_version returned a message at or above the cursor: v%d", m.Version)
			}
		}
		t.Logf("%d older messages", len(older.Results))
	})

	t.Run("08_profile", func(t *testing.T) {
		requireClient(t)
		if peerID == "" {
			t.Skip("no peer to resolve")
		}
		p, err := client.Profile(ctx, peerID)
		if err != nil {
			t.Fatalf("profile: %v", err)
		}
		t.Logf("peer: photos=%d avatar=%t", len(p.ProfilePhotos), client.AvatarURL(*p) != "")
	})

	t.Run("09_media_download", func(t *testing.T) {
		requireClient(t)
		if peerID == "" {
			t.Skip("no peer")
		}
		p, err := client.Profile(ctx, peerID)
		if err != nil {
			t.Skip("could not resolve the peer")
		}
		url := client.AvatarURL(*p)
		if url == "" {
			t.Skip("peer has no avatar")
		}
		// Profile media is unsigned and public, unlike chat and album media.
		blob, err := client.Download(ctx, core.MediaRef{URL: url})
		if err != nil {
			t.Fatalf("download: %v", err)
		}
		t.Logf("avatar: %d bytes, %s", len(blob.Data), blob.MimeType)
	})

	t.Run("10_nearby_grid", func(t *testing.T) {
		requireClient(t)
		// Every grid is this same call against a different path, so exercising
		// one validates the shape for all 23.
		page, err := client.Nearby(ctx, GridOptions{Sort: SortDistance, Limit: 10})
		if err != nil {
			t.Fatalf("nearby: %v", err)
		}
		t.Logf("%d profiles (max=%d max_free=%d block_size=%d cache_id=%t)",
			len(page.Results), page.Max, page.MaxFree, page.BlockSize, page.CacheID != "")
	})

	t.Run("11_albums", func(t *testing.T) {
		requireClient(t)
		albums, err := client.Albums(ctx, "")
		if err != nil {
			t.Fatalf("albums: %v", err)
		}
		t.Logf("%d albums", len(albums))
		if len(albums) > 0 {
			contents, err := client.AlbumImages(ctx, albums[0].ID.String())
			if err != nil {
				t.Fatalf("album images: %v", err)
			}
			t.Logf("album %s: %d images", albums[0].Name, len(contents.Results))
		}
	})

	t.Run("12_error_shape", func(t *testing.T) {
		requireClient(t)
		// SCRUFF carries errors in the status code with no body. Confirm that
		// rather than assuming it.
		_, err := client.Profile(ctx, "0")
		if err == nil {
			t.Skip("server accepted profile id 0")
		}
		if apiErr, ok := core.AsAPIError(err); ok {
			t.Logf("error: status=%d body=%q (empty body is expected)",
				apiErr.StatusCode, apiErr.Body)
		} else {
			t.Logf("non-API error: %v", err)
		}
	})

	t.Run("13_realtime", func(t *testing.T) {
		requireClient(t)
		rt, err := client.Realtime()
		if err != nil {
			t.Skipf("realtime unavailable: %v", err)
		}
		// Unlike Recon, SCRUFF pushes nothing on connect — a quiet account
		// produces no frames. Success here is "connected and stayed up",
		// so treat a clean timeout as a pass.
		runCtx, stop := context.WithTimeout(ctx, 20*time.Second)
		defer stop()

		events := make(chan core.Event, 8)
		done := make(chan error, 1)
		go func() {
			done <- rt.Run(runCtx, func(ev core.Event) {
				select {
				case events <- ev:
				default:
				}
			})
		}()

		select {
		case ev := <-events:
			t.Logf("realtime event: %T", ev)
		case <-runCtx.Done():
			t.Log("no frames in 20s — normal for a quiet account")
		}
		if err := <-done; err != nil && !errors.Is(err, context.Canceled) &&
			!errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("realtime: %v", err)
		}
	})

	t.Run("14_send", func(t *testing.T) {
		requireClient(t)
		testenv.RequireWrites(t)
		peer := testenv.Peer(t, "SCRUFF")

		guid, err := client.SendText(ctx, peer, "smoke test "+time.Now().Format(time.RFC3339))
		if err != nil {
			t.Fatalf("send: %v", err)
		}
		t.Logf("sent guid=%s", guid)

		// The echo comes back lowercase; confirm the case-insensitive match
		// that prevents duplicating your own messages.
		page, err := client.Chat(ctx, peer, ChatOptions{})
		if err != nil {
			t.Fatalf("chat after send: %v", err)
		}
		found := false
		for _, m := range page.Results {
			if SameGUID(m.GUID, guid) {
				found = true
				if m.GUID == guid {
					t.Log("echo preserved guid case")
				} else {
					t.Logf("echo changed guid case as expected: sent %s, got %s", guid, m.GUID)
				}
				break
			}
		}
		if !found {
			t.Error("sent message did not appear in history")
		}

		if err := client.SendTyping(ctx, peer); err != nil {
			t.Errorf("typing: %v", err)
		}
		if err := client.MarkInboxViewed(ctx, time.Now()); err != nil {
			t.Errorf("mark inbox viewed: %v", err)
		}
	})
}
