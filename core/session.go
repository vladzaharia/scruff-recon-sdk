package core

import "context"

// SessionSaver is called when an SDK's session changes — typically after a
// token refresh — so the caller can persist it.
//
// This is how the SDKs stay free of storage concerns. The bridge implements it
// against its database; a standalone program might write a file; a test can
// ignore it.
//
// Implementations should be quick and must tolerate being called from any
// goroutine. Returning an error does not fail the in-flight request: the
// refreshed credentials are still valid in memory, they just were not durably
// stored, and the SDK reports that through the returned error of whatever call
// triggered the refresh.
type SessionSaver[S any] func(ctx context.Context, session S) error

// Store pairs an in-memory session with an optional saver.
//
// It carries no lock of its own; SDKs that mutate a session concurrently — Recon
// during token refresh — hold their own mutex around Get and Set.
type Store[S any] struct {
	session S
	save    SessionSaver[S]
}

// NewStore returns a Store seeded with session. save may be nil.
func NewStore[S any](session S, save SessionSaver[S]) *Store[S] {
	return &Store[S]{session: session, save: save}
}

// Get returns the current session.
func (s *Store[S]) Get() S { return s.session }

// Set replaces the session and invokes the saver, if any.
func (s *Store[S]) Set(ctx context.Context, session S) error {
	s.session = session
	if s.save == nil {
		return nil
	}
	return s.save(ctx, session)
}

// OnUpdate replaces the saver.
func (s *Store[S]) OnUpdate(save SessionSaver[S]) { s.save = save }
