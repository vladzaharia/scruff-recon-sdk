# AGENTS.md — sdk/scruff

Go client for the SCRUFF app API. Protocol reference:
[`docs/api/scruff.md`](../docs/api/scruff.md).

## Read this before touching the signing code

**The HMAC signature base is `"<lon>~<lat>~<client_version>~<device_type>"`.**

A widely repeated write-up — including an earlier revision of this repository's own
docs, and one of the analysis passes that produced them — puts `device_id` in the third
position. That is **wrong** and produces a signature the server rejects. This was
verified by reading the signing routine directly. `TestSignRequestUsesClientVersionNotDeviceID`
asserts both that we compute the right form and that we do *not* compute the wrong one;
do not delete that second assertion.

Two more details that are easy to get subtly wrong:

- The HMAC **key and the message prefix are the same string**. Unusual, but correct.
- `timestamp` is **decimal seconds with six fraction digits** (`1789866828.155000`), not
  a unix integer.

## The credential model

There are **no tokens and no expiry**. The client generates `device_id`, binds it once
with email and password, and sends it forever. Treat it exactly as a long-lived bearer
token — leaking it is equivalent to leaking the password. `Session` is therefore
security-sensitive in its entirety.

## Other things that will bite

| Trap | Reality |
|---|---|
| `device_id` XOR `signature` | A request carries one or the other — **except `account/connect`, which forces both**. Omitting the signature there fails login. This is the single exception and it is the thing people get stuck on. |
| Conversation ids | **There are none.** A thread is keyed by the peer's `profile_id`, and an inbox conversation's `id` field *is* that peer id. |
| Ordering key | `version`, not `id` and not `created_at`. It is a dense, gap-free per-conversation sequence starting at 1. |
| Message `guid` case | Outbound **UPPERCASE**, inbound **lowercase**. Always use `SameGUID`. A case-sensitive compare duplicates every message you send when it echoes back. |
| Identifier case | `device_id` is **UPPERCASE** hex (203/203 captured), `hardware_id` is **lowercase** (219/219). Same prefix, opposite case. Getting it wrong fingerprints us; `TestIdentifierShapes` asserts both. |
| `GET /app/chat` 404 | Means "no more messages" — the normal backfill terminator, not an error. `Chat` translates it to an empty page. |
| `device_settings` | A **JSON-encoded string**, not an object. Decode it twice. |
| `has_image` | An `int` photo *version*, despite reading like a boolean. |
| Realtime class 208 vs 209 | **208 is typing; 209 is the read receipt.** Several public write-ups have these backwards. |
| Realtime key derivation | `K = SHA-256(aes256_key)` and `IV = MD5(aes256_iv)` hash the **32-character hex strings**, not the bytes they encode. Hex-decoding first silently yields the wrong key. |
| Fixed IV | Derived once per session, not per frame. Cryptographically weak, but it is the protocol. |
| `multipart/mixed` | The chat send uses `multipart/mixed`, so Go's `ParseMultipartForm` (which only handles `multipart/form-data`) will not parse it. Use `MultipartReader`. |
| Multipart part order | Fixed, matching the app: image, video, recipient, guid, request_guid, message_type, then the optional fields. Keeping this order keeps our requests indistinguishable from the real client. |
| Album sharing | Goes through a **chat message of type 12** carrying `shared_album_id`, not a permissions endpoint. `POST /app/albums/permissions` exists server-side but the app never calls it. |
| `POST` vs `GET /app/location` | Completely unrelated. The POST publishes your position; the GET is the nearby profile grid. |
| `limit` | Has **no client-side default** — it comes from server remote config, and omitting it is correct. Advance paging by the response's `block_size`, not by the limit you asked for. |
| `DELETE /app/block` with no params | **Deletes every block.** Guard against sending it accidentally. |

## Errors

There is **no error envelope** — a rejected request has an empty body. SCRUFF instead
defines a custom **420–452 status vocabulary**, and the same code means different things
on different endpoints (`403` alone is "blocked by peer", "album not empty", "Pro cap
reached", and "wrong password"). Interpret status codes **per endpoint**; see
[`docs/api/scruff.md §1.5`](../docs/api/scruff.md#15-errors-client).

Three endpoints do return a parseable body: `430` banned terms, `406` max hashtags, and
the `401` from anonymous register (which is a config payload, not an error).

## Rate limiting

The server **does** rate-limit. No 429 appears in the captures, but one has since been
observed: an opaque `429 Too Many Requests (Rate Limit Exceeded)` with **no `Retry-After`
and no `RateLimit-*` headers**, applied even to the anonymous `register` call — so it is
not per-account, and most likely keyed on source address.

There is therefore no advertised window to obey and no correct backoff to compute.
`DefaultLimiter` mirrors the per-path token buckets the *app* enforces on itself, which is
the best available proxy for "behave like the real client". **Keep it on** unless a test
needs it off, and do not add retry-on-429 — retrying an opaque block just extends it.

## Testing

- `go test ./...` — unit tests, no network.
- `go test -tags integration ./... -v` — staged smoke test.

**Fresh-device login is expected to fail.** Registering a brand-new `device_id` is the
least reliable part of this protocol. The integration test reports that stage as a
failure and then falls back to an imported session via `SCRUFF_DEVICE_ID`, so the rest of
the surface is still exercised. Do not "fix" this by making stage 02 skip silently — the
whole point is that the failure is visible.

## Do not

- Log anything, and especially not `device_id`, `aes256_key`, `socket.pwd`, or the
  `suggested_email` the anonymous bootstrap returns (it is a real Google address).
- Add moderator or administrator surface. `trials/admin_*`, `boost/grant` and the
  verification/anti-fraud flows (`face_liveness`, `sms/send`, `captcha`) are documented
  but deliberately not implemented: they are not things a member does.
- Add bulk or looping helpers over the person-affecting writes. Woofs, favorites, blocks,
  looks and album shares are implemented, but one call per deliberate human action. The
  app enforces its own caps here (`limit_add_favorite_user` 80,
  `limit_hide_block_user_v2` 150) precisely because this surface is abusable.
- Call `UnblockAll`, or send `DELETE /app/block` with no parameters, unless the user
  explicitly asked to clear every block. `Unblock("")` deliberately errors rather than
  falling through to that.
- Exercise any write against a live account casually. The writes here are `[client]`-
  derived and have never been sent to the real API; the unit tests assert the request we
  build, which is the part we actually know.
