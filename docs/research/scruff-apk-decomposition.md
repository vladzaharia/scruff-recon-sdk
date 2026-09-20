# Scruff APK — full decomposition

Static decomposition of the Scruff Android APK (jadx). The API layer is in the
non-obfuscated package `com.perrystreet.network.apis` (186 files); DTOs in
`com.perrystreet.dto`, enums in `com.perrystreet.enums`. This is the definitive
API spec; complements `docs/scruff-api.md` (captured traffic) and
`docs/scruff-realtime.md`.

## Host, versioning, signing

- **Base host:** `https://cdn-api.scruffapp.com`, paths under `/app/...` (no `/v1`; profile view uses `api_version=2` query). `@tfh`-marked responses are unwrapped from `{"results": …}`.
- **First-party ("husband") hosts** (get param injection + signing): `cdn-api`, `cdn-profiles`, `cdn-profilemedia`, `cdn-chat`, `cdn-chat2`, `cdn-app`, `cdn-album` (all `.scruffapp.com`).
- **TLS pinning:** OkHttp `CertificatePinner` over `*.scruffapp.com`. Actual SHA-256 pins live in `res/values/arrays.xml` (`root_certificate_hashes`) — not in the code decompile; extract from the APK if reproducing pinning. (Irrelevant for a Go client that just uses normal TLS.)

### Request signing & standard params (StandardParamsInterceptor → `c17`)
Added to first-party requests (query for GET/DELETE, body for POST/PUT):
`hardware_id`, `client_version`, `client_semver` (8.16.0), `flavor` (1), `device_type` (2), `device_id`, and on the signed path `timestamp` + `signature` (+ `latitude`, `longitude`).
- **`signature` = HMAC-SHA256-hex**, key = UTF-8 of `"<longitude>~<latitude>~<device_id>~<device_type>"`, message = `"<that string>:<timestamp>"`, `timestamp` = unix seconds (double).
- **RequestToken** endpoints (`@InjectRequestToken`) add `request_token` = SHA-256-hex of `"<reversed(device_id:device_type)>:<timestamp>"` + `timestamp` + `device_id`.
- **Register/connect/forgot** additionally get (`AccountRegisterParamsInterceptor` + `zj9`): `aes256_key`, `aes256_iv`, `device_name`, `system_name`, `device_os_version`, `system_version`, `locale`, `location_provider`, `register_count(_for_version)`, `domain_fronting_enabled`, `domain_fronting_host`, `build` (169074), `user_agent`.

## Login / session

1. **`POST /app/account/connect`** (form): `request_guid`, `refresh_token` (bool flag), `email`, `password`, `login_token` (magic-link, optional), `old_device_id`, `device_id` → Completable (sets session). Plus standard + register params (incl. `aes256_key`/`aes256_iv`).
2. **`POST /app/account/register`** (form): `request_guid`, `debug`, `apple_store_country_code?`, `remote_configs?` → **`AccountRegisterResponseDTO`**: `profile` (own AccountDTO), **`socket` = {host, port, pwd}** (realtime creds), `cdn`/`app_cdn`/`album_image_cdn`, `device_settings`, `account_tier`, `indicators`, `features`, `favorite_folders`, flags.
3. `POST /app/profile/device_settings` (field `device_settings`). Magic link: `POST /app/account/magic_link` {email, device_id}. Logout: `POST /app/logout`.

Connector persists: `device_id`, `hardware_id`, `aes256_key`, `aes256_iv`, and the returned `socket{host,port,pwd}` + CDNs.

## Messaging

### Send — `POST /app/chat` (multipart, `InboxService`)
Parts: `image` (file, image/jpg), `video` (file, octet-stream), `recipient` (peer id), `guid`, `request_guid` (=guid), `message_type`, `location`, `reaction` (emoji), `reacted_to` (target guid), `message` (text), `album_image_id`, `media_identifier` (always null), `video_filename`, `mute`, `media_behavior` (ordinal: 0 normal, 1 single-view), `reply[guid]`, `album_share_limit`, `shared_album_id`, `reply[moment_id]`. Response `{results:{guid}}`. The send also waits on the socket for a matching delivery event (correlate by guid).

**message_type (`ChatMessageType`):** 0 Unset, 1 Text (`message`), 2 Image (`image` part), 3 Video / 7 HlsVideo (`video`+`video_filename`), 4 Location (`location`), 5 Emoji, 6 Gif (`message` = GIF reference/URL, no file part), 8 Reaction (`reaction`+`reacted_to`), 9 Typing (realtime only), 12 Album (`shared_album_id`+`album_image_id`+`album_share_limit`). Reply: add `reply[guid]` or `reply[moment_id]` to any.

