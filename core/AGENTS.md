# AGENTS.md — sdk/core

Shared transport plumbing for the network SDKs. Read this before changing anything here,
because the constraints are not obvious from the code.

## Hard constraints

- **Standard library only.** This module's `go.mod` has no `require` block and must keep
  it that way. Anything needing a dependency belongs in a network SDK. If you find
  yourself wanting `golang.org/x/...` here, put it in the caller instead.
- **Never log.** No `fmt.Print`, no `log`, no injected logger. These SDKs run inside a
  bridge that owns logging, and credentials pass through this package.
- **Never retry or refresh here.** `Transport` sends exactly one request. Token refresh
  is the `Authenticator`'s job, because it is the only thing that knows what a token is.

## The one design decision worth understanding

`Authenticator.Authorize` takes a `*core.Request`, **not** an `*http.Request`.

This looks like needless indirection until you see why: Recon authenticates with an
`Authorization` header, but SCRUFF injects identity parameters into the **query string
for GET and the form body for POST**. By the time you hold an `*http.Request` the body is
already sealed, so an `*http.Request`-based interface could serve one network and not the
other. `Request.SetParam` routes a key/value to the right place based on method and body
kind; that is the whole trick.

If you are tempted to "simplify" this to `*http.Request`, don't — it breaks SCRUFF.

## Things that look wrong but are deliberate

- `Backoff.Delay` returns a *random* value in `(0, ceiling]`, not the ceiling. Full
  jitter is intentional: both networks drop every client at once when a CDN edge cycles,
  and identical backoff produces a thundering herd. Tests assert the ceiling, not the
  value.
- `Limiter` exists even though **neither network was ever observed rate-limiting** — no
  429, no `RateLimit-*` header, no `Retry-After` in any capture. It is here because
  SCRUFF's own client self-throttles per endpoint, and behaving like the real client is
  the point.
- `Store` has no mutex. Callers that mutate concurrently (Recon, during token refresh)
  hold their own lock. Adding one here would give a false sense of atomicity across
  get-modify-set.
- `Paginate` is generic over the *cursor type* rather than taking a page number, because
  neither network uses offsets: Recon pages by the ISO timestamp of the oldest message
  held, SCRUFF by the lowest message version.

## Testing

`go test ./...` — pure, no network, fast. `Limiter` takes an injectable clock (the
unexported `now` field) so time-dependent tests are deterministic; use it rather than
sleeping.

## Adding to this package

Only add something here when **both** SDKs need it. A helper used by one network belongs
in that SDK, even if it feels generic. The test for "does this belong in core" is whether
removing it would force duplication, not whether it could theoretically be shared.
