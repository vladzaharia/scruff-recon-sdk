# SCRUFF realtime — encrypted WebSocket reference

SCRUFF's push channel is a **server→client-only WebSocket carrying AES-256-CBC
ciphertext**, keyed by material the *client* generates and registers at login. There is
no subprotocol and no in-band handshake — authentication happens entirely in the upgrade
headers.

Alongside it runs an HTTP **catch-up poll** that returns byte-identical frames, so a
correct implementation handles one message format from two transports.

Companion to [`scruff.md`](./scruff.md) and [`scruff-enums.md`](./scruff-enums.md). Tags
follow the same [provenance convention](./scruff.md#provenance-tags).

**Treat the socket as a trigger, not a source of truth.** It has no backlog, no
acknowledgement, and no resume token. Every event should cause a REST reconciliation by
`version`; REST is authoritative.

---

## 1. Connecting **[observed]**

Endpoint and credentials come from the `socket` object in the
[`register` response](./scruff.md#41-the-register-response-observed):

```jsonc
"socket": { "host": "d1jj3suym8i42u.cloudfront.net", "port": 443, "pwd": "<32 hex>" }
```

```http
GET wss://<socket.host>/
Upgrade: websocket
Sec-WebSocket-Version: 13
Sec-WebSocket-Extensions: permessage-deflate
X-Device-Id:  <device_id>
X-Token-Auth: <socket.pwd>
X-Request-Id: <32 hex, per connection>
User-Agent:   okhttp/5.3.2
```

→ `101 Switching Protocols`.

- Path is `/`. Append `:<port>` only when the port is neither `0` nor `443`.
- The host is a CloudFront edge (domain-fronted), so it **rotates** — re-read it from
  `register` rather than caching it.
- **No subprotocol** is negotiated, and there is no application-level auth frame after
  the upgrade. The three `X-*` headers are the whole authentication story.
- `X-Token-Auth` is `socket.pwd` from `register` — **not** anything returned by
  `connect`, which returns an empty body.

ℹ️ Calling `register` again rotates `socket.pwd`. The app does this on every foreground,
which is also how it recovers a stale socket credential.

---

## 2. Decryption **[observed]**

Frames arrive as **WebSocket text frames containing base64**. Decode, decrypt, strip
padding, parse JSON.

```
K   = SHA-256( aes256_key  as ASCII )     → 32-byte AES-256 key
IV  = MD5    ( aes256_iv   as ASCII )     → 16-byte IV
plaintext = PKCS7_strip( AES-256-CBC-Decrypt( K, IV, base64_decode(frame) ) )
```

⚠️ **The key is the SHA-256 of the 32-character hex *string*, not of the bytes it
encodes.** Hex-decoding `aes256_key` before hashing produces the wrong key. The same
applies to `aes256_iv` and MD5.

⚠️ **The IV is fixed for the whole session**, derived once from `aes256_iv`. It is not
prepended per frame and not rotated. (Reusing a fixed IV across messages under CBC is
weak — identical plaintext prefixes produce identical ciphertext prefixes — but it is
what the protocol does.)

Both `aes256_key` and `aes256_iv` are **client-generated**, 32 hex characters each, and
registered at login in the device descriptor. You choose them; the server keys the stream
to `(account, device)` identified by `X-Device-Id` + `X-Token-Auth`.

Practical notes: reject frames whose decoded length is not a multiple of 16 rather than
letting the cipher throw, and fall back to treating the payload as raw bytes if base64
decoding fails.

---

## 3. Frame envelope **[observed]**

Every decrypted frame shares one envelope:

```jsonc
{
  "class":        201,          // event-type discriminator — see §4
  "id":           "<32 hex>",   // per-event server id
  "request_guid": "<32 hex>",   // correlates to YOUR HTTP request; null for unsolicited events
  "timestamp":    1789866828,   // unix epoch seconds
  "backlog":      0,            // 0 = live
  "results":      { }           // payload; shape depends on class
}
```

`class` and `id` are always present; `request_guid` and `results` are nullable.

ℹ️ Some deliveries wrap the payload with the class repeated as
`message_class_name` / `message_class_value` alongside `results`. Dispatch on the numeric
`class`, which is always present.

### `request_guid` correlation

Every mutating REST request carries a `request_guid`. When the server finishes the work it
emits a frame carrying that same guid. This is how the app confirms writes: send
`POST /app/chat` with `guid`, then wait for class `200` with a matching `request_guid`.

Frames with `request_guid: null` are unsolicited — someone else acted on you.

---

## 4. Class codes **[client]**

87 codes. Each also carries an *event type* that tells you which HTTP verb the class
corresponds to, so a repository layer can invalidate the right cache: `CallbackPost`,
`CallbackPut`, `CallbackDelete`, or `Standard` (unsolicited, no originating request).

### Social

| Class | Name | Type |
|---|---|---|
| `0` | Unknown | Standard |
| `1` | Woof | Standard |
| `2` | Album | Standard |
| `3` | Match | Standard |
| `5` | View | Standard |

### Albums — 100–112

| Class | Name | Type |
|---|---|---|
| `100` | AlbumCreate | POST |
| `101` | AlbumRename | PUT |
| `102` | AlbumDelete | DELETE |
| `103` | AlbumPermissionGrant | POST |
| `104` | AlbumPermissionRevoke | DELETE |
| `105` | AlbumImageCreate | POST |
| `106` | AlbumImageDelete | DELETE |
| `107` | AlbumImageMove | PUT |
| `108` | AlbumImageCaption | PUT |
| `109` | AlbumImageChatArchive | POST |
| `110` | AlbumImageSortOrder | PUT |
| `111` | AlbumImageCrossUserArchive | POST |
| `112` | AlbumVideoDownloadReady | PUT |

⚠️ `103 AlbumPermissionGrant` maps to `POST /app/albums/permissions`, which **this build
of the app never calls** — album sharing goes through a chat message of `message_type=12`
instead. The class exists server-side regardless.

`112` is how you receive a signed video URL after requesting one via
`PUT /app/albums/images/download`.

### Chat — 200–209

| Class | Name | Type | Notes |
|---|---|---|---|
| `200` | ChatMessageDelivered | POST | **Echo of your own send.** `request_guid` matches. Payload's `recipient_id` is the peer. |
| `201` | ChatMessageReceived | Standard | **Inbound message.** `sender_id` is the peer. |
| `202` | ChatMessageUnsend | PUT | Peer unsent a message → redact it |
| `203` | ChatThreadDelete | DELETE | |
| `204` | ChatInboxDelete | DELETE | |
| `206` | ChatThreadMigrated | Standard | |
| `207` | ChatMessageUpdated | PUT | |
| `208` | **ChatRecipientTyping** | Standard | `{profile_id, target_id}` |
| `209` | ChatMessageViewed | Standard | `{target_id, max_read_version}` — read receipt |

⚠️ **`208` is typing, not a read receipt.** Some earlier public notes describe it as
"read/opened conversation". Read receipts are `209`, which carries `max_read_version`.

`205` is absent from this build.

The `results` payload for `200`/`201` is a full chat message object — the same schema as
`GET /app/chat` ([scruff.md §6.2](./scruff.md#62-fetch-messages-observed)) — **plus** an
embedded `sender` profile object and a `recipient` stub. That enrichment means you can
render an inbound message without a second round trip, though you should still reconcile
by `version`.

### Favorite folders — 300–302

| Class | Name | Type |
|---|---|---|
| `300` | FavoriteFolderCreate | POST |
| `301` | FavoriteFolderDelete | DELETE |
| `302` | FavoriteFolderUpdate | PUT |

### Account and profile — 400–423

| Class | Name | Type |
|---|---|---|
| `400` | AccountLogin | POST |
| `401` | AccountLogout | POST |
| `402` | AccountConnect | POST |
| `403` | AccountRegister | POST |
| `404` | AccountDeleteDevice | DELETE |
| `405` | ProfileSaved | POST |
| `406` | ProfileDisable | POST |
| `407` | ProfileEnable | DELETE |
| `408` | ProfileDelete | DELETE |
| `409` | ProfilePhotoRated | Standard |
| `410` | ProfilePhotoUploaded | POST |
| `412` | ProfilePhotoDeleted | DELETE |
| `413` | ProfilePhotoProgress | POST |
| `414` | ProfilePhotoSwapped | PUT |
| `415` | ProfileHashtagCreated | POST |
| `416` | ProfileHashtagDeleted | DELETE |
| `417` | ProfileSmsDelivered | POST |
| `418` | AccountRegisterNeeded | Standard |
| `419` | ProfilePhotoVerified | Standard |
| `420` | StripeSyncCompleted | POST |
| `421` | ProfileAgeVerified | POST |
| `422` | FaceLivenessCompleted | POST |
| `423` | ExplicitContentSettingsUrlCreated | POST |

⚠️ **`418 AccountRegisterNeeded` is the session-invalidation signal.** On receipt,
re-run `POST /app/account/register`. `411` is absent.

`402`/`403` are your *own* device completing connect/register — useful for multi-device
coordination, not errors.

### Blocks, store, travel, events

| Class | Name | Type |
|---|---|---|
| `500` | Unblock | DELETE |
| `600` | TransactionCreated | POST |
| `601` | SubscriptionDeactivated | PUT |
| `602` | TransactionFailed | POST |
| `603` | SubscriptionUpdated | PUT |
| `604` | SubscriptionUpdateFailed | PUT |
| `800` | TripCreated | POST |
| `801` | TripDelete | DELETE |
| `802` | TripUpdate | PUT |
| `803` | AmbassadorCreate | POST |
| `804` | AmbassadorDelete | DELETE |
| `901` | EventRsvpCreate | POST |
| `902` | EventRsvpDelete | DELETE |

### Alerts, support, admin, boost

| Class | Name | Type |
|---|---|---|
| `1000` | ServerAlertAvailable | Standard |
| `1100` | TicketCreate | POST |
| `1101` | TicketDelete | DELETE |
| `1102` | TicketUpdate | PUT |
| `1103` | TicketWebhookUpdate | Standard |
| `1200` | AdminAlert | Standard |
| `1201` | AdminActivatePro | POST |
| `1202` | AdminDeactivatePro | DELETE |
| `1203` | ActivateFreeTrial | POST |
| `1204` | AdminActivateBetaFeatures | Standard |
| `1205` | AdminNotice | Standard |
| `1300` | ActivateBoostSuccess | PUT |
| `1301` | ActivateBoostFailure *(deprecated)* | Standard |

### AI search, video chat, moments

| Class | Name | Type |
|---|---|---|
| `1402` | BuildABearContentAvailable | Standard |
| `1403` | BuildABearSearchDeleted | Standard |
| `1404` | BuildABearProgress | Standard |
| `1500` | VideoChatCallReceived | Standard |
| `1501` | VideoChatCallUpdate | Standard |
| `1700` | MomentsCreate | POST |
| `1701` | MomentsDelete | DELETE |
| `1702` | MomentAvailable | Standard |
| `1703` | MomentsMuteCreate | POST |
| `1704` | MomentsMuteDelete | DELETE |
| `1705` | MomentsExclusionsUpdated | POST |
| `1706` | MomentsCreateDuplicated | POST |

ℹ️ `1702 MomentAvailable` fires frequently. Some earlier notes describe it as a
heartbeat; it is not — it announces a new Moment. There is **no application-level
heartbeat frame**; liveness is WebSocket ping/pong ([§5](#5-keepalive-and-reconnection-client)).

A minimal messaging client only needs `200`, `201`, `202`, `208`, `209`, and `418`.

---

## 5. Keepalive and reconnection **[client]**

- **Ping:** WebSocket-level ping every **5 seconds**. No application-level heartbeat.
- **Close:** normal closure is code `1000`.
- **Read limit:** allow generous frames; the reference implementation caps at 10 MiB.
- **Reconnect:** on any close or error, reconnect. The app uses a flat delay; exponential
  backoff with jitter is the better choice for a third-party client.
- **On reconnect, reconcile over REST.** There is no backlog and no resume — anything
  that happened while you were disconnected is only reachable via
  `GET /app/inbox/stream` and `GET /app/chat`.

If frames stop decrypting, or you receive `418 AccountRegisterNeeded`, re-run
`POST /app/account/register` to obtain a fresh `socket.pwd` and reconnect.

---

## 6. The HTTP poll fallback **[client]**

```http
GET /app/socket/poll?request_guids=["<guid1>","<guid2>"]
```

`request_guids` is a **JSON array literal serialised into a single query value**, then URL
encoded.

```jsonc
{ "results": [ /* frames, identical schema to §3 */ ] }
```

⚠️ **Poll results are byte-identical to WebSocket frames** — same envelope, same class
codes — and should go through exactly the same dispatcher. They are **not** encrypted:
this is ordinary HTTPS JSON.

### Which guids are eligible

A `request_guid` becomes pollable when its originating request either carries the header
`PSS-ADD-REQUEST-GUID-TO-SOCKET-POLLING: true`, or targets one of these:

| Verb | Paths |
|---|---|
| `POST` | `/app/account/register`, `/app/account/connect`, `/app/login`, `/app/logout`, `/app/chat`, `/app/store/android`, `/app/albums/permissions`, `/app/profile`, `/app/profile/photo`, `/app/profile/disable` |
| `PUT` | `/app/chat`, `/app/profile/photo` |
| `DELETE` | `/app/chat`, `/app/inbox`, `/app/inbox/thread`, `/app/albums/permissions`, `/app/block`, `/app/profile`, `/app/profile/photo`, `/app/profile/disable` |

`GET`s are never registered. A guid is de-registered immediately if its request returned
non-2xx.

### Timing

The poll ticks on an interval that depends on socket health:

| Socket state | Interval |
|---|---|
| Connected | 5 000 ms |
| Disconnected or connecting | 2 000 ms |
| Not logged in / backgrounded | disabled |

On each tick, each pending guid is included based on its age:

| Age | Action |
|---|---|
| < threshold | **Skip** — give the socket a chance. Threshold is **10 s** when the socket is connected, **2 s** otherwise. |
| threshold … 40 s | **Include** in `request_guids` |
| ≥ 40 s | **Drop.** Give up; no further poll, no retry. |

If no guid qualifies, no HTTP request is made at all.

The interval is also forced to the aggressive rate around purchase flows, where missing a
transaction confirmation matters.

**Net effect:** with a healthy socket the poll almost never fires. With the socket down it
becomes the primary delivery channel at 2-second granularity. A server-side client that
does not want to implement the WebSocket at all can skip straight to polling — you will
still get every event you caused, but **not** unsolicited ones (inbound messages from
others), since those have no `request_guid`. For those, poll `GET /app/inbox/stream`.

---

## 7. Recommended algorithm

1. Log in; persist `device_id`, `aes256_key`, `aes256_iv`.
2. `POST /app/account/register` → `socket{host, port, pwd}` and the CDN hosts.
3. Derive `K = SHA-256(aes256_key)` and `IV = MD5(aes256_iv)`.
4. Open the WebSocket with the three `X-*` headers. Ping every 5 s.
5. Per frame: base64-decode → AES-256-CBC decrypt → PKCS7 strip → parse JSON.
6. Dispatch on `class`:
   - `201` → inbound message from `sender_id`
   - `200` → your own send echoed; correlate by `request_guid`
   - `202` → redact the referenced message
   - `208` → typing from `profile_id`
   - `209` → read receipt; advance to `max_read_version`
   - `418` → session invalid; re-`register` and reconnect
   - anything else → log and ignore; unknown classes are expected
7. **Treat every message event as a trigger**: re-fetch
   `GET /app/chat?profile_id=<peer>` and reconcile by `version`, which is the authoritative
   dense per-conversation sequence.
8. Send over REST (`POST /app/chat`); await the class `200` echo for confirmation.
9. Keep a REST poll of `GET /app/inbox/stream` as the reliable baseline — the socket
   guarantees nothing.

---

## 8. Known gaps

| Gap | Why |
|---|---|
| **Payload shapes for most classes** | Only the chat classes (`200`, `201`, `202`, `208`, `209`) were exercised during capture. The remaining ~80 classes have confirmed names, numbers, and verb correlations, but their `results` payloads are client-derived or unexercised. |
| **`backlog` semantics beyond `0`** | `0` means live. Whether non-zero values indicate replay depth or an ordinal is not determinable from the client. |
| **Server-side close codes** | Only normal closure (`1000`) and abnormal closure were observed. |
| **Class `205`, `411`, `6`–`99`** | Absent from this build; whether they exist server-side is unknown. |