### Fetch / sync (interface `ha8`)
- **`GET /app/chat`**: `profile_id`, `max_version`, `inbox_style`=2, `free_features`, `limit` → JSON `{results:[{message:{...}}], max_read_version, profile_id, min_version, min_version_free, disable_read_receipts}`. Client tracks per-conversation `max_version`; returns messages with version > max_version.
- **`GET /app/inbox/stream`**: `cutoff_timestamp` (epoch seconds), `limit` → inbox list.
- `GET /app/chat/recent`; `GET/POST/DELETE /app/chat/unsent_messages` (drafts).
- **Read receipts:** `POST /app/chat/message_viewed` {version, guid, target_id}; `PUT /app/chat` {profile_id, viewed:true, version, guid}; inbox read `POST /app/inbox/view` {last_message_timestamp}.
- **Unsend (delete):** `PUT /app/chat` {profile_id, unsent:true, version, request_guid, guid, free_features} → `PutChatUnsendDTO`. Delete conversation `DELETE /app/chat` {profile_id}; `DELETE /app/inbox/thread`; `DELETE /app/inbox` {unread_only}.

### Media resolve — `GET /app/chat/media` (`ChatImageService`)
Query: `message_type`, `media_type` (0 image, 1 video), `guid`, `recipient_id`, `sender_id`, `version` → `MediaUrlResponseDTO {image_url, video_url, manifest_url (HLS), manifest_cookies (CloudFront Policy/Signature/Key-Pair-Id/expires_at)}`. Rate-limited (dedicated limiter for `/app/chat/media`).

## Realtime socket (definitive crypto)

- Endpoint from register `socket{host,port,pwd}`. WS `Request`: URL `scheme://host:port`; upgrade headers `X-Device-Id`, `X-Token-Auth` (= socket `pwd`), `X-Request-Id` (per-connection). 5-second ping; reconnect backoff ×2 capped 30s. Poll fallback: `GET /app/socket/poll?request_guids=…`.
- **Decryption (`nu6`/`x9`):** each Base64 text frame → `AES/CBC/PKCS7Padding`, **key = SHA-256(`aes256_key` ASCII) (32 bytes)**, **IV = MD5(`aes256_iv` ASCII) (16 bytes)** → UTF-8 JSON → Moshi `SocketMessageDTO`.
- **Envelope `SocketMessageDTO`:** `{class (int), id, request_guid, timestamp, results (JSON payload)}`. `SocketMessageClassEventType`: Standard (react), CallbackPost/Put/Delete (confirmations of a REST call the client made).
- **Classes (messaging):** 1 Woof, 3 Match, 5 View, 200 ChatMessageDelivered (CallbackPost), 201 ChatMessageReceived (Standard), 202 ChatMessageUnsend (CallbackPut), 203 ChatThreadDelete, 204 ChatInboxDelete, 206 ChatThreadMigrated, 207 ChatMessageUpdated, 208 ChatRecipientTyping, 209 ChatMessageViewed. (Also Album 100–112, Account 400+, Profile 405+, Boost 1300+, Moments 1700+, etc.)

## Other services (all `/app/...`, base cdn-api)

- **account:** `/app/account/devices` (GET/DELETE), `/app/account/sensitive_content_settings_url`, age/faceliveness/sms verification, `/app/account/remote_config`.
- **profile:** `GET /app/profile?target=&api_version=2` (ProfileDTO ~61 fields incl. profile_photos, online/recent, unread, album_images, ratings), `POST /app/profile/note`, `POST /app/profile/view`, photo add/reorder/delete (`/app/profile/photo`), genders/pronouns/hashtags.
- **block:** `POST/DELETE /app/block` (recipient, hide). **favorites:** `/app/favorite` + folders. **match:** `/app/rate`. **poke:** `POST /app/poke` {recipient, moment_id?}.
- **albums:** `/app/albums` (list/create/update/delete), `/app/albums/images` (content GET, multipart POST, PUT/DELETE), `/app/albums/cover_image`, cross-user archive.
- **moments:** `/app/moments*` (feed/nearby/trending/tap_ins), `POST /app/moment` (multipart), view/delete/mute/report.
- **discover:** `/app/discover`, `/app/discover/more`. **events:** `/app/events*`. **films:** `/app/film_festival*`. **boost:** `/app/store/boost`, `/app/boost/latest_stats`. **buildabear (AI):** `/app/ai/*`. **banners:** `/app/banner`.
- **location:** `POST /app/location`, `/app/static_map`. **push:** `POST /app/push` {device_token, push_environment}. **analytics:** `POST /app/logs/event`. **domainfronting:** S3 config + `/ping_ip` + `/ping`.

## Beeper-relevant capabilities (Scruff)
Text, image, video, GIF, location, **reactions (send+recv, type 8)**, **replies (reply[guid], reply[moment_id])**, **unsend/delete (PUT unsent)**, read receipts (message_viewed / class 209), typing (send via type 9 / realtime class 208), unread, presence (online/recent), avatars (cdn-profilemedia), albums (type 12), backfill (max_version), woofs/matches/views (social). No message edit endpoint.
