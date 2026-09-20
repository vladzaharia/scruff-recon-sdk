// Package core holds the transport plumbing shared by the network SDKs in this
// repository, plus the small set of interfaces they all satisfy.
//
// It deliberately depends on nothing but the standard library. Anything that
// needs a third-party dependency — a WebSocket, a Matrix bridge — belongs in a
// network SDK or in the bridge, not here.
//
// # The shape contract
//
// Every SDK built on this package follows the same shape, so that knowing one
// makes the next one predictable:
//
//	// Authenticate. Returns a Session, which is plain data and JSON-serialisable.
//	sess, err := network.Login(ctx, network.Credentials{...})
//
//	// Or resume from a Session you stored earlier.
//	c := network.New(sess)
//
//	// Persist token refreshes without the SDK knowing what your storage is.
//	c.OnSessionUpdate(func(s network.Session) error { return save(s) })
//
//	// REST calls are methods returning native, lossless types.
//	convos, err := c.Conversations(ctx)
//
//	// Realtime is an EventSource; Run blocks until ctx is cancelled.
//	go c.Realtime().Run(ctx, func(ev core.Event) { ... })
//
// Rules the SDKs hold to:
//
//   - Domain types are native to the network. There is no shared Message or
//     Conversation type, because Recon keys conversations by UUID and SCRUFF by
//     the peer's integer id, and flattening that would lose information a
//     re-implementer needs.
//   - Every failed call returns an *APIError, so callers can branch on
//     IsUnauthorized, IsRateLimited, and friends without knowing the network.
//   - Nothing is logged. The SDKs never print, and never touch credentials
//     beyond sending them.
//
// Conformance is enforced at compile time rather than by convention: each SDK
// carries assertions such as
//
//	var _ core.Authenticator = (*Client)(nil)
//	var _ core.EventSource   = (*Realtime)(nil)
//
// so drift breaks the build rather than surprising a caller.
package core
