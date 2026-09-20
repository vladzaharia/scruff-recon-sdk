// Package recon is a client for the Recon (recon.com) v3 API.
//
// Recon publishes no API. This package was written from the public Angular web
// client and from traffic produced by the author's own account. See
// docs/api/recon.md in this repository for the protocol reference.
//
// # Usage
//
//	sess, err := recon.Login(ctx, recon.Credentials{Email: "…", Password: "…"})
//	if err != nil { return err }
//
//	c := recon.New(sess)
//	c.OnSessionUpdate(func(ctx context.Context, s recon.Session) error {
//	    return save(s) // access tokens last 15 minutes and rotate
//	})
//
//	convos, err := c.Conversations(ctx)
//
// # Three things that will catch you out
//
// The API returns accessToken already prefixed with "Bearer ". Re-adding the
// prefix yields "Bearer Bearer", which the profile service accepts and the
// messaging and SignalR services reject with an empty-body 401 — so a naive
// implementation appears to work until it doesn't. This package strips the
// prefix on receipt and adds exactly one; see stripBearer.
//
// Message history pages with a before=<ISO createdDate> cursor, not an offset,
// and the totalRecords on a history response is the size of the returned page
// rather than the conversation total.
//
// SignalR and REST disagree about field names for the same concepts: realtime
// sends profileId and messageText, REST sends senderProfileId and text. The
// event types in this package normalise that.
package recon
