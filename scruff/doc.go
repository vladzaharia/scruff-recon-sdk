// Package scruff is a client for the SCRUFF app API.
//
// SCRUFF publishes no API. This package was written from the Android app and
// from traffic produced by the author's own account. See docs/api/scruff.md in
// this repository for the protocol reference.
//
// # Usage
//
//	sess, err := scruff.Login(ctx, scruff.Credentials{Email: "…", Password: "…"})
//	if err != nil { return err }
//
//	c := scruff.New(sess)
//	convos, err := c.Inbox(ctx, scruff.InboxOptions{})
//
// # The credential model
//
// There are no tokens and no expiry. The client generates a device id, binds it
// to the account once with email and password, and then sends it as a plain
// parameter on every request forever. Treat it exactly as you would a
// long-lived bearer token: leaking it is equivalent to leaking the password.
//
// # Three things that will catch you out
//
// The HMAC signature base is "<lon>~<lat>~<client_version>~<device_type>". A
// widely repeated write-up says device_id in the third position; that is wrong
// and produces a signature the server rejects.
//
// Signing is normally applied only when no device id exists yet — except on
// account/connect, which carries a device id AND forces the signature. Omitting
// it there fails login.
//
// Message guids differ in case by direction: outbound uppercase, inbound
// lowercase. Compare case-insensitively or you will duplicate your own sent
// messages when they echo back.
package scruff
