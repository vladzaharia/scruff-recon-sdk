# SCRUFF API — complete reference

Unofficial, reverse-engineered reference for the SCRUFF app API
(`com.appspot.scruffapp`, Perry Street Software). SCRUFF publishes no API and no
documentation.

**Sources.** Decompilation of the Android app v8.16.0 (`client_semver 8.16.0`,
`client_version 8.1600`, `build 169074`, OkHttp 5.3.2) with `jadx`, plus three mitmproxy
captures of the author's own account taken through an Android 14 emulator with TLS trust
and OkHttp pinning bypassed via Frida.
Raw decompilation notes: [`../research/scruff-apk-decomposition.md`](../research/scruff-apk-decomposition.md).

**Companion documents**
- [`scruff-realtime.md`](./scruff-realtime.md) — the AES-encrypted WebSocket
- [`scruff-enums.md`](./scruff-enums.md) — every enum and numeric wire value
- [`../openapi/scruff.yaml`](../openapi/scruff.yaml) — machine-readable REST spec

## Provenance tags

Every endpoint carries one of these. **Both `[observed]` and `[client]` are reliable** —
they differ in how the fact was established, not in how much you should trust it.

| Tag | Meaning |
|---|---|
| **[observed]** | Request *and* response seen on the wire. Authoritative, including which fields are actually populated in practice. |
| **[client]** | Read directly from the official app's implementation. Paths, methods, parameter names, body shapes, model field names, and enum values are authoritative — HTTP annotations give the exact route and parameter binding, and the JSON adapters give the exact wire names. The one limitation is coverage, not accuracy: a field the app never reads will not appear here even if the server sends it. |
| **[unverified]** | Genuinely not determined, either way. Each instance says what was tried; see [§15](#15-known-gaps). |

> **Why `[client]` is trustworthy here.** The app's HTTP layer is fully declarative —
> routes and parameter bindings are annotations, and every model's JSON field names come
> from generated adapters. The release build obfuscates those annotation classes, so the
> obfuscated-to-real mapping was first recovered from the HTTP library's own annotation
> parser inside the app, then applied consistently. The result is a transcription of the
> app's actual request contract, not an inference about it.

---

## 1. Transport

### 1.1 Hosts

| Purpose | Host | Auth |
|---|---|---|
| API | `https://cdn-api.scruffapp.com` | identity params |
| Profile media | `https://cdn-profilemedia.scruffapp.com` | **none** (unsigned, public) |
| Albums | `https://cdn-album.scruffapp.com` | CloudFront signed |
| Chat media | rotating CloudFront host (e.g. `d2tgz1bwhwwcbm.cloudfront.net`) | CloudFront signed |
| Moments | `d2315u7pex5t3w.cloudfront.net` | CloudFront signed |
| Realtime WebSocket | from `socket.host` (e.g. `d1jj3suym8i42u.cloudfront.net`) | headers |
| Advertised but unused | `cdn-profiles.scruffapp.com`, `cdn-app.scruffapp.com`, `cdn-chat.scruffapp.com`, `cdn-chat2.scruffapp.com` | — |
| Status page | `https://status.scruff.com/index.json` | none |
| Failover config | `https://s3.amazonaws.com/scruffbeta/{domain_fronting,alt_ips}.json` | none |

`app.scruffapp.com` is only the marketing/landing host — it is not the API.

Staging hosts follow `https://<team>.scruffapp.com` where `<team>` ∈ `teamops`,
`teamrevenue`, `teamlabs`, `teamretention`, `teamspare`, `teamuser`.

All paths live under `/app/`.

### 1.2 Headers

| Header | Value |
|---|---|
| `User-Agent` | `okhttp/5.3.2` on **every** request, including media |
| `Accept` | `application/json` |
| `Content-Type` | `application/x-www-form-urlencoded` on POST/PUT; `multipart/mixed` on `POST /app/chat`; `multipart/form-data` on uploads; `application/json` on the few JSON-body endpoints |

**There is no `Authorization` header and no cookies.** Identity travels entirely in
parameters.

Two opt-out headers the app sets internally, honoured by its own interceptors rather than
the server: `PSS-DISABLE-DOMAIN-FRONTING: true` and `PSS-LONG-READ-WRITE-TIMEOUT: true`.

### 1.3 Identity parameters — on every authenticated call

Sent as **query parameters** for `GET`/`DELETE` and as **form fields** (or multipart
parts) for `POST`/`PUT`:

| Field | Value | Notes |
|---|---|---|
| `device_id` | `droid-<32 UPPERCASE hex>` (38 chars) | **The entire credential.** See [§3](#3-authentication). |
| `hardware_id` | `droid-<16 lowercase hex>` (22 chars) | Stable per install, derived from the Android id. **Not a UUID.** Suppressed entirely if it would be `droid-null` or `droid-000000000000000`. |
| `client_version` | `8.1600` | |
| `client_semver` | `8.16.0` | |
| `flavor` | `1` | `1` = SCRUFF (2 = Jack'd, 3 = GROWLr) |
| `device_type` | `2` | `2` = Android |

Plus, on every non-`GET`:

| Field | Value |
|---|---|
| `request_guid` | 32 hex — per-request idempotency key, auto-injected if absent |

⚠️ **A request carries either `device_id` OR (`signature` + `timestamp`), never both** —
except `POST /app/account/connect`, which forces both. See
[§3.2](#32-request-signing-client).

### 1.4 Rate limiting

⚠️ **The server does rate-limit, and the limit is not per-account.** No 429 appears in any
of the three captures, which for a long time suggested there was none — but a later
attempt from the same network was refused with `429 Too Many Requests
(Rate Limit Exceeded)` on the *anonymous* `POST /app/account/register`, a call carrying
no credentials at all. The same 429 then applied to an authenticated register with a
known-good `device_id`, so it is not tied to the account or to the login path.

What the 429 looks like, and what it does not carry:

```http
HTTP/1.1 429 Too Many Requests
Content-Type: text/plain; charset=utf-8
Status: 429 Too Many Requests
X-Cache: Error from cloudfront
```

- The body is plain text — `429 Too Many Requests (Rate Limit Exceeded)` — unlike the
  empty-bodied 400s described below.
- **No `Retry-After` and no `X-RateLimit-*`**, so there is no advertised window to wait
  out and no way to compute a correct backoff. Treat it as an opaque block.
- It arrives immediately rather than after a burst, so it is a standing state rather than
  something a single session triggers.

The scope appears to be the source address: a Recon session from the same host at the
same time worked normally. Reverse-engineering traffic is the obvious way to earn one.

**The client does.** The app ships a per-path token bucket and throws locally before a
request is ever sent. Mirror these rates to behave like a real client:

| Path | Capacity / burst | Refill |
|---|---|---|
| `/app/profile` | 60 | 1.0 / s |
| `/app/location` | 20 | 0.33 / s |
| `/app/chat/media` | 60 | 1.0 / s |
| *(everything else)* | 30 | 2.0 / s |

**Timeouts.** Default connect 25 s, read 20 s, write 20 s. Raised to 60 s read/write for
these 13 grid endpoints (and for anything sending `PSS-LONG-READ-WRITE-TIMEOUT: true`):

`/app/location`, `/app/albums/received`, `/app/albums/permissions`,
`/app/grid/matches_mutual`, `/app/favorite`, `/app/block`, `/app/events/rsvps`,
`/app/explorer/herenow`, `/app/explorer/heresoon`, `/app/explorer/ambassadors`,
`/app/viewers/incoming`, `/app/woofs/incoming`, `/app/inbox/recent`, `/app/inbox/unread`.

### 1.5 Errors **[client]**

**Errors are carried in the HTTP status code, not in a response body.** There is no error
envelope — a rejected request comes back with `Content-Length: 0`. Instead, SCRUFF
defines an extensive vocabulary of **custom status codes in the 4xx range**, and the
client maps each to a message. This is the whole error model.

| Status | Meaning |
|---|---|
| `420` | Too many profiles created in period |
| `421` | Profile deleted too recently |
| `422` | Email already in use |
| `423` | Captcha required → `POST /app/captcha` |
| `424` | Invalid profile name |
| `425` | Unknown location |
| `426` | No profiles |
| `427` | Invalid email |
| `428` | Age restriction |
| `429` | Rate limit |
| `430` | Banned terms in content |
| `431` | Spammy behaviour |
| `432` | SMS validation required → `POST /app/sms/send` |
| `433` | Upgrade required (client too old) |
| `434` | Previously flagged |
| `435` | Feature disabled |
| `436` | Sandbox receipt validation error |
| `437` | Offline |
| `438` | Jailed |
| `439` | Suspended |
| `440` | Carrier not allowed |
| `441` | Country code mismatch |
| `442` | Not associated with suspended account |
| `443` | Too many deliveries |
| `444` | Too many devices |
| `445` | Too many recent sends to region |
| `446` | Too many SMS validations per device |
| `447` | Subscription token invalid |
| `448` | Invalid product |
| `449` | Invalid offer |
| `450` | Payment provider error (Stripe) |
| `451` | Age verification required → [§13](#13-verification-client) |
| `452` | Liveness check required → [§13](#13-verification-client) |

Standard codes are also used conventionally: `400`, `401`, `402` (payment required —
i.e. upgrade to Pro), `403`, `404`, `405`, `406`, `408`, `409`, `410`, `413` (too many),
`415` (image metadata unacceptable), and `500`/`501`/`502`/`503`.

⚠️ **The same code means different things on different endpoints.** There is no global
mapping. `403` alone is: *the user blocked you* on `POST /app/chat`, *cannot delete an
album that still has photos* on `DELETE /app/albums`, *max Pro hides/blocks reached* on
`POST /app/block`, and *invalid password* on `POST /app/account/connect`. Interpret
status codes per endpoint.

A few worth knowing:

| Endpoint | Code | Meaning |
|---|---|---|
| `POST /app/chat` | `403` | User blocked you |
| | `406` | Image too large |
| | `404` | Image unparsable |
| | `426` | No profiles (free-tier browsing limit) |
| `GET /app/chat` | `404` | No more messages — **this is how backfill terminates** |
| `POST /app/account/connect` | `403` | Invalid password |
| `POST /app/block` | `402` / `403` | Max free / max Pro hides+blocks reached |
| | `422` | No recent interaction with that profile |
| `POST /app/favorite` | `405` | Folder not found |
| | `402` / `403` | Max free / max items reached |
| `POST /app/favorite/folders` | `409` | Folder already exists |
| | `403` | Folder not empty |
| `POST /app/poke` | `409` | Already woofed |
| `POST /app/albums/permissions` | `405` | At least one photo required |
| | `402` | Max shares reached |
| `POST /app/albums/cross_user_archive` | `403` | Cannot save into a private album |
| | `409` | Media already uploaded |
| Any grid | `426` | No profiles |
| | `402` | Payment required |

### The three structured error bodies

Almost every error is status-only, but three endpoints return a parseable body:

**`POST /app/profile` → `430`** (banned terms):

```jsonc
{ "results": { "reason": 1, "terms": ["…"], "fulltext": ["…"] } }
```

`reason` is a `BannedTermType` — see [scruff-enums.md §8](./scruff-enums.md#8-moderation-and-reporting).
The same body comes back from `GET /app/moments/creation_allowed`.

**`POST /app/profile/hashtag` → `406`**: `{ "max_hashtags": 10 }`

**`POST /app/account/register` → `401`**: an anonymous config payload, not an error at
all — see [§4.2](#42-anonymous-bootstrap-register-observed).

⚠️ **`429` is real and has been observed**, with no `Retry-After`. It is not per-account —
it applies to unauthenticated calls too. See [§1.4](#14-rate-limiting). The trigger
threshold is still unknown; only the effect has been seen.

⚠️ **Several of these are actionable, not fatal.** `423`, `432`, and `451` are
challenge responses: satisfy the challenge and retry. `433` means your `client_version`
is too old to be served.

Standard codes observed on the wire:

| Status | `Content-Type` | Body |
|---|---|---|
| `400` | `text/html;charset=utf-8` | **Empty.** Generic rejection with no detail. |
| `401` | `text/html;charset=utf-8` | On `register` only: a **JSON anonymous-config payload**, not an error. See [§4.2](#42-anonymous-bootstrap-register-observed). |
| `200` (no body) | `text/html;charset=utf-8` | Success for `poke`, `profile/view`, `inbox/view`, `moment/view`, `chat/typing`, `account/connect`. |

⚠️ Note that **success-with-no-body is typed `text/html`**, not `application/json`. Never
branch on content type; branch on status.

### 1.6 Response conventions

Many endpoints wrap their payload in a results envelope:

```jsonc
{ "results": { /* … */ } }
```

⚠️ **Whether an endpoint wraps is a per-endpoint property, not a global rule** — the app
marks each one explicitly. Wrapped endpoints include everything under `/app/moments*`,
`/app/discover`, `/app/film_festival*`, `/app/chat/media`, `/app/verification*`,
`/app/face_liveness*`, `/app/account/remote_config`, `/app/socket/poll`, and the
streaming grids. Endpoints not listed return their payload bare. `GET /app/profile`
wraps, and its `results` is an **array with one element**.

Grid endpoints use a richer standard envelope — see
[§5.2](#52-the-standard-grid-envelope-observed).

Server infrastructure: nginx + Phusion Passenger behind CloudFront. Every response carries
a redundant `Status: <code> <reason>` header alongside the status line, plus
`cache-control: no-cache`, `Expires: Thu, 01 Jan 1970 00:00:01 GMT`, and CloudFront
`X-Cache`/`Via`/`X-Amz-Cf-*`. Large JSON is gzipped and chunked.

### 1.7 Domain fronting and failover **[observed]**

The app can route around blocking by resolving an alternate CloudFront edge and moving the
real host into a **query parameter**:

```http
GET https://d3m5zvpqmaf1bv.cloudfront.net/app/discover?…&Host=cdn-api.scruffapp.com&signature=…&timestamp=…
Host: cdn-api.scruffapp.com
```

Note the real host appears **both** as a `Host=` query parameter and as the HTTP `Host`
header. Status codes are identical to the direct path.

Configuration comes from two unauthenticated S3 objects, fetched before any API call:

| URL | Purpose |
|---|---|
| `https://s3.amazonaws.com/scruffbeta/domain_fronting.json` | list of fronting URLs (33 bytes observed) |
| `https://s3.amazonaws.com/scruffbeta/alt_ips.json` | alternate IP list for diagnostics (41 bytes) |

⚠️ Bodies were not persisted in the captures — only sizes are known.

Reachability probes: `POST /ping` (with `PSS-DISABLE-DOMAIN-FRONTING: true`) and
`GET /ping_ip?client_version=8.16.0` → `{dfs}`. Service status:
`GET https://status.scruff.com/index.json` → `{"status":"OK","ok":true,"uptime":100.0}`.

**TLS pinning** is enforced over `*.scruffapp.com`; the pin hashes live in the APK's
`res/values/arrays.xml` under `root_certificate_hashes`. A third-party client does not
need to pin.

---

## 2. Identifiers

| Id | Shape | Role |
|---|---|---|
| `device_id` | `droid-<32 UPPERCASE hex>` | **The session credential.** Client-generated, bound to the account by `connect`. |
| `hardware_id` | UUID v4 | Per-install identity. |
| `profile_id` | int64 | Your own, and every other user's. |
| `guid` (message) | 32 hex | Client-generated. **Outbound UPPERCASE, inbound lowercase — compare case-insensitively.** |
| `request_guid` | 32 hex | Per-request idempotency key. |
| `version` (message) | int | **Dense, gap-free, per-conversation sequence starting at 1.** The ordering and sync key. |
| `aes256_key` / `aes256_iv` | 32 hex each | Client-generated at login; key the realtime stream. |
| `socket.pwd` | 32 hex | Returned by `register`; the realtime `X-Token-Auth`. |

⚠️ **A conversation has no id.** Threads are keyed by the **other participant's
`profile_id`**. The `id` field on an inbox conversation object *is* that peer's profile id.

---

## 3. Authentication

A device-bound session with **no tokens, no headers, and no expiry**. The
client-generated `device_id` is the entire ongoing credential, bound to the account once
by an email/password call.

### 3.1 The credential model

- `device_id` is **generated by the client**, not issued by the server. It is
  `"droid-" + 32 hex chars`, i.e. 38 characters in total. The hex is **UPPERCASE**
  (203 of 203 captured device ids), unlike `hardware_id`, which is lowercase
  (219 of 219). Case is not cosmetic: it is a fingerprint.
- `POST /app/account/connect` binds it to the account using email + password.
- Every later request simply carries it. There is **no refresh and no expiry.**
- Treat `device_id` exactly as you would a long-lived bearer token: it is a full account
  credential, and leaking it is equivalent to leaking the password.

### 3.2 Request signing **[client]**

Used only on the pre-authentication endpoints.

```
base      = "<longitude>~<latitude>~<client_version>~<device_type>"
key       = UTF-8 bytes of base
message   = base + ":" + timestamp
signature = lowercase_hex( HMAC-SHA256(key, message) )
```

- `timestamp` is **decimal seconds with six fraction digits** — `String.format("%f", millis/1000.0)`,
  e.g. `1789866828.155000`. Not an integer.
- The HMAC key and the message prefix are the same string. That is unusual but correct.
- When coordinates are unknown the app sends `"0.0"` for both, and
  `location_provider=unknown`.

⚠️ **The third component is `client_version`, not `device_id`.** Several public notes
(including an earlier revision of this project's own docs) state
`<lng>~<lat>~<device_id>~<device_type>`. That is wrong and will produce a signature the
server rejects.

**When it is attached:** only when `device_id` is absent — i.e. on `/app/account/register`
(anonymous), `/app/account/connect`, `/app/account/forgot` — or when the caller forces it,
which only the magic-link path does. Once a `device_id` exists, ordinary calls carry it and
**no signature**.

Alongside `signature` and `timestamp` the app sends `latitude`, `longitude`, and
`location_provider` (the last is a form field only, never a query parameter).

### 3.3 `request_token` **[client]**

A second, different scheme, used on exactly two endpoints:

```
request_token = sha256_hex( reverse("<device_id>:2") + ":" + timestamp )
```

where `2` is `device_type` and `timestamp` uses the same `%f` format. Sent as
`{request_token, timestamp, device_id}`.

Applies only to `GET /app/face_liveness/token` and `POST /app/face_liveness`. Those two
are explicitly excluded from the standard identity-parameter injection.

### 3.4 Fresh-device login **[observed]**

The complete sequence from a cold start, reconstructed from `scruff-bootstrap.flow`.
Only steps 3 and 4 matter.

| # | Call | Auth | Result |
|---|---|---|---|
| 1 | `GET /app/recommendations`, `/app/chat/unsent_messages`, `/app/alerts` | signed | `400`, empty — harmless |
| 2 | `POST /app/account/register` (no `device_id`) | signed | `401` + **anonymous config payload** |
| **3** | **`POST /app/account/connect`** | **signed + `device_id`** | **`200`, empty body** |
| **4** | **`POST /app/account/register`** | **`device_id`, unsigned** | **`200` + full session** |
| 5 | `GET /app/inbox/stream` … | `device_id` | normal operation |

ℹ️ **Step 2 is optional.** The `401` is not a failure — it returns a usable anonymous
configuration (socket endpoint, CDN hosts, feature flags, and the device's suggested
email). It is *not* required to obtain a session. Everything before step 3 is telemetry
and tolerates `400`s.

⚠️ **Step 3 is the one that trips people up.** `connect` carries a `device_id` *and*
forces the signature block, breaking the otherwise-universal
"`device_id` XOR `signature`" rule. Omitting the signature here fails.

### 3.5 `POST /app/account/connect` **[observed]**

The email/password gate.

```http
POST /app/account/connect
Content-Type: application/x-www-form-urlencoded
```

| Field | Notes |
|---|---|
| `email`, `password` | credentials |
| `device_id` | **client-generated**, `droid-<32 UPPERCASE hex>` |
| `old_device_id` | previous device id, for rotation/migration. **The reference implementation omits it.** |
| `refresh_token` | slot; literal `false` when none |
| `login_token` | alternative to password (magic link) |
| `request_guid` | 32 hex |
| `signature`, `timestamp`, `latitude`, `longitude`, `location_provider` | **forced** — see [§3.2](#32-request-signing-client) |
| *(device descriptor)* | see [§3.7](#37-the-device-descriptor-block-observed) |

**Response: `200 OK` with `Content-Length: 0`.** No token, no body. Success is the status
code alone; the client keeps the `device_id` it generated. Follow immediately with
`register`.

### 3.6 `POST /app/account/register` **[observed]**

Loads or refreshes the session. No email or password — it works purely off `device_id`.
Also called on every app foreground to rotate the realtime socket credentials.

| Field | Notes |
|---|---|
| `request_guid` | |
| `debug` | `false` |
| `remote_configs` | `{}` |
| `apple_store_country_code` | optional |
| *(identity + device descriptor)* | |

Full response in [§4.1](#41-the-register-response-observed).

### 3.7 The device descriptor block **[observed]**

Sent on `register`, `connect`, and `forgot`:

| Field | Example |
|---|---|
| `system_name`, `device_name` | `emu64a; google; sdk_gphone64_arm64; Google; sdk_gphone64_arm64` (`Build.DEVICE; BRAND; PRODUCT; MANUFACTURER; MODEL`) |
| `device_os_version`, `system_version` | `34` (`Build.VERSION.SDK_INT`) |
| `locale` | `en-US` |
| `build` | `169074` |
| `user_agent` | `Dalvik/2.1.0 (Linux; U; Android 14; sdk_gphone64_arm64 Build/UE1A.230829.050)` |
| `aes256_key`, `aes256_iv` | 32 hex each — **client-generated**, key the realtime stream |
| `register_count`, `register_count_for_version` | monotonic per app launch |
| `latitude`, `longitude`, `location_provider` | |
| `domain_fronting_enabled`, `domain_fronting_host` | `1`, `www.worldtravelsonline.net` |
| `install_referrer`, `referrer_click_timestamp`, `app_install_timestamp` | attribution |

### 3.8 Other account endpoints **[client]**

| Method | Path | Params |
|---|---|---|
| POST | `/app/account/forgot` | `email` + descriptor + signature |
| POST | `/app/account/magic_link` | **JSON** `{email, device_id}`; signature forced; no identity params |
| POST | `/app/login` · `/app/logout` | no body |
| GET | `/app/account/devices` | → `[{id, device_type, device_id, hardware_id, client_version, created_at, updated_at}]` |
| DELETE | `/app/account/devices` | `target_id` |
| POST/DELETE | `/app/profile/disable` | disable / re-enable account |
| DELETE | `/app/profile` | **delete account** |
| GET | `/app/account/remote_config` | → `{beta, production}` |
| GET/PUT | `/app/account/transactions` | `?subscriptions=1` / `{id, operation}` |
| POST | `/app/account/sensitive_content_settings_url` | `locale` → URL arrives on socket class 423 |
| POST | `/app/captcha` | `g-recaptcha-response`, `android_api` |
| GET | `app/profile/validate_email` | `email` — ⚠️ **no leading slash** in the app source |
| POST | `/app/free_trial` | `activation_code` |
| POST | `/app/review_prompt` | — |

ℹ️ **`POST /app/logout` exists.** Clearing local state without calling it leaves the
device session alive server-side.

---

## 4. The session payload

### 4.1 The `register` response **[observed]**

21 top-level keys. Most clients model a fraction of it; this is the whole thing.

```jsonc
{
  "socket": { "host": "d1jj3suym8i42u.cloudfront.net", "port": 443, "pwd": "<32 hex>" },
  "now": "Sun, 20 Sep 2026 01:18:45 GMT",      // ⚠️ RFC-1123 string, NOT a unix int
  "profile": { /* your own AccountDTO — see §7.1 */ },
  "account_tier": {
    "tier": "pro",                              // "free" | "pro"
    "pro_type": { "type": "paid_subscription", "activated_at": "…", "expires_at": "…",
                  "auto_renew": true, "store_id": "google_play" }
  },
  "device_settings": "{\"unit_type\":1,…}",     // ⚠️ a STRING containing JSON — decode twice
  "cdn":             "https://cdn-profiles.scruffapp.com/",
  "profile_cdn":     "https://cdn-profilemedia.scruffapp.com/",
  "app_cdn":         "https://cdn-app.scruffapp.com/",
  "album_image_cdn": "https://cdn-album.scruffapp.com/",
  "indicators": {
    "woofs":    { "last": "<rfc1123>", "viewed": "<rfc1123>" },
    "albums":   { "last": "…", "viewed": "…" },
    "looks":    { "last": "…", "viewed": "…" },
    "messages": { "last": "…", "viewed": "…" },
    "recent_profiles": [ { "timestamp": "…", "short": { /* ShortProfile */ } } ]
  },
  "favorite_folders": [ { "id": 1, "creator_id": 123, "folder_type": null, "name": "…" } ],
  "travel_alert_warning": false,
  "current_event": { "count": 0, "label": null, "icon": null },
  "stripe_public_key": "pk_live_…",
  "gdpr": false, "kisa": false, "explicit_content_law": false,
  "features": [ { "key": "chat:reactions", "name": "…", "data": null, "enabled": true } ],
  "boost": { "status": { "boost_state": null, "stats_available": false,
                         "eligible_if_online": true, "available_count": 0 } },
  "review_prompt_eligible": true,
  "store_items": { /* see below */ }
}
```

Also present per the APK **[client]**: `is_admin`, `beta`, `tester`, `logview_enabled`,
`stripe_environment`, `age_verification`, `festival_enabled`, `splash_animations`,
`suggested_email`.

⚠️ **`device_settings` is a JSON-encoded *string***, not an object. Decode it a second
time. Keys: `audio_disable`, `audio_outgoing_disable`, `domain_fronting_enabled`,
`domain_fronting_host`, `return_key_send`, `small_chat_image_preview_bubbles`,
`small_thumbnails`, `unit_type`, `vibrate_on_looks_enabled`, `push_disabled`,
`high_contrast`.

⚠️ **Use the returned CDN hosts.** Hardcoding `cdn-profilemedia.scruffapp.com` works today
but the values are served for a reason.

**`features[]`** — 47 entries observed, each `{key, name, data, enabled}`. Observed keys
include `chat:reactions`, `chat:video`, `chat:ephemeral_media`, `chat:typing:bubble`,
`albums:v7`, `albums:profile_tab`, `video:hls:encoding`, `profile:verification`,
`profile:hashtags`, `profile:boost:discover`, `nearby:filters`, `explore:enabled`,
`boost24h:limits:woofs`, `subscription:weekly`. Non-null `data` payloads carry per-feature
config, e.g. `boost24h:limits:viewers → {max_viewers_free: 1000}`.

**`store_items`** — `is_play_store_enabled`, `google_store.{subscriptions, boost,
boost_bundles, pro_pass}.items[]` (Play SKU strings), and
`stripe.subscriptions[] = {id, duration (days), price (cents), currency, play_store_id?}`.

### 4.2 Anonymous bootstrap `register` **[observed]**

The `401` variant from [§3.4](#34-fresh-device-login-observed). Status `401`,
`Content-Type: text/html`, but the body is JSON and useful:

```jsonc
{
  "socket": { "host": "…", "port": 443, "pwd": "…" },
  "now": "<rfc1123>",
  "cdn": "…", "app_cdn": "…", "album_image_cdn": "…",   // note: no profile_cdn
  "current_event": { "count": 0, "label": null, "icon": null },
  "suggested_email": "…",     // ⚠️ the device's Google account address
  "gdpr": false, "kisa": false, "explicit_content_law": false,
  "new_onboarding": true,
  "features": []
}
```

⚠️ **Privacy:** `suggested_email` is the signed-in Google account on the device, returned
to an *unauthenticated* caller. Do not log it.

---

## 5. Profile grid, browse, and search

This is the largest and most useful undocumented part of the API.

### 5.1 One endpoint, 23 paths **[client]**

Every grid in the app — nearby, woofs, viewers, favorites, blocks, albums, Venture, film
voters — is the **same request against a different path**, returning the **same
envelope**. The app declares it once, as a single `GET` with the path supplied at call
time, a free-form query map, and four fixed array parameters (`target_types[]`,
`favorite_folder_ids[]`, `moment_stack_profile_ids[]`, `target_profile_ids[]`).

That means one implementation covers all 23 grids below. Anything you learn about paging
or filtering on one applies to every other.

| Grid | Path | Extra parameter |
|---|---|---|
| Nearby | `/app/location` | — |
| Search | `/app/location` | filter options ([§5.4](#54-search-filters-client)) |
| Hashtag browse | `/app/location` | `hashtags` filter |
| Woofs received | `/app/woofs/incoming` | — |
| Woofs sent | `/app/woofs/outgoing` | — |
| Viewed you | `/app/viewers/incoming` | — |
| You viewed | `/app/viewers/outgoing` | — |
| Albums received | `/app/albums/received` | — |
| Album unlocked-for | `/app/albums/permissions` | `album_id` |
| Unread inbox | `/app/inbox/unread` | — |
| Recent inbox | `/app/inbox/recent` | — |
| Mutual matches | `/app/grid/matches_mutual` | — |
| Event RSVPs | `/app/events/rsvps` | `id` (event) |
| Favorites | `/app/favorite` | `folder_id` |
| Blocks | `/app/block` | — |
| Venture: here now | `/app/explorer/herenow` | `location_id` |
| Venture: here soon | `/app/explorer/heresoon` | `location_id` |
| Venture: ambassadors | `/app/explorer/ambassadors` | `location_id` |
| Venture: online now | `/app/location` | `location_id` |
| Moments exclusions | `/app/moments/exclusions` | — |
| Moments muted | `/app/moments/mutes` | — |
| Moments targeted | `/app/moments/targeted_profiles` | `target_types[]` |
| Film voters | `/app/film_festival/voters` | `film_id` |

**Common query parameters:**

| Param | Type | Required | Notes |
|---|---|---|---|
| `query_sort_type` | int | yes | `0` Distance, `1` Time, `2` Online, `3` DistanceNewness |
| `offset` | int | yes | paging cursor |
| `latitude`, `longitude` | double | yes | |
| `location_provider` | string | yes | e.g. `unknown`, `fused` |
| `limit` | int | no | page size; server default via `block_size` |
| `cache_id` | string | no | echo from the previous response for stable paging |
| `location` | string | no | `"<lat>, <lng>"`, or free-text location |
| `folder_id`, `album_id`, `location_id`, `id`, `film_id` | long | no | grid-specific |
| `target_types[]`, `favorite_folder_ids[]`, `moment_stack_profile_ids[]`, `target_profile_ids[]` | arrays | no | |

⚠️ **Always echo `cache_id` back** when paging. It is an opaque cursor token that keeps
the server-side result set stable; without it, results shift under you between pages.

**The paging contract:** you ask with `offset` (+ `cache_id`, + `limit` where you have
one); the server answers with the page it actually chose and tells you the real stride in
`block_size`. Advance `offset` by `block_size`, not by your requested `limit`.

ℹ️ **`limit` has no client-side default.** It comes from server-delivered remote config
per endpoint, and when the key is absent the app **omits the parameter entirely** and
lets the server decide. Omitting it is the correct default behaviour. See
[scruff-enums.md §15](./scruff-enums.md#15-server-driven-limits) for the per-endpoint
config keys — and note 17 of the 23 grids send no `limit` at all.

`max_free` is the paywall boundary: paging past it returns `426` (no profiles) or `402`
(payment required) rather than an empty page.

### 5.2 The standard grid envelope **[observed]**

```jsonc
{
  "results": [ /* ShortProfile — see §5.3 */ ],
  "max": 500,              // total available
  "max_free": 100,         // free-tier cap
  "offset": 0,
  "count": 873,
  "block_size": 25,        // server page size
  "cache_id": "<opaque>",
  "request_id": null,
  "buckets": {
    "distance": { "meters": { "0": 12, "1": 8 }, "miles": { "0": 5 } },
    "time":     { "0": 3, "1": 7 },
    "online":   { }
  },
  "ui_components": {
    "timer": { "title": "…", "starts_at": "…", "ends_at": "…" },
    "hint":  { "id": "…", "title": "…", "description": "…",
               "cta_button_deep_link": "…", "cta_button_title": "…" },
    "toast": { "title": "…", "cta_button_title": "…", "trigger_offset": 0 }
  }
}
```

`buckets` is a histogram — keys are bucket indices **as strings**, values are counts. The
distance grids return `buckets.distance`; the time-ordered grids (woofs, viewers, albums
received) return `buckets.time`.

`ui_components` is server-driven UI **[client]** and can be ignored.

### 5.3 `ShortProfile` **[observed]**

The object every grid returns. Treat **all** fields as optional — coverage varies by grid.

| Field | Type | Notes |
|---|---|---|
| `id` | int64 | profile id |
| `name` | string | |
| `logged_in`, `online`, `recent` | bool | presence |
| `last_login` | RFC-1123 string | |
| `dst` | double | distance in **metres** |
| `dst_ovr` | double | distance from the search origin **[client]** |
| `has_image` | **int** | ⚠️ a photo *version*, not a boolean |
| `album_images` | int | count |
| `flavors` | int[] | |
| `face_pic` | bool | |
| `traveling`, `new_member` | bool | |
| `hide_distance`, `hide_global` | bool | |
| `disable_read_receipts` | bool | |
| `album_shared_to`, `album_shared_from` | bool | |
| `flag_count_lifetime` | int | |
| `unread` | int | inbox/viewers grids only; **present only when > 0** |
| `action_at` | RFC-1123 string | inbox/viewers/albums-received only; sort key and paging cursor |
| `block_type` | int | `0` Block, `1` Hide **[client]** |
| `profile_photos[]` | object[] | below |

**`profile_photos[]` element:**

```jsonc
{ "version": 3, "thumbnail_key": "<32 hex>-thumb", "fullsize_key": "<32 hex>-full",
  "x_center_offset_pct": 0.5, "y_center_offset_pct": 0.5, "height_pct": 1.0,
  "crop_source": 1, "created_at": "<rfc1123>",
  "verified_status": 0, "moderation_state": 2, "violation": 0 }
```

### 5.4 Search filters **[client]**

Serialised into the same query string as [§5.1](#51-one-endpoint-23-paths-client).

| Param | Type | Enum |
|---|---|---|
| `location` | string | |
| `latitude`, `longitude` | double | |
| `member_name` | string | name search |
| `min_height`, `max_height` | double (cm) | |
| `min_weight`, `max_weight` | double (kg) | |
| `min_age`, `max_age` | int | |
| `body_hair` | int[] | `BodyHair` |
| `ethnicity` | int[] | `Ethnicity` |
| `community` | int[] | `Community` |
| `community_interests` | int[] | `Community` |
| `community_intersection` | bool | AND vs OR |
| `relationship_interests` | int[] | `RelationshipInterest` |
| `relationship_status` | int[] | `RelationshipStatus` |
| `sex_preferences` | int[] | `SexPreference` |
| `sex_preferences_intersection` | bool | |
| `verified` | bool | |
| `online` | int | `OnlineMode` |
| `image` | int | `ImageFilter` |
| `browse_mode` | int | `BrowseMode` |
| `hashtags` | string[] | ⚠️ each element is wrapped in literal `"` quotes by the client |

All enums in [scruff-enums.md](./scruff-enums.md).

ℹ️ **Saved searches are local.** No REST endpoint exists; filters are persisted on-device.

### 5.5 Match stack **[client]**

| Method | Path | Params |
|---|---|---|
| GET | `/app/grid/matches` | `latitude`, `longitude`, `more`, `limit` → `{results, expires, refresh_count, max_refresh}` |
| GET | `/app/grid/matches_mutual` | grid params |
| POST | `/app/rate` | `rating` (`ProfileRating`), `recipient`, `pool`, `bucket`, `pipe`, `browse_mode` |
| DELETE | `/app/rate` | `target_id`, `browse_mode` |
| POST | `/app/rate/reminder` | `target_id`, `pool`, `bucket`, `pipe`, `browse_mode` |

### 5.6 Discover and recommendations **[observed]**

```http
GET /app/discover?latitude=&longitude=&locale=      ← works UNAUTHENTICATED (signed)
```

```jsonc
{ "results": [ {
    "type": "profile_stack",
    "profile_stack": {
      "stack_id": "discover:stack:<kind>:<32 hex>:<your id>",
      "title": "…", "stack_description": "…", "more_title": "See more",
      "has_more": true,
      "style": "even_six",          // even_six | user_carousel | big_corner | multi_tile | individual | alert
      "pipeline": "communities,combined,0.5;hairiness,combined,0.3;woofiness,combined,0.2",
      "bucket": "photo_and_location",
      "user_pool": "scruff",
      "profiles": [ /* ShortProfile */ ]
    } } ] }
```

ℹ️ `pipeline` is the **ranking DSL** — a `;`-separated list of
`<signal>,<mode>,<weight>` triples. It is echoed to the client, not interpreted by it.

Observed stack kinds: `guys_who_viewed_you`, `most_woofed_legacy`,
`most_woofed_new_guys_legacy`, `mutual_matches`, `new_guys_legacy`,
`new_private_photos_shared_with_you`, `online_legacy`, `uploaded_public_photos`.

A `{grid: …}` card variant also exists **[client]**:
`{title, more_title, deep_link, profiles}`.

| Method | Path | Params |
|---|---|---|
| GET | `/app/discover/more` | `stack_id` → `[ShortProfile]` **[client]** |
| GET | `/app/recommendations` | `flavor`, `latitude`, `longitude` → `{stacks: [{stack_type, row, priority, profiles}]}` **[observed]** |
| GET | `/app/banner` | `id`, `locale`, `no_redirect` **[client]** |

`stack_type`: `1` NewUsersNearby, `2` MostWoofedNearby.

### 5.7 Server alerts **[client]**

| Method | Path | Params |
|---|---|---|
| GET | `/app/alerts` | `latitude`, `longitude`, `build`, `register_count`, `system_version`, `locale`, `existing_alert_identifiers` |
| PUT | `/app/alerts` | `downloaded_at`, `alert_count` |
| POST | `/app/alerts/survey` | `alert_id`, `value` |
| GET | `/app/alerts/templates` | `name`, `version`, `platform` → `{url, version}` |

⚠️ `GET /app/alerts` returned `400` in every capture — consistent with
`latitude=0.0&longitude=0.0`. Supply real coordinates.

`ServerAlertDTO` has 33 fields; see [scruff-enums.md](./scruff-enums.md) for
`ServerAlertType`, `ServerAlertNavigationType`, `ServerAlertDisplayLocation`.

---

## 6. Messaging

### 6.1 Inbox stream **[observed]**

```http
GET /app/inbox/stream?cutoff_timestamp=<unix>&limit=<n>
```

Conversation list, sorted by `action_at` descending. Omit `cutoff_timestamp` for the first
page (25 items); supply it to page (50 items).

```jsonc
{
  "results": [ {
    /* …all ShortProfile fields; `id` is the PEER's profile id… */
    "unread": 2,                  // present only when > 0
    "action_at": "<rfc1123>",     // sort key and paging cursor
    "messages": [ /* 1–2 most recent, same schema as §6.2, plus per-message `unread` bool */ ]
  } ],
  "max": 0, "max_free": 0, "offset": 0, "block_size": 25,
  "timestamp": 1789866828, "count": 873, "request_id": null
}
```

**New-activity detection:** poll page 1; any conversation whose `action_at` is newer than
your high-water mark, or which has `unread > 0`, has new messages.

| Method | Path | Params |
|---|---|---|
| POST | `/app/inbox/view` | `last_message_timestamp` (unix) — clears unread badges **[observed]** |
| DELETE | `/app/inbox` | `unread_only` (bool) **[client]** |
| DELETE | `/app/inbox/thread` | `profile_id` **[client]** |
| GET | `/app/inbox/unread` · `/app/inbox/recent` | grid params **[client]** |

### 6.2 Fetch messages **[observed]**

```http
GET /app/chat?profile_id=<peer>&inbox_style=2&free_features=[]&max_version=<n>&limit=<n>
```

| Param | Required | Notes |
|---|---|---|
| `profile_id` | yes | the peer — this *is* the conversation key |
| `inbox_style` | yes | client sends `2` |
| `free_features` | — | URL-encoded JSON array, usually `[]` |
| `max_version` | no | **backfill cursor** — returns messages with `version < max_version` |
| `limit` | no | page size |

```jsonc
{
  "profile_id": 123456,
  "results": [ /* ASCENDING by version */ ],
  "min_version": 0,          // 0 = full history available
  "min_version_free": 40,    // free-tier floor for this page
  "max_read_version": 87,    // read watermark
  "disable_read_receipts": false
}
```

**Message object:**

```jsonc
{
  "id": 987654321,            // global monotonic
  "sender_id": 123456,
  "recipient_id": 654321,
  "guid": "<32 hex>",         // ⚠️ outbound UPPERCASE, inbound lowercase
  "version": 42,              // ⚠️ dense per-conversation sequence from 1 — the sync key
  "message_type": 1,          // see scruff-enums.md
  "created_at": "<rfc1123>",
  "message": "…",             // text for type 1; JSON blob for type 8; absent for media
  "unread": true,             // inbox previews only
  "reply": {                  // optional quote
    "guid": "<32 hex>",       // …either a message reply…
    "moment_id": 55,          // …or a Moment reply
    "moment_result": { "image_url": "…", "media_type": 0 }
  },
  "fullsize_width": 1080, "fullsize_height": 1920,   // media types
  "media_behavior": 0,        // 0 normal, 1 single-view
  "album_id": 77, "is_album_shared": true,
  "album_blurhash": "…", "album_version": "<32 hex>",
  "sender": { /* ShortProfile — WebSocket deliveries only */ }
}
```

⚠️ **`version`, not `id`, is the ordering key**, and it is dense and gap-free per
conversation starting at 1. Sync forward by keeping the highest `version` you hold;
backfill by re-requesting with `max_version = lowest held version` until you reach
`version == 1` or `min_version + 1`.

⚠️ **`guid` case differs by direction.** Compare case-insensitively or you will duplicate
your own sent messages when they echo back.

### 6.3 Send **[observed]**

```http
POST /app/chat
Content-Type: multipart/mixed; boundary=<uuid>
```

Parts, **in the order the app sends them** — the first five carry
`Content-Transfer-Encoding: binary` and `Content-Type: text/plain; charset=utf-8`:

| # | Part | Notes |
|---|---|---|
| 1 | `image` | file part, `image.jpg`, `image/jpg` — types 2/6 |
| 2 | `video` | file part, `application/octet-stream` — types 3/7 |
| 3 | `recipient` | peer profile id |
| 4 | `guid` | client-generated **UPPERCASE** 32 hex — the message guid |
| 5 | `request_guid` | **equal to `guid`** in observed sends |
| 6 | `message_type` | see [scruff-enums.md](./scruff-enums.md) |
| 7 | `location` | type 4 |
| 8 | `reaction` | emoji — type 8 |
| 9 | `reacted_to` | target message guid — type 8 |
| 10 | `message` | text body |
| 11 | `album_image_id` | |
| 12 | `media_identifier` | always null in this build |
| 13 | `video_filename` | |
| 14 | `mute` | |
| 15 | `media_behavior` | `0` normal, `1` single-view |
| 16 | `reply[guid]` | message reply |
| 17 | `album_share_limit` | |
| 18 | `shared_album_id` | type 12 |
| 19 | `reply[moment_id]` | Moment reply |
| — | identity parts | `hardware_id`, `client_version`, `client_semver`, `flavor`, `device_type`, `device_id` |

```jsonc
{ "results": { "guid": "<32 hex>" } }
```

The send also echoes back on the realtime socket as class `200` with a matching
`request_guid`.

⚠️ **Album sharing goes through chat**, not a permissions endpoint: send
`message_type=12` with `shared_album_id` and `album_share_limit`. See
[§8](#8-private-albums).

### 6.4 Read receipts, typing, drafts **[observed]** / **[client]**

| Method | Path | Params |
|---|---|---|
| PUT | `/app/chat` (unsend) | `profile_id`, `unsent=true`, `version`, `guid`, `request_guid`, `free_features` |
| PUT | `/app/chat` (viewed) | `profile_id`, `viewed=true`, `version`, `guid` |
| DELETE | `/app/chat` | `profile_id` — deletes the thread |
| POST | `/app/chat/message_viewed` | `version`, `guid`, `target_id` |
| POST | `/app/chat/typing` | `target_id` — **send-only**, no receive counterpart over REST |
| GET | `/app/chat/recent` | — |
| GET | `/app/chat/gifs` | `q`, `lang` |
| GET/POST/DELETE | `/app/chat/unsent_messages` | drafts, server-synced per target |

**Read state** is the `max_read_version` watermark: a message is read by the other side
iff `version <= max_read_version`. `disable_read_receipts` suppresses it.

Typing *receipt* arrives over the socket as class `208`, not over REST.

### 6.5 Chat media **[observed]**

```http
GET /app/chat/media?guid=&message_type=&media_type=0&sender_id=&recipient_id=&version=
```

`media_type`: `0` image, `1` video.

```jsonc
{ "results": { "image_url": "…", "video_url": "…", "manifest_url": "…",
               "manifest_cookies": { "CloudFront-Policy": "…", "CloudFront-Signature": "…",
                                     "CloudFront-Key-Pair-Id": "…", "expires_at": "…" } } }
```

⚠️ URLs are **CloudFront-signed and expiring**, and the host rotates. Use them verbatim
and re-resolve on 403. There is a single full size — no separate thumbnail.

For HLS video, `manifest_url` is an `.m3u8` and `manifest_cookies` must be sent as cookies
to fetch the segments. Downloading the manifest alone gets you a useless text file.

### 6.6 Realtime fallback **[client]**

```http
GET /app/socket/poll?request_guids=["<guid1>","<guid2>"]
```

⚠️ `request_guids` is a **JSON array literal serialised into one query value**, then URL
encoded — not a comma-separated list.

Returns `{"results": [...]}` of frames **identical in schema to WebSocket frames** (and
unencrypted — this is ordinary HTTPS JSON). It only returns events *you caused*, matched
by `request_guid`; unsolicited events such as inbound messages have no guid and never
appear here.

Full semantics — which requests register a guid, the 10 s/2 s inclusion thresholds, the
40 s give-up, and the 5 s/2 s poll cadence — are in
[scruff-realtime.md §6](./scruff-realtime.md#6-the-http-poll-fallback-client).

---

## 7. Profiles

### 7.1 Fetch **[observed]**

```http
GET /app/profile?target=<id>&latitude=&longitude=
      &thumbnail_constraint=-thumbnail&fullsize_constraint=-fullsize&api_version=2
```

⚠️ `api_version=2` matters — the app always sends it.

Returns `{"results": [ProfileDTO]}` — an **array with one element**.

`ProfileDTO` is `ShortProfile` plus roughly 40 more fields: `about`, `city`, `ideal`,
`fun`, `notes`, `age_in_years`, `height` (cm), `weight_kg`, `body_hair`, `ethnicity`,
`relationship_status`, `relationship_interests[]`, `community[]`, `community_interests[]`,
`sex_preferences[]`, `sex_safety_practices[]`, `vaccinations[]`, `hiv_status`,
`last_tested_at`, `testing_reminder_frequency`, `accepts_nsfw_content`, `browse_mode`,
`looking_for`, `my_rating`, `his_rating`, `favorite`, `folders[]`,
`shared_album_ids_to[]`, `gender_identities[]`, `pronouns[]`, `hashtags[]` (each with
`mutual`), `urls[]` (`{service, url}`), `partner`, `home_location`, `trips[]`, `rsvps[]`,
`ambassadors[]`, `video_chat.enabled`, `bucket`/`pipe`/`pool`.

⚠️ **Nullability is much wider on foreign profiles than on your own.** `city`,
`ethnicity`, `about`, `fun`, `relationship_status`, `ideal`, `body_hair`, `height`,
`weight_kg`, `last_tested_at`, `hiv_status`, `accepts_nsfw_content`, `verified_status`,
`partner`, and `home_location` are all nullable, and `created_at` was `null` on every
observed foreign profile.

**Nested objects:**

```jsonc
"home_location": { "id": 1, "geo_city_id": 2, "location_type": 0, "has_image": 1,
                   "latitude": 0.0, "longitude": 0.0,
                   "iso2_country_code": "US", "name": "…", "gdpr": false },
"trips": [ { "id": 1, "category": 3, "profile_id": 123, "notes": null,
             "ongoing": false, "server_created": false,
             "starts_at": "…", "ends_at": "…", "location": { /* as home_location */ } } ],
"rsvps": [ { "id": 1, "title": "…", "city": "…", "starts_at": "…",
             "image_url": "…", "time_zone": "…", "dst": 0.0 } ]
```

Your own profile (`AccountDTO`, in the `register` response) adds `email`, `birthday`,
`created_at`, `updated_at`, `home_location_id`, `disabled`, `stealth`, `overnight`,
`promos`, `has_password`, `discover_global_top`, `verified_status`,
`explicit_content_visibility`, `suggestive_content_visibility`, `photo_index0`–`5`,
`spam_score`, `flag_count`, `jailed_until`, `predicted_filters`, and every `hide_*` /
`disable_*` flag.

### 7.2 Edit **[client]**

```http
POST /app/profile
Content-Type: application/json
```

Body is `AccountParamsDTO` — **every editable field**. The app additionally merges in
`timezone`, the request-token block, standard identity params, and all register params.

- **Required:** `request_guid`. Also accepts `password`, `id`.
- **Text:** `about`, `ideal`, `fun`, `city`, `name`, `email`, `notes`
- **Dates:** `birthday`, `last_tested_at`
- **Numbers:** `height` (cm, double), `weight_kg` (double)
- **Enum ints:** `ethnicity`, `looking_for`, `relationship_status`, `body_hair`,
  `accepts_nsfw_content`, `hiv_status`, `testing_reminder_frequency`,
  `explicit_content_visibility`, `suggestive_content_visibility`, `browse_mode`,
  `home_location_id`
- **Enum int arrays:** `community`, `community_interests`, `flavors`,
  `relationship_interests`, `sex_preferences`, `sex_safety_practices`, `vaccinations`
- **Objects:** `partner` (profile id), `urls[]`, `gender_identities[]`, `pronouns[]`,
  `hashtags[]`, `remote_configs`
- **Booleans:** `hide_message_preview`, `hide_age`, `hide_alerts`, `hide_distance`,
  `hide_discover`, `hide_discover_most_woofd`, `hide_trending_moments`,
  `hide_nearby_moments`, `hide_global`, `hide_stats`–`hide_stats4`, `hide_hosting`,
  `hide_notification_images`, `show_sensitive_content`, `discover_global_top`, `promos`,
  `logged_in`, `has_password`, `stealth`, `overnight`, `traveling`,
  `disable_auto_trip_creation`, `disable_auto_travel_creation`, `disable_video_chat`,
  `disable_read_receipts`, `age_verification_failed`

⚠️ **Asymmetric key:** the write key is `disable_auto_travel_creation` but the read model
exposes `disable_auto_travel_icon`.

### 7.3 Photos, hashtags, notes **[client]**

| Method | Path | Params |
|---|---|---|
| POST | `/app/profile/photo` | **multipart**: `request_guid`, `photo_index`, `crop_top/left/bottom/right`, `crop_x_center_offset_pct`, `crop_y_center_offset_pct`, `crop_height_pct`, `system_cropped_thumbnail`; file parts `original`, `image`, `thumbnail` |
| PUT | `/app/profile/photo` | `from`, `to` — reorder |
| DELETE | `/app/profile/photo` | `photo_index` |
| POST | `/app/profile/note` | `target_id`, `note` — private note about another user |
| GET | `/app/profile/genders` | → `{results, pinned_count}` |
| GET | `/app/profile/pronouns` | → `[{id, name}]` |
| GET | `/app/profile/popular_hashtags` | `query` → `[{hashtag, count}]` |
| POST/DELETE | `/app/profile/hashtag` | `hashtag` / `hashtag_id` |
| POST | `/app/profile/view` | `target` — records a "look" |
| POST | `/app/profile/device_settings` | `device_settings` (JSON **string**) |
| GET | `/app/profile/stats` | `profile_id` — Insights |

### 7.4 Media renditions **[observed]**

Profile photos are served **unsigned** from `cdn-profilemedia.scruffapp.com`:

```
https://cdn-profilemedia.scruffapp.com/<32 hex>-thumb
https://cdn-profilemedia.scruffapp.com/<32 hex>-full
```

Keys come from `profile_photos[].thumbnail_key` / `.fullsize_key`. `image/webp`,
cacheable by etag.

Constraint suffixes accepted as `thumbnail_constraint` / `fullsize_constraint`:

| Fullsize | Thumbnail |
|---|---|
| `-fullsize`, `-fullsize-small`, `-fullsize-400K`, `-fullsize-800K`, `-fullsize-1600K`, `-fullsize-6400K`, `-original-50000K` | `-thumbnail`, `-thumbnail-small`, `-thumbnail-300`, `-thumbnail-600` |

A photo's `etags` map enumerates which renditions exist for it.

---

## 8. Private albums

### 8.1 Album lifecycle **[client]**

| Method | Path | Params | Socket class |
|---|---|---|---|
| GET | `/app/albums` | `exclude_private_album`, `exclude_recent_album`, `target_id` | — |
| POST | `/app/albums` | `name`, `album_type` | 100 |
| PUT | `/app/albums` | `album_id`, `name` | 101 |
| DELETE | `/app/albums` | `album_id` | 102 |

```jsonc
// AlbumDTO
{ "id": 1, "name": "…", "album_type": 2, "count": 12, "is_album_shared": true,
  "album_version": "<32 hex>", "first_image": { /* AlbumImageDTO */ },
  "is_default": false, "profile": { /* ShortProfile */ } }
```

`album_type`: `0` Archive, `1` Recent, `2` Private, `3` ProfileSynthetic, `4` ChatSynthetic.

### 8.2 Album contents **[observed]**

```http
GET /app/albums/images?album_id=&target_id=&limit=
      &full_size_constraints[]=-fullsize&thumbnail_constraints[]=-thumbnail
```

⚠️ Two encodings of the same parameters exist in the wild:
`full_size_constraints[]=-fullsize` (repeated) and
`fullsize_constraints=["-fullsize"]` (URL-encoded JSON array). Note the
singular/plural inconsistency. The repeated-bracket form is what the current build sends.

```jsonc
{
  "album_id": 1,
  "album": { "id": 1, "name": "…", "created_at": "…", "updated_at": "…",
             "is_default": false, "album_type": 2, "profile": { /* ShortProfile */ } },
  "results": [ {
    "id": 10, "guid": "<32 hex>", "album_id": 1, "profile_id": 123,
    "creator_id": 123, "sender_id": null,
    "pending_moderation": false,
    "media_type": 1,                    // 1 image, 2 video, 3 gif, 4 HLS
    "media_hash": "…", "sort_order": 0, "caption": null,
    "fullsize_width": 1080, "fullsize_height": 1920,
    "video_length_seconds": null,
    "created_at": "…", "updated_at": "…",
    "fullsize_url": "…", "fullsize_url_legacy": "…",
    "thumbnail_url": "…", "thumbnail_url_legacy": "…",
    "video_url": "…", "manifest_url": "…",
    "manifest_cookies": { "CloudFront-Policy": "…", "CloudFront-Signature": "…",
                          "CloudFront-Key-Pair-Id": "…" }
  } ],
  "offset": 0, "block_size": 25
}
```

Album URLs are CloudFront-signed with a **wildcard resource per image hash** — one
signature covers `-fullsize`, `-thumbnail`, and `-video` for that image.

### 8.3 Album images **[client]**

| Method | Path | Params | Socket |
|---|---|---|---|
| POST | `/app/albums/images` | **multipart**: `guid`, `request_guid`, `album_id`, `album_type`, `sort_order`, `exif`; files `image` (`image/jpg`), `video` | 105 |
| PUT | `/app/albums/images` | move: `album_id`, `image_id` → 107; edit: + `sort_order`, `caption` → 108/110 | |
| DELETE | `/app/albums/images` | `image_id` | 106 |
| PUT | `/app/albums/images/download` | `image_id` → signed video URL arrives on socket 112 | 112 |
| GET | `/app/albums/cover_image` | `album_id`, `album_version` → `{signed_url, album_version}` | — |
| POST | `/app/albums/cross_user_archive` | `album_id`, `image_id` — save someone's album image | 111 |
| POST | `/app/albums/chat_archive` | `message_guid`, `album_id`, `message_version`, `thread_key` | 109 |

### 8.4 Sharing **[client]**

| Method | Path | Params | Socket |
|---|---|---|---|
| DELETE | `/app/albums/permissions` | `target_ids[]`, `album_id` — **unshare** | 104 |
| GET | `/app/albums/permissions` | grid params — "unlocked for" grid | — |
| GET | `/app/albums/received` | grid params — albums shared with you | — |

⚠️ **There is no `POST /app/albums/permissions` call site in v8.16.0.** The endpoint
exists server-side — socket class `103 AlbumPermissionGrant` maps to it, and the client
registers it as an awaited callback — but sharing in this build happens implicitly by
**sending a chat message of `message_type=12`** carrying `shared_album_id` and
`album_share_limit`. See [§6.3](#63-send-observed).

---

## 9. Moments

Ephemeral stories.

| Method | Path | Params | Tag |
|---|---|---|---|
| GET | `/app/moments` | — → `{exclusions_count, my_moments, moments, nearby_moments}` | [observed] |
| GET | `/app/moments/show` | `moment_id` → `{moment}` | [client] |
| GET | `/app/moments/nearby` | `latitude`, `longitude` | [client] |
| GET | `/app/moments/trending` | `latitude`, `longitude`, `locale`, `cache_id` | [client] |
| GET | `/app/moments/tap_ins` | `tap_in_id` | [client] |
| GET | `/app/moments/creation_allowed` | `overlays[]` | [client] |
| GET | `/app/moments/targets` | — → `[ProfileDTO]` | [client] |
| GET | `/app/moments/targeted_profiles_preview` | `exclusions[]` | [client] |
| POST | `/app/moment` | **multipart** — see below | [client] |
| DELETE | `/app/moment` | `moment_id` | [client] |
| POST | `/app/moment/view` | `moment_id` | [observed] |
| POST/DELETE/GET | `/app/moments/mutes` | `target_profile_id` / `target_profile_ids[]` / grid | [client] |
| POST/GET | `/app/moments/exclusions` | `exclusion_list_subtractions` (JSON array string) / grid | [client] |
| POST | `/app/flag` | `flag[profile_id]`, `flag[reason]`, `flag[moment_id]` — report | [client] |

`POST /app/moment` multipart parts: `request_guid`, `guid`, `target_profile_ids`,
`favorite_folder_ids`, `target_types`, file `image`, file `video`,
`crop_top/left/bottom/right`, `exclusion_list_additions`, `exclusion_list_subtractions`,
`temporary_exclusions`, `mute`, `exif`, `duration_in_hours`, `overlay_texts`,
`tap_in_data`.

```jsonc
// MomentDTO
{ "id": 1, "guid": "<32 hex>", "created_at": "…",
  "image_url": "…", "media_type": 0,          // 0 image, 1 HLS video
  "manifest_url": "…", "manifest_cookies": { … },
  "viewed": false, "muted": false,
  "classification_status": 2, "classification_outcome": 2,
  "tap_in": { "id": 1, "title": "…",
              "placement": { "scale": 1.0, "rotation": 0.0,
                             "x_offset_pct": 0.5, "y_offset_pct": 0.5 },
              "additional_user_count": 3, "profiles": [ /* ShortProfile */ ] } }
```

Expiry is set at post time via `duration_in_hours`; there is no explicit expiry field on
read. Audience targeting uses `MomentTargetGroup` ids — see
[scruff-enums.md](./scruff-enums.md).

⚠️ `GET /app/moments/trending` returned `400` in all four observations, consistent with
`latitude=0.0&longitude=0.0`.

---

## 10. Social relations

| Method | Path | Params | Tag |
|---|---|---|---|
| POST | `/app/poke` | `recipient`, `moment_id?` — **woof** (socket class 1) | [observed] |
| GET | `/app/woofs/incoming` · `/app/woofs/outgoing` | grid params | [observed] |
| GET | `/app/viewers/incoming` · `/app/viewers/outgoing` | grid params | [observed] |
| POST | `/app/profile/view` | `target` — records a look | [observed] |
| POST | `/app/favorite` | `recipient`, `folder_id?`, `limit?` | [client] |
| PUT | `/app/favorite` | `target_id`, `folder_ids[]`, `limit?` — set folders | [client] |
| DELETE | `/app/favorite` | `recipient`, `folder_id?` | [client] |
| GET | `/app/favorite` | grid params | [client] |
| GET | `/app/favorite/folders` | → `[{id, name, folder_type}]` | [client] |
| POST/PUT/DELETE | `/app/favorite/folders` | `name` / `folder_id`+`name` / `folder_id` | [client] |
| POST | `/app/block` | `recipient`, `hide` (bool), `limit?` — `hide=true` hides, `false` blocks | [client] |
| DELETE | `/app/block` | *(no params)* clears all; or `target_id`; or `target_ids[]` | [client] |
| GET | `/app/block` | grid params | [client] |

⚠️ `DELETE /app/block` **with no parameters deletes every block**. Guard against sending
it accidentally.

Client-side limits leak through the telemetry `remote_configs` block **[observed]**:
`limit_get_nearby_users` 100, `limit_hide_block_user_v2` 150, `limit_get_looks_v2` 25,
`limit_get_woofs_v2` 15, `limit_add_favorite_user` 80.

---

## 11. Venues, events, travel (SCRUFF Venture)

| Method | Path | Params | Socket |
|---|---|---|---|
| GET | `/app/events` | `latitude`, `longitude`, sort options; legacy also `metric=1`, `hot=1`, `featured=1`, `location_id` | — |
| GET | `/app/events/details` | `event_id`, `latitude`, `longitude` | — |
| POST/DELETE | `/app/events/rsvps` | `event_id` | 901 / 902 |
| GET | `/app/events/rsvps` | grid params | — |
| GET | `/app/events/profile` | `profile_id` | — |
| GET | `/app/explorer/cities` | `latitude`, `longitude` → `[LocationDTO]` | — |
| GET | `/app/explorer/details` | `location_id` → `{here_now, here_soon, ambassadors, online, events, profile_details}` | — |
| GET | `/app/explorer/geocode` | `query`, `language_code`, `country_code` | — |
| POST/DELETE | `/app/explorer/ambassadors` | `location_id` / `id` | 803 / 804 |
| GET | `/app/explorer/ambassadors` · `/herenow` · `/heresoon` | grid params + `location_id` | — |
| GET | `/app/explorer/trips` | `profile_id` | — |
| POST/PUT | `/app/explorer/trips` | `location`, `location_id`, `id`, `starts_at`, `ends_at`, `notes`, `category`, `ongoing?`, `server_created?` | 800 / 802 |
| DELETE | `/app/explorer/trips` | `id` | 801 |
| GET | `/app/static_map` | `scale`, `size`, `zoom`, `latitude`, `longitude` → image |
| POST | `/app/location` | `latitude`, `longitude`, `speed`, `provider` | — |

All **[client]**.

⚠️ **`POST /app/location` and `GET /app/location` are unrelated.** The POST publishes your
position; the GET is the nearby profile grid ([§5](#5-profile-grid-browse-and-search)).

`EventDTO` has 30 fields including `title`, `description`, `city`, `address`,
`tickets_url`, `starts_at`, `ends_at`, `latitude`, `longitude`, `rsvp_count`,
`rsvp_profiles`, `has_rsvpd`, `child_events`, `time_zone`, `dst`.

---

## 12. Membership, store, boost **[client]**

| Method | Path | Params | Socket |
|---|---|---|---|
| POST | `/app/store/android` | `legacy_transaction`, `signature`, `signed_data`, `item_id`, `order_id`, `pss_offer_id`, `source`, `analytics_properties` | 600 / 602 |
| GET | `/app/store/stripe/customer_session` | → `{customer_id, customer_session_client_secret}` | — |
| POST | `/app/store/stripe/setup_intent` | → `{setup_intent_client_secret}` | — |
| POST | `/app/store/stripe/payment_intent` | `product_id`, `offer_id` | — |
| POST | `/app/store/stripe/payment_method` | `payment_method_id`, `customer_id` | — |
| GET | `/app/store/stripe/subscription_details` | `product_id`, `offer_id` | — |
| POST | `/app/store/stripe/subscription_status` | `payment_intent_id` | 420 |
| GET | `/app/store/boost` | → `BoostStatusDTO` | — |
| PUT | `/app/store/boost` | `source` — activate | 1300 |
| GET | `/app/boost/latest_stats` | → `{woofs_count, looks_count, unlocks_count, chats_count}` | — |

Admin-only, gated on `is_admin`: `POST /app/trials/admin_grant`,
`POST|DELETE /app/trials/admin_activate`, `POST /app/boost/grant`,
`POST /app/trials/admin_beta_features`.

---

## 13. Verification **[client]**

| Method | Path | Params |
|---|---|---|
| GET | `/app/age_verification/token` | → `{session_id, region}` |
| POST | `/app/age_verification` | `session_id` |
| GET | `/app/face_liveness/token` | **`@InjectRequestToken`** → `{session_id, region}` |
| POST | `/app/face_liveness` | **`@InjectRequestToken`** — `session_id`, `source` |
| POST | `/app/sms/send` | `phone_number`, `manual` |
| POST | `/app/sms/validate` | `code` |
| GET | `/app/verification/poses` | → `[{pose_id, pose_url, guid, presigned_url}]` |
| POST | `/app/verification` | `pose_id1`, `pose_id2`, `guid1`, `guid2` |
| PUT | *(the pose `presigned_url`)* | raw body — **S3, off-host, unauthenticated** |

The two face-liveness endpoints use the `request_token` scheme from
[§3.3](#33-request_token-client), not the HMAC signature.

---

## 14. Miscellaneous **[client]**

| Method | Path | Notes |
|---|---|---|
| POST | `/app/push` | `device_token`, `push_environment` |
| POST | `/app/logs/event` | `events` — JSON array string of `{category, action, label, value, profile_id, client_version, timestamp}` → `{last_timestamp}` |
| POST/PUT | `app/video_chat/room` | `target_id` / `room_id`, `receiver_id`, `status` — ⚠️ **no leading slash** in source |
| GET/POST | `/app/support/ticket` | multipart `ticket` (JSON string), `survey_name`, file `image` |
| POST | `/app/support/ticket/update` | same shape |
| DELETE | `/app/support/ticket` | `id` |
| GET | `/app/support/ticket_remote_url` | WebView URL |
| GET/POST | `/app/support/survey` | `id`, `system_version` / multipart |
| GET/POST | `/app/flag` | fetch the report form template / submit a report |
| GET | `/app/film_festival` · `/details` · `/voters` | film festival |
| POST/DELETE | `/app/film_festival/vote` | `film_id` |
| GET/DELETE | `/app/ai/searches` | AI search ("Build-a-Bear") |
| POST | `/app/ai/searches/feedback` | **JSON** `{response_id, feedback, comment, profile_ids}` |
| POST | `/app/ai/submit_answer` | `response_id`, `question_id` |
| GET | `/app/ai/upload_path` | `question_id` → `{url, response_id}`; then `PUT` raw audio to that URL (off-host) |

⚠️ **Profile report reasons are server-driven** — fetch the form template from
`/app/flag` or `/app/support/survey` and render it. Only `FlagMomentReason` (for Moments)
is a client-side enum.

**WebView/HTML endpoints** (same host, identity params appended):
`/app/faqs/{tos, privacy, guidelines, safersex, verification, moments, boost, …}` and
`/app/redir/{facebook, instagram, twitter, merchandise}`.

---

## 15. Known gaps

Honest inventory of what this document genuinely cannot tell you.

| Gap | Why, and what was tried |
|---|---|
| **`inbox_style` values other than `2`** | The literal `2` is the only occurrence in the entire app — no constant, no enum, no alternative branch. Formats `0`/`1` are unreferenced by any code path, live or dead. |
| **Profile report reason ids** | Genuinely server-driven. The reason list lives only in the form definition returned by `GET /app/support/survey?id=flag`, which the app renders dynamically. Checked for a bundled fallback list, a `*ReportReason*` class, and a string-array — none exists. Only Moments has a client-side reason enum. |
| **Legacy `looking_for` / `flavors` numbering** | The app has no enum for either and round-trips them untouched. The modern successors (`RelationshipInterest`, `Community`) are the strong hypothesis for the numbering, but the legacy vocabulary is not in the binary. Echo what the server sends. |
| **Server-side handling of `free_features`** | The client's *intent* is unambiguous from the call sites, but what the server does with the assertion is inference. |
| **Default values for remote-config limits** | Genuinely absent. The numeric resolver has no fallback branch — when a key is missing the client omits the parameter entirely and lets the server choose. |
| **`POST /app/albums/permissions` request shape** | The endpoint exists server-side (realtime class `103` maps to it, and the client registers it as an awaited callback) but v8.16.0 has **no call site**. Sharing goes through a chat message of type 12 instead. |
| **Failover JSON bodies** | `domain_fronting.json` (33 B) and `alt_ips.json` (41 B) were not persisted in the captures — only their sizes are known. |
| **Unexercised write surfaces** | No live traffic for favorites/folder mutation, blocks, search filters, profile editing, photo upload, album create/share, venues, purchases, or push registration. All `[client]`-derived — reliable for request shape, unconfirmed for response detail. |
| **Payload shapes for most realtime classes** | See [scruff-realtime.md §8](./scruff-realtime.md#8-known-gaps). |

### Recently closed

Open in earlier revisions, now resolved: the error model
([§1.5](#15-errors-client) — it is a custom status-code vocabulary, plus three structured
bodies), TLS pin values, `push_environment` (`"debug"` / `"production"`),
`client_version` composition, `media_behavior`, `album_share_limit`, `thread_key`, the
three Moments target endpoints, `GET /app/socket/poll` semantics, the per-endpoint
`limit` remote-config keys, `block_size`, and the full profile-attribute enum tables
([scruff-enums.md](./scruff-enums.md)).

---

## 16. Platform notes

Not API surface, but useful context for anyone building against this.

**TLS pinning.** The app pins at the **CA root** level, not leaf or intermediate — seven
production pins, of which five are the published Amazon Trust Services roots (consistent
with an AWS-hosted backend). Pins are stored as bare base64 SHA-256 SPKI hashes and the
`sha256/` prefix is added at runtime. A separate three-pin debug array exists for
staging. Because these are CA-level pins, any certificate chaining to Amazon Trust
Services validates. There is no `network_security_config.xml` — pinning is entirely
programmatic. **A third-party client does not need to pin anything.**

**Deep links.** One custom scheme, `scruff://open`, plus 59 verified HTTPS App Links, all
under `https://scruff.com/l/`. The three that carry identifiers and matter for
integration: `/l/profile/*`, `/l/chat/*`, and `/l/magic_link` (passwordless login).
A separate notification namespace exists at
`https://www.scruff.com/notification/{woof,chat,match,message,moment,album,…}/`.

**Bundled third-party identifiers.** All public client keys by design, none of them
secrets: a Google Maps/API key, a Firebase project config (app id, sender id, storage
bucket), a reCAPTCHA **site** key, Branch live and test tokens, a Google Play billing
public verification key, and an AWS Cognito **unauthenticated** identity pool that backs
face-liveness verification. No Stripe publishable key ships in the app — it arrives
server-side in the `register` response as `stripe_public_key`.

**Alternate CDN.** The app exposes a user-facing "use alternate CDN" toggle and ships a
paired `cdn-chat` / `cdn-chat2` host set, alongside the domain-fronting machinery in
[§1.7](#17-domain-fronting-and-failover-observed). The in-app copy is explicit that this
exists because carriers block SCRUFF in some regions.

**React Native.** The app ships a React Native bundle alongside the native code, so some
surface (likely Moments or the store) is RN. It was not analysed here and may contain
endpoint strings not covered by this document.

---

## 16. Minimum viable client

1. Generate and persist `hardware_id` (`droid-` + 16 **lowercase** hex), `device_id`
   (`droid-` + 32 **UPPERCASE** hex),
   `aes256_key` and `aes256_iv` (32 hex each).
2. `POST /app/account/connect` with email, password, your `device_id`, **and the forced
   signature block**. Expect `200` with an empty body.
3. `POST /app/account/register` with `device_id`, unsigned. Parse `socket`, `profile`, and
   the CDN hosts.
4. Send the six identity params on everything — query for `GET`/`DELETE`, form fields for
   `POST`/`PUT` — plus `request_guid` on writes. No signature from here on.
5. `GET /app/inbox/stream` for conversations; page with `cutoff_timestamp`.
6. `GET /app/chat?profile_id=<peer>` per thread. Track the highest `version`; backfill with
   `max_version`.
7. Resolve media with `GET /app/chat/media` and use the signed URL immediately.
8. Connect the realtime socket — see [scruff-realtime.md](./scruff-realtime.md) — and treat
   it as a trigger to re-fetch over REST.
9. Send with `POST /app/chat` (multipart/mixed), `message_type=1`, an UPPERCASE `guid`.
10. Mark read with `POST /app/inbox/view`; respect the client-side rate buckets in
    [§1.4](#14-rate-limiting).
