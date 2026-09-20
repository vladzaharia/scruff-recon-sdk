# Getting started

How to get from nothing to a working client on either network — in Go with these SDKs, or
in any language against the [OpenAPI specs](../openapi/) and the full references.

Both networks require a **specific call order** before anything else works, and both
punish getting it wrong in ways that look like something else. That order is the whole
point of this page.

- [Recon](#recon) — install → authenticate → read → realtime
- [SCRUFF](#scruff) — bootstrap → connect → register → read → realtime
- [Implementing in another language](#implementing-in-another-language)
- [The five mistakes that cost the most time](#the-five-mistakes-that-cost-the-most-time)

---

## Recon

### The flow

```
POST /account/appInstallations        → appInstallationId        (unauthenticated)
POST /account/accounts/authenticate   → accessToken + refreshToken + profileId
GET  /account/accounts/{id}/appSettings → the real server limits
... everything else, with Authorization: Bearer <accessToken>
POST .../sessions/{sid}/refreshTokens → new token pair, every ~15 minutes
```

**Step 1 is not optional.** `authenticate` requires the `appInstallationId` from step 0,
and `applicationId` on that call must be the **integer `3`** — not a UUID, despite the
name. This is the single most common first-attempt failure.

```go
sess, err := recon.Login(ctx, recon.Credentials{Email: email, Password: password})
if err != nil {
    return err
}
c := recon.New(sess)
```

`Login` does both calls. If you are implementing this yourself, see
[recon.md §3](./recon.md#3-authentication).

### Persist the session, or you will re-authenticate forever

Access tokens last **15 minutes** and rotate on refresh. The SDK refreshes automatically,
but it can only tell you about the new tokens through a callback:

```go
c.OnSessionUpdate(func(ctx context.Context, s recon.Session) error {
    return saveSomewhere(s) // called on every refresh
})
```

Skip this and you get a working client that silently logs in again every quarter hour.

### Read the limits rather than assuming them

```go
settings, _ := c.AccountAppSettings(ctx)
// maxMessageLength 1000, maxFileSizeBytes 52428800, maxFileAttachments 10
```

The real values differ from what is commonly assumed — notably messages cap at **1000**
characters, not 4096. Hardcoding the wrong ones truncates messages the server would have
accepted and rejects files it would have taken.

### First useful calls

```go
convos, _ := c.Conversations(ctx)          // not paginated; can be ~900 threads
for _, conv := range convos {
    peer, _ := c.Profile(ctx, conv.Peer()) // Peer() excludes you
    msgs, _ := c.Messages(ctx, conv.ID, recon.MessagesOptions{Limit: 20})
    _ = peer
    _ = msgs
}
```

Paging messages uses a **`before=<ISO timestamp>` cursor**, not `skip`/`take`. And
`totalRecords` on message history is the size of the page you just got, not the
conversation total — using it for paging arithmetic will not work.

### Realtime

```go
go c.Realtime().Run(ctx, func(ev core.Event) {
    switch e := ev.(type) {
    case recon.MessageEvent:
        fmt.Println("message in", e.ConversationID)
    case recon.ConversationsSyncEvent:
        // Free full sync, pushed right after joining. Use it.
    }
})
```

SignalR over WebSocket. The channel authenticates with the **plain access token** — there
is no separate SignalR token. See [recon-realtime.md](./recon-realtime.md).

---

## SCRUFF

### The flow

```
POST /app/account/register   (no device_id, signed)  → 401 + anonymous config  [OPTIONAL]
POST /app/account/connect    (email + password + device_id + signature) → 200, empty body
POST /app/account/register   (device_id, unsigned)   → 200 + full session
... everything else, with device_id as a plain parameter
```

Three things about this that are not guessable:

1. **You generate the `device_id` yourself.** The server does not issue it. `droid-` plus
   32 **UPPERCASE** hex characters. (`hardware_id` is `droid-` plus 16 **lowercase** — the
   cases really are opposite.)
2. **`connect` needs the signature even though it carries a `device_id`.** Every other
   endpoint takes one or the other. This exception is where most implementations stall.
3. **The first `register` returning 401 is not a failure.** It returns a usable anonymous
   configuration payload — socket host, CDNs, feature flags — and is optional for login.

```go
sess, err := scruff.Login(ctx, scruff.Credentials{Email: email, Password: password})
if err != nil {
    // Fresh-device registration is the fragile part of this protocol.
    // If you already have a bound device, resume from it instead:
    sess, err = scruff.ImportSession(ctx, scruff.Session{DeviceID: deviceID})
}
c := scruff.New(sess)
```

### There is no token and no expiry

The `device_id` **is** the credential, forever. Treat it exactly as you would a password:
leaking it is equivalent to leaking the account. There is nothing to refresh, so a stored
session keeps working indefinitely — which also means you cannot recover from a leak by
waiting.

### Set a location before anything spatial

Every request is signed over your coordinates, and the grid, moments and events endpoints
all need a real position. With none set, the client sends `0.0` — which is what the app
sends with no fix, and what makes `moments/trending` return 400.

```go
c := scruff.New(sess, scruff.WithLocationProvider(func() scruff.LatLng {
    return currentPosition() // consulted per request, not per client
}))
```

### First useful calls

```go
inbox, _ := c.Inbox(ctx, scruff.InboxOptions{})
for _, conv := range inbox.Results {
    // A thread has no id of its own. It is keyed by the peer's profile id,
    // and an inbox conversation's `id` field IS that peer id.
    page, _ := c.Chat(ctx, conv.PeerID(), scruff.ChatOptions{})
    _ = page
}
```

Order messages by **`version`** — a dense, gap-free per-conversation sequence starting at
1 — not by `id` and not by `created_at`. Page grids by the response's **`block_size`**,
not by the `limit` you asked for.

### Realtime

```go
rt, err := c.Realtime()
if err != nil {
    return err // session predates the AES key being stored
}
go rt.Run(ctx, func(ev core.Event) {
    switch e := ev.(type) {
    case scruff.MessageEvent:
        // Reconcile over REST by version. The socket has no backlog,
        // no acknowledgement and no resume, so treat it as a trigger.
    case scruff.AckEvent:
        // Confirmation of a write you made, correlated by RequestGUID.
        fmt.Println(e.Name, e.Kind, e.RequestGUID)
    case scruff.SessionInvalidEvent:
        // Class 418: re-register. Rotates socket credentials.
    }
})
```

Realtime is an **enhancement, not the delivery path**. REST polling by `version` is the
reliable baseline. See [scruff-realtime.md](./scruff-realtime.md) for all 87 class codes.

---

## Implementing in another language

The [OpenAPI specs](../openapi/) cover the REST surface and can be fed to a generator.
Four things they cannot express, which you must handle by hand:

| | |
|---|---|
| **SCRUFF request signing** | HMAC-SHA256. Key **and** message prefix are the same string: `"<lon>~<lat>~<client_version>~<device_type>"`; message is that plus `":" + timestamp`. Timestamp is decimal seconds with **six** fraction digits. [scruff.md §3.2](./scruff.md#32-request-signing-client) |
| **SCRUFF identity placement** | The six identity parameters move with the verb — query on GET/DELETE, form fields on POST/PUT, multipart parts on the chat send. A generated client that puts them in one place will half work. |
| **Realtime** | Neither SignalR nor an AES-256-CBC socket is expressible in OpenAPI. Both are documented in prose: [recon-realtime.md](./recon-realtime.md), [scruff-realtime.md](./scruff-realtime.md). |
| **SCRUFF error semantics** | There is no error body. A custom **420–452 status vocabulary** carries the meaning, and the same code means different things on different endpoints. Interpret per endpoint: [scruff.md §1.5](./scruff.md#15-errors-client). |

For SCRUFF realtime specifically: derive keys as `K = SHA-256(aes256_key)` and
`IV = MD5(aes256_iv)`, hashing the **32-character hex strings themselves**, not the bytes
they encode. Hex-decoding first silently yields the wrong key and a channel that connects
and then produces garbage.

---

## The five mistakes that cost the most time

1. **Recon: `Bearer Bearer`.** `accessToken` arrives **already prefixed** with `"Bearer "`.
   Adding your own prefix produces a token the *profile* service accepts and the
   *messaging* and *SignalR* services reject with an empty-body 401 — so it looks like it
   works, until the part you care about doesn't.

2. **Recon: `applicationId` must be integer `3`.** Not a UUID. Nothing else works until
   this is right.

3. **SCRUFF: signing `connect`.** It needs the signature *and* a `device_id`, unlike every
   other endpoint. Omitting the signature fails login with no useful error.

4. **SCRUFF: message GUID case.** Outbound guids are **UPPERCASE**, the server echoes them
   back **lowercase**. A case-sensitive comparison duplicates every message you send when
   it comes back. Compare case-insensitively.

5. **SCRUFF: `DELETE /app/block` with no parameters deletes every block on the account.**
   Always scope it with `target_id` or `target_ids[]`.

Each network's full trap list is in its reference: [recon.md](./recon.md),
[scruff.md](./scruff.md).
