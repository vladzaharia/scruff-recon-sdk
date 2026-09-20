# API references

Unofficial, reverse-engineered documentation for two networks that publish no API.

Everything here was derived from the vendors' own clients — the Recon Angular web bundle
and the SCRUFF Android app — plus traffic captured from the author's own account. Neither
API was probed beyond what those clients do during ordinary use.

| Network | REST | Realtime | Enums |
|---|---|---|---|
| **Recon** | [`recon.md`](./recon.md) | [`recon-realtime.md`](./recon-realtime.md) — SignalR | [`recon-enums.md`](./recon-enums.md) |
| **SCRUFF** | [`scruff.md`](./scruff.md) | [`scruff-realtime.md`](./scruff-realtime.md) — encrypted WebSocket | [`scruff-enums.md`](./scruff-enums.md) |

Machine-readable REST specs live in [`../openapi/`](../openapi/) — OpenAPI 3.1,
`redocly lint`-clean, covering 71 Recon paths and 55 SCRUFF paths. The raw
reverse-engineering notes are in [`../research/`](../research/).

**New to either API?** Start with [`getting-started.md`](./getting-started.md) — the
required call order for each network, which is the part that is not guessable, plus the
mistakes that cost the most time.

## What is covered

Both references, and the Go clients beside them, cover **everything a member can do**.
The line is drawn at **member, not moderator**: administrator and anti-fraud surface is
documented in prose but deliberately left unimplemented and unmodelled — Recon's
`payment` and `verification` services and its `dvrt` admin paths, SCRUFF's
`trials/admin_*`, `boost/grant`, `face_liveness`, `sms/send` and `captcha`.

⚠️ **Read paths are largely `[observed]`; most write paths are `[client]` and have never
been sent to the live API by this project.** Their request shapes come from the vendors'
own clients and are authoritative, but the responses are unconfirmed. If you exercise one
and the server disagrees with what is written here, that is new information — please
report it rather than assuming the document is merely sloppy.

## Provenance

Every endpoint is tagged. **`[observed]` and `[client]` are both reliable** — they differ
in how the fact was established, not in how much to trust it.

- **`[observed]`** — request and response seen on the wire.
- **`[client]`** — read from the vendor's own client implementation. Paths, parameters,
  body shapes, and enum values are authoritative; the limitation is coverage (a field the
  client never reads won't appear here), not accuracy.
- **`[unverified]`** — genuinely undetermined. Each instance says what was tried, and
  each document ends with a "Known gaps" section listing them.

No vendor source code — decompiled, minified, or otherwise — appears in this repository.
These documents are original prose describing observed behaviour.

## At a glance

|  | Recon | SCRUFF |
|---|---|---|
| Auth | Email + password → 15-minute RS256 JWT | Email + password → non-expiring device session |
| Credential | `Authorization: Bearer <jwt>` | `device_id` query/form parameter |
| Refresh | `refreshTokens` before expiry | None — the session does not expire |
| Realtime | SignalR over WebSocket, JSON frames | AES-256-CBC over WebSocket, plus an HTTP poll |
| Conversation key | `conversationId` (UUID) | the peer's `profile_id` (int) |
| History cursor | `before=<ISO createdDate>` | `max_version=<int>` |
| Errors | `{title, t101ErrorCode, errors}` | Status code only, plus a custom 420–452 vocabulary |
| Rate limiting | None observed | None server-side; the client self-throttles |
| Reactions / replies | Not supported by the network | Supported |

## The traps

Each of these cost real debugging time and is easy to get wrong:

1. **Recon returns `accessToken` already prefixed with `"Bearer "`.** Re-adding the
   prefix yields `Bearer Bearer`, which the *profile* service accepts and the *messaging*
   and *SignalR* services reject with an empty-body 401 — so your first test passes and
   the real work fails. ([recon.md §3.4](./recon.md#34-attaching-the-token-and-the-bearer-bearer-trap))
2. **Recon's `applicationId` must be the integer `3`.** A UUID makes the server reject
   the entire request model. ([recon.md §3.1](./recon.md#31-register-an-app-installation-observed))
3. **Recon's SignalR and REST disagree on field names.** Realtime sends
   `profileId`/`messageText`; REST sends `senderProfileId`/`text`, for the same two
   concepts. ([recon-realtime.md §4.1](./recon-realtime.md#41-receivemessage))
4. **Recon paginates with a `before=<ISO date>` cursor**, not `skip`/`offset`, and
   `totalRecords` on message history is the *page* size, not the total.
   ([recon.md §8.2](./recon.md#82-fetch-message-history-observed))
5. **SCRUFF's HMAC signature base is `lon~lat~client_version~device_type`** — using
   `device_id` (as several public write-ups state) produces a signature the server
   rejects. ([scruff.md §3.2](./scruff.md#32-request-signing-client))
6. **SCRUFF's `connect` carries both a `device_id` and a signature**, breaking the
   otherwise-universal "one or the other" rule. Omitting the signature there fails login.
   ([scruff.md §3.4](./scruff.md#34-fresh-device-login-observed))
7. **SCRUFF message `guid` case differs by direction** — outbound uppercase, inbound
   lowercase. Compare case-insensitively or you will duplicate your own sent messages.
   ([scruff.md §6.2](./scruff.md#62-fetch-messages-observed))
8. **Two SCRUFF profile enums are stored out of display order.** `relationship_status`
   `8`/`9` and `sex_preferences` `7` will be wrong if transcribed positionally.
   ([scruff-enums.md §2](./scruff-enums.md#2-profile-attributes))
