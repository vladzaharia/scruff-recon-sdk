# scruff-recon-sdk

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8.svg)](https://go.dev)

Reverse-engineered **API documentation** and **Go clients** for two dating networks that
publish no API: **Recon** and **SCRUFF**.

Two things live here, and you can take either on its own:

| | |
|---|---|
| 📘 **[Protocol references](docs/api/)** | ~5,000 lines documenting both APIs in their entirety — messaging, profile grids, private albums, moments, social graph, media, realtime. Language-agnostic; useful even if you never write Go. |
| 📦 **Go SDKs** | [`recon`](recon/) and [`scruff`](scruff/), standalone modules over a shared [`core`](core/). No Matrix, no mautrix, no heavy dependency tree. |

> [!IMPORTANT]
> **Personal interoperability only.** These clients access *your own* account through an
> unofficial client, which the networks' terms may prohibit. There is no affiliation with
> or endorsement by either network. Use realistic request rates, and don't use this to
> collect data about other people.

## The documentation

Neither network publishes an API, so this was derived from their own clients — Recon's
Angular web bundle and SCRUFF's Android app — plus traffic captured from the author's own
account. Every endpoint carries a provenance tag saying how it was established.

| Network | REST | Realtime | Enums |
|---|---|---|---|
| **Recon** | [`recon.md`](docs/api/recon.md) | [SignalR](docs/api/recon-realtime.md) | [`recon-enums.md`](docs/api/recon-enums.md) |
| **SCRUFF** | [`scruff.md`](docs/api/scruff.md) | [encrypted WebSocket](docs/api/scruff-realtime.md) | [`scruff-enums.md`](docs/api/scruff-enums.md) |

Start at [`docs/api/README.md`](docs/api/README.md), which has an at-a-glance comparison
and the eight traps that cost the most debugging time. There are also OpenAPI 3.1 specs
for the REST surface in [`docs/openapi/`](docs/openapi/) — REST only, since neither
SignalR nor an AES-framed socket is expressible in OpenAPI.

`[observed]` and `[client]` tags are both reliable; they record *how* a fact was
established, not how much to trust it. `[unverified]` means genuinely undetermined, and
every document ends with a "Known gaps" section listing those honestly.

## Install

```sh
go get github.com/vladzaharia/scruff-recon-sdk/recon
go get github.com/vladzaharia/scruff-recon-sdk/scruff
```

### Recon

Recon uses a self-issued RS256 JWT that lasts 15 minutes and rotates.

```go
sess, err := recon.Login(ctx, recon.Credentials{Email: email, Password: password})
if err != nil {
    return err
}

c := recon.New(sess)
// Tokens rotate — persist the session when they do, or you re-authenticate
// every 15 minutes.
c.OnSessionUpdate(func(ctx context.Context, s recon.Session) error { return save(s) })

convos, err := c.Conversations(ctx)
for _, conv := range convos {
    peer, _ := c.Profile(ctx, conv.Peer())
    msgs, _ := c.Messages(ctx, conv.ID, recon.MessagesOptions{Limit: 20})
    fmt.Printf("%s: %d messages\n", peer.Name, len(msgs))
}

// Realtime over SignalR.
go c.Realtime().Run(ctx, func(ev core.Event) {
    if m, ok := ev.(recon.MessageEvent); ok {
        fmt.Println("new message in", m.ConversationID)
    }
})
```

### SCRUFF

SCRUFF has no tokens and no expiry: a client-generated `device_id` is bound to the
account once and sent forever. Treat it exactly as you would a password.

```go
sess, err := scruff.Login(ctx, scruff.Credentials{Email: email, Password: password})
if err != nil {
    // Fresh-device registration is the fragile part of this protocol. If you
    // already have a bound device, resume from it instead:
    sess, err = scruff.ImportSession(ctx, scruff.Session{DeviceID: deviceID})
}

c := scruff.New(sess)
inbox, _ := c.Inbox(ctx, scruff.InboxOptions{})
for _, conv := range inbox.Results {
    // A thread has no id of its own — it is keyed by the peer's profile id.
    page, _ := c.Chat(ctx, conv.PeerID(), scruff.ChatOptions{})
    fmt.Printf("%s: %d messages\n", conv.Name, len(page.Results))
}
```

## Design

The two protocols have almost nothing in common — one is JWT plus SignalR, the other is a
device-bound session plus an AES-256-CBC WebSocket — so [`core`](core/) holds only what
genuinely generalises: transport, per-path rate limiting, typed errors, cursor
pagination, media handling, and a supervised reconnect loop with backoff and jitter.

`core` is **standard library only**; its `go.mod` has no `require` block. Each SDK adds
exactly one dependency, `coder/websocket`.

The interesting design decision is that `core.Authenticator.Authorize` takes a
`*core.Request` rather than an `*http.Request`. Recon authenticates with a header, but
SCRUFF injects identity into the query string on GET and into the form body on POST — and
an `*http.Request` has an already-sealed body. A neutral request description is what lets
one interface cover both.

Neither SDK logs anything. Credentials pass through these packages, and the caller owns
logging policy.

## Layout

```
core/             shared transport — standard library only
recon/            Recon client
scruff/           SCRUFF client
docs/api/         protocol references (start here)
docs/openapi/     OpenAPI 3.1 specs for the REST surface
docs/research/    raw reverse-engineering notes
```

## Development

```sh
# Separate modules, so iterate — a wildcard from the root reaches none of them.
for m in core recon scruff; do (cd $m && go build ./... && go vet ./... && go test ./...); done
```

Unit tests never touch the network. The integration tests that do are gated twice: behind
the `integration` build tag and behind credentials in a gitignored `credentials.env`.

```sh
for m in recon scruff; do (cd $m && go test -tags integration ./... -v); done
```

They are **read-only** unless `SMOKE_ALLOW_WRITES=1`, because writes on these networks
contact real people and are not undoable. SCRUFF's fresh-device login stage is expected
to fail and falls back to an imported session; that is by design, not a broken test.

Working on this with a coding agent? [`AGENTS.md`](AGENTS.md) and the per-module
`AGENTS.md` files carry the constraints and traps that aren't obvious from the code.

## License

[MIT](LICENSE). "Recon" and "SCRUFF" are trademarks of their respective owners, used here
only to identify the networks whose protocols are documented. Neither is affiliated with,
endorses, or has reviewed this project.
