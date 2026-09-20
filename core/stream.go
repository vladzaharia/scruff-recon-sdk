package core

import (
	"context"
	"math"
	"math/rand/v2"
	"time"
)

// Event is a realtime event. Each SDK defines its own concrete event types;
// callers type-switch on them.
//
// The marker method keeps unrelated types out of a handler by accident, at the
// cost of requiring SDK event types to be declared in their own package — which
// they are.
type Event interface {
	isCoreEvent()
}

// EventMarker is embedded by SDK event types to satisfy Event.
type EventMarker struct{}

func (EventMarker) isCoreEvent() {}

// EventSource is a realtime connection. Run blocks until ctx is cancelled,
// delivering events to on.
//
// Implementations reconnect internally; Run returning a non-nil error other
// than ctx.Err() means the source gave up.
//
// on is called from a single goroutine, so handlers need not be safe for
// concurrent use, but they should not block: a slow handler stalls the read
// loop and will eventually stall the connection.
type EventSource interface {
	Run(ctx context.Context, on func(Event)) error
}

// Backoff computes reconnect delays: exponential with full jitter, capped.
//
// Full jitter — a uniform draw from [0, delay] rather than delay itself —
// matters because both networks drop every client at once when an edge cycles,
// and a fleet of clients with identical backoff reconnects in a thundering herd.
type Backoff struct {
	Min    time.Duration // defaults to 1s
	Max    time.Duration // defaults to 2m
	Factor float64       // defaults to 2
}

// Delay returns the wait before attempt n, counting from zero.
func (b Backoff) Delay(attempt int) time.Duration {
	min, max, factor := b.Min, b.Max, b.Factor
	if min <= 0 {
		min = time.Second
	}
	if max <= 0 {
		max = 2 * time.Minute
	}
	if factor <= 1 {
		factor = 2
	}
	if attempt < 0 {
		attempt = 0
	}
	d := float64(min) * math.Pow(factor, float64(attempt))
	if d > float64(max) || math.IsInf(d, 0) {
		d = float64(max)
	}
	return time.Duration(rand.Int64N(int64(d)) + 1)
}

// Supervise runs connect in a loop until ctx is cancelled, backing off between
// attempts and resetting the backoff after a connection that lasted at least
// stable.
//
// Both networks' realtime layers need exactly this: neither supports resume, so
// a dropped connection means reconnect from scratch and reconcile over REST.
// The stable threshold prevents a connection that succeeds and immediately dies
// from resetting the backoff and hammering the server.
func Supervise(ctx context.Context, b Backoff, stable time.Duration, connect func(context.Context) error) error {
	if stable <= 0 {
		stable = 30 * time.Second
	}
	attempt := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		start := time.Now()
		_ = connect(ctx)
		if time.Since(start) >= stable {
			attempt = 0
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		t := time.NewTimer(b.Delay(attempt))
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
		attempt++
	}
}
