# Recon API — complete reference

Unofficial, reverse-engineered reference for the [recon.com](https://www.recon.com) v3
API. Recon publishes no API and no documentation; everything here was derived from the
public Angular web client and from traffic produced by the author's own account.

**Sources.** Static analysis of the production bundle `main.a600a03ce454f4ba.js`
(3.6 MB decompressed; 179 REST call sites + 4 JSON-Patch), plus a mitmproxy capture of a
live web session (279 requests). The mobile apps wrap the same web app, so this surface
is shared. See [`../research/recon-web-decomposition.md`](../research/recon-web-decomposition.md)
for the raw bundle decomposition.

**Companion documents**
- [`recon-realtime.md`](./recon-realtime.md) — the SignalR push channel
- [`recon-enums.md`](./recon-enums.md) — every lookup table and numeric enum
- [`../openapi/recon.yaml`](../openapi/recon.yaml) — machine-readable REST spec

## Provenance tags

Every endpoint carries one of these. **Both `[observed]` and `[client]` are reliable** —
they differ in how the fact was established, not in how much you should trust it. Nothing
here was verified by probing the API; it was never exercised beyond what the web client
did on its own during ordinary use.

| Tag | Meaning |
|---|---|
| **[observed]** | Request *and* response seen on the wire. Authoritative, including which fields are actually populated in practice. |
| **[client]** | Read directly from the official web client's implementation. Paths, methods, parameter names, body shapes, and enum values are authoritative — this is the code that talks to the server. The one limitation is coverage, not accuracy: a field the client never reads will not appear here even if the server sends it. |
| **[unverified]** | Genuinely not determined, either way. Each instance says what was tried; see [§20](#20-known-gaps). |

---

## 1. Transport and conventions

### 1.1 Hosts

| Purpose | Host |
|---|---|
| API gateway | `https://www.recon.com/api/<service>/` |
| Media CDN (public) | `https://media.recon.t101api.com/` |
| Media via gateway (authenticated) | `https://www.recon.com/api/media/` |
| Token issuer (JWT `iss`) | `https://accounts.recon.t101api.com` |
| Token audience (JWT `aud`) | `api.recon.t101api.com` |

The gateway reverse-proxies to a set of `*.recon.t101api.com` microservices. The web
client keeps a registry of per-service base URLs and can have them overridden at runtime
by a `discovery` service (stored in `localStorage.discoBaseUrls`); in the production
build every route resolves under `https://www.recon.com/api/`.

### 1.2 Services

All 18 registered services, with the path segment under `/api/`:

| Registry key | Path segment | Covered in |
|---|---|---|
| `accounts` | `account` | [§4](#4-account) |
| `profiles` | `profile` | [§5](#5-profile) |
| `profileSearch` | `profileSearch` | [§6](#6-profilesearch--the-nearby--explore-grid) |
| `profileRelations` | `profileRelations` | [§7](#7-profilerelations) |
| `messaging` | `messaging` | [§8](#8-messaging) |
| `media` | `media` | [§9](#9-media--galleries) |
| `feed` | `feed` | [§10](#10-feed) |
| `advertising` | `dvrt` | [§11](#11-dvrt--sponsored-content--broadcast-messages) |
| `locations` | `location` | [§12](#12-location) |
| `event` | `event` | [§13](#13-event-client) |
| `membership` | `membership` | [§14](#14-membership-client) |
| `payments` | `payment` | [§15](#15-payment-client) |
| `verification` | `verification` | [§16](#16-verification-client) |
| `pushNotification` | `pushNotification` | [§17](#17-pushnotification-client) |
| `antiAbuse` | `antiAbuse` | [§18](#18-antiabuse-client) |
| `contentReview` | `contentReview` | [§19](#19-contentreview-client) |
| `signalR` | `signalR` | [recon-realtime.md](./recon-realtime.md) |
| `discovery` | `discovery` | Registered but unused in the production build. Runtime base-URL override only. |

### 1.3 Global request shaping

The web client installs two interceptors. A third-party client should replicate the
first; the rest are cosmetic.

1. **`culture` is mandatory.** Every `/api/*` request carries `culture=<lang>`, one of
   `en`, `fr`, `es`, `de`, `pt`. Omitting it is untested.
2. **`deviceTypeId=1`** is appended to media calls and to anything whose URL contains
   `media`.
3. **Query parameters are re-sorted alphabetically** by the client before sending. This
   is why captured URLs look alphabetised. It is not required by the server.
4. **`cache-control: no-cache`** is set on any URL containing `/files/`.
5. **`ngsw-bypass=true`** appears on SignalR URLs to bypass the SPA's service worker.
   Irrelevant outside a browser.

### 1.4 Request headers

| Header | Value |
|---|---|
| `Authorization` | `Bearer <jwt>` — see [§3.4](#34-attaching-the-token-and-the-bearer-bearer-trap) |
| `Content-Type` | `application/json` on JSON bodies; `multipart/form-data; boundary=…` on uploads |
| `Accept` | `application/json` |

**No cookies are required.** The browser client mirrors its tokens into cookies
(`v3_Auth`, `v3_RefreshToken`, `v3_AccountId`, `v3_ProfileIds`, `v3_SessionId`,
`v3_Signalr`, `v3_AccessTokenExpiry`, `v3_RefreshTokenExpiry`) and sends them alongside
the header, but the server authenticates from the `Authorization` header alone. A
non-browser client can ignore cookies entirely.

### 1.5 Response envelopes

Most collection endpoints return:

```jsonc
{ "data": [ /* … */ ], "totalRecords": 0 }
```

⚠️ **`totalRecords` is not always the collection total.** On
`messaging/…/conversations/{cid}/content` it equals the number of records *in the returned
page*. Do not use it for paging arithmetic. The relations endpoints add a
`mostRecentDate` field alongside `data`.

### 1.6 Caching

| Endpoint class | `cache-control` | `etag` |
|---|---|---|
| `appSettings` (anonymous) | `public, max-age=86400` | opaque |
| `appSettings` (account-scoped) | `private, max-age=3600` | opaque |
| Profile lookups / enum tables | `public, max-age=86400` | opaque |
| `profiles/{id}/{version}` | `public, max-age=21600` | `"{version}"` |
| `profileSearch/…/defaultFilters` | `private, max-age=3600` | — |
| Messaging, relations, feed | `no-store, no-cache` | — |
| Media CDN | `public, max-age=2592000, immutable` | `"staticcontent"` |

⚠️ The media `etag` is the **constant literal `"staticcontent"` for every image**. It is
useless for revalidation. Treat media as immutable per `{fileId}` instead.

### 1.7 Rate limiting

**No rate limiting was observed.** No `X-RateLimit-*`, no `RateLimit-*`, and no
`Retry-After` header appears anywhere in the capture, and no 429 was ever returned. The
web client implements no client-side throttle either.

This is an absence of evidence, not a guarantee. A well-behaved client should still pace
itself; this project's Go SDK applies a conservative default limiter.

### 1.8 Errors **[observed]**

The original capture contained no 4xx or 5xx at all, so this was long an open question.
It is now answered: a malformed request produced a real error response, and the envelope
**is** RFC 7807 ProblemDetails — the ASP.NET Core default — extended with Recon's own
fields.

An actual `404` body, verbatim apart from formatting:

```jsonc
{
  "type":     null,
  "title":    "Invalid GUID parameter.",
  "status":   404,
  "detail":   null,
  "instance": null,
  "link":     null,
  "extensions":    { "t101ErrorCode": 901000001 },
  "t101ErrorCode": 901000001
}
```

Notes for a parser:

- `type`, `detail`, and `instance` are the standard ProblemDetails members but were
  `null` here. Do not rely on them being populated.
- **`t101ErrorCode` appears twice** — once nested under `extensions` (where ASP.NET puts
  non-standard members) and once promoted to the top level. Read the top-level copy; fall
  back to `extensions`.
- `link` is a Recon addition, `null` in this response.
- `title` is the only human-readable prose and drives every toast and dialog.
- On validation failures the standard ProblemDetails `errors` member appears as an ASP.NET
  ModelState dictionary, `{"FieldName": ["message", …]}`. The client consumes it in one
  place (change-email), taking the first key's first message.

⚠️ **`title` is sometimes load-bearing, not just cosmetic.** At least one flow matches it
against a literal English string (the age-verification path treats
`"Account has already completed Age verification"` as *success*). Localisation of these
strings would therefore be a breaking change for the vendor's own client — but don't
depend on it either.

⚠️ **`title` is sometimes load-bearing, not just cosmetic.** At least one flow matches it
against a literal English string (the age-verification path treats
`"Account has already completed Age verification"` as *success*). Localisation of these
strings is therefore risky for the server and worth not depending on as a client.

`errors` is only consumed in one place (change-email), which takes the first key's first
message. It appears to be standard ASP.NET model validation.

**Status codes the client branches on:** `401` (refresh token, replay the request **once**),
`403` (paywall, follow limit, profile not found), `404` (not found, cancelled request,
blocked conversation), `500` (one hardcoded retry message). Nothing else is special-cased.

**Complete numeric code list.** These are all the codes present anywhere in the client;
the format is `AAA000BBB`:

| Code | Meaning | Where it surfaces |
|---|---|---|
| `101000050` | Password found in breach corpus | Change password |
| `102000040` | Profile blocked | Declared but never actually compared — dead constant |
| `102000050` | Profile not found | Friend request → redirect |
| `102000061` | Data recently requested | DSAR throttle |
| `103000002` | Your friends limit reached | Friend request |
| `103000009` | Target's friends limit reached | Friend request |
| `103000010` | Daily limit reached | Profile visit → paywall (on `403`) |
| `300000001` | Request not found | DSAR download → link expired |
| `901000001` | Friend request cancelled | Accept friend request (on `404`) |
| `301000005` | You have been blocked | Replying to a conversation (on `404`) |
| `777` | Password found in breach corpus | Change **email** only. Almost certainly a bug — the identical message uses `101000050` on the change-password path. Handle both. |

ℹ️ One endpoint breaks the pattern entirely. `POST account/accounts/passwordValidation`
returns **`200` with a failure payload**: `{isValid: bool, errors: [{description: string}]}`
— an *array of objects*, unrelated to the `errors` dictionary above. Do not reuse your
error parser there.

Rate-limit style errors carry a templated `title`: the cruise throttle returns a `title`
containing a literal `{cruiseMinUpdatePeriod}` token that the client substitutes from
`appSettings.limits.cruiseMinUpdatePeriodDays` before display.

### 1.9 CORS

Responses set `access-control-expose-headers: Authorization, RefreshToken, Location`,
implying a silent token-refresh path via response headers. **No response in the capture
ever carried `Authorization` or `RefreshToken`**, so that path appears unused.
`Location` *is* used — see [§8.4](#84-create-a-conversation-client).

---

## 2. Identifiers

| Id | Shape | Role |
|---|---|---|
| `accountId` | UUID | Login and billing identity. Used by `account`, `verification`, `membership`. |
| `profileId` | UUID | Public persona. **All messaging, media, search, and relations are keyed by this.** |
| `sessionId` | UUID | Issued at login; part of the refresh and logout paths. |
| `appInstallationId` | UUID | Per-install identity, created before login. |
| `conversationId` | UUID | |
| `mediaMetadataId` / `fileId` | UUID | |

One account may own several profiles (`profileIds[]` in the auth response). Exactly one
is active, named in the JWT's `profile_id` claim. The web client takes `profileIds[0]`
and offers no profile picker.

---

## 3. Authentication

Plain email + password yielding a self-issued RS256 JWT. No OAuth, no Firebase, no
App Check, no API key. `appSettings.enabledFeatures.recaptcha` exists but reCAPTCHA was
not required on the captured login. MFA exists as a response flag but was not exercised.

### 3.1 Register an app installation **[observed]**

```http
POST /api/account/appInstallations?culture=en
Content-Type: application/json
```
```json
{
  "applicationId": 3,
  "deviceTypeId": 1,
  "appVersion": "1.7.31",
  "userAgent": "<your UA string>",
  "description": "",
  "isInstalledPwa": false
}
```

⚠️ **`applicationId` must be the integer enum `3`** (the Recon web/v3 client). Sending a
UUID makes the server reject the entire model with an `apiModel required` error. This is
the single most common way to get stuck on first contact.

`201 Created`, `Location: /appInstallations/{id}`:

```jsonc
{
  "id": "<uuid>",              // ← this is the appInstallationId
  "createdDate": "<iso8601>",
  "lastUsedDate": "<iso8601>",
  "applicationId": 3,
  "deviceTypeId": 1,
  "appVersion": "1.7.31",
  "userAgent": "…",
  "description": "",
  "isInstalledPwa": false
}
```

Unauthenticated. `PUT /api/account/appInstallations/{id}` updates it **[client]**.

### 3.2 Authenticate **[observed]**

```http
POST /api/account/accounts/authenticate?culture=en
Content-Type: application/json
```
```json
{
  "emailAddress": "…",
  "password": "…",
  "deviceTypeId": 1,
  "appVersion": "1.7.31",
  "userAgent": "…",
  "appInstallationId": "<uuid from §3.1>"
}
```

`200 OK`:

```jsonc
{
  "accessToken": "Bearer eyJ…",   // ⚠️ ALREADY prefixed — see §3.4
  "accessTokenExpiry": "<iso8601>",   // +15 minutes
  "refreshToken": "<opaque, ~84 chars>",
  "refreshTokenExpiry": "<iso8601>",  // +14 days
  "accountId": "<uuid>",
  "sessionId": "<uuid>",
  "profileIds": ["<uuid>"],       // ⚠️ plural array, even though one is active
  "membershipLevelId": 1,         // 0 = free/official, ≥1 = premium
  "signalRToken": "<base64>",     // "<profileId>.<expUnix>.<hmac>" — UNUSED, see below
  "roles": [],
  "mfaEnabled": false
}
```

`userAgent` is truncated to 255 characters by the client and is recorded server-side as
the device description — it shows up in the account's device list, so pick something
honest.

ℹ️ **`signalRToken` is a red herring.** It is returned but the web client never uses it;
SignalR authenticates with the plain access token. See
[recon-realtime.md](./recon-realtime.md).

### 3.3 JWT claims **[observed]**

RS256. Decoded payload:

| Claim | Example |
|---|---|
| `email` | account email |
| `account_id` | UUID |
| `profile_id` | UUID of the **active** profile |
| `country_code` | ISO-2 |
| `membership_level` | int |
| `device_type` | `"Web"` |
| `age_verified_or_exempt` | bool |
| `nbf` / `iat` / `exp` | `exp - iat == 900` (15 minutes) |
| `iss` | `https://accounts.recon.t101api.com` |
| `aud` | `api.recon.t101api.com` |

### 3.4 Attaching the token, and the `Bearer Bearer` trap

```http
Authorization: Bearer <jwt>
```

⚠️ **This is the single worst trap in the API.** The `accessToken` value returned by
`authenticate` and `refreshTokens` **already includes the `"Bearer "` prefix**. If you
naively do `"Bearer " + resp.accessToken` you produce `Bearer Bearer eyJ…`, and then:

- the **`profile`** service accepts it — so your first test looks like it works;
- the **`messaging`** and **`signalR`** services reject it with an **empty-body 401**.

Strip the prefix on receipt and re-add exactly one. SignalR's `?access_token=` query
parameter needs the **bare** JWT with no prefix at all, while the `JoinConversations`
hub argument wants it **with** the prefix — see
[recon-realtime.md](./recon-realtime.md).

### 3.5 Refresh **[client]** — not yet observed

```http
POST /api/account/accounts/{accountId}/sessions/{sessionId}/refreshTokens?culture=en
Content-Type: application/json
```
```json
{ "refreshToken": "<token>" }
```

Returns the same DTO as [§3.2](#32-authenticate-observed).

⚠️ **Never seen on the wire.** The captured session re-authenticated from scratch rather
than refreshing, so this path is read from the bundle only. Two details that are
confirmed from the bundle:

- The interceptor **skips** adding `Authorization` for any URL containing
  `/refreshTokens`, so this call is sent **unauthenticated**.
- Refresh is pre-emptive, triggered when the token is "nearly invalid" with
  `jwtGracePeriodInMinutes: 3`.

`refreshTokenExpiry` is a real 14-day bound; once it passes, re-authenticate.

### 3.6 Logout **[client]**

```http
DELETE /api/account/accounts/{accountId}/sessions/{sessionId}?culture=en
```

Discarding tokens client-side leaves the server session alive. Call this to end it.

---

## 4. `account`

Base `https://www.recon.com/api/account/`.

### 4.1 `GET appSettings` **[observed]** — unauthenticated

```http
GET /api/account/appSettings?culture=en&deviceTypeId=1
```

**This is the most useful single endpoint in the API.** It is the authoritative source of
every server-side limit, and a client that hardcodes limits instead of reading them will
be wrong. Full response:

```jsonc
{
  "environment": "",
  "ttlMinutes": 3600,
  "enabledFeatures": { "geolocation": true, "pushNotification": true, "recaptcha": true },
  "limits": {
    "interests": 10,
    "blocks": 50,
    "friends": 500,
    "following": 500,
    "minImageResolution": 400,
    "minPasswordLength": 8,
    "minimumZxcvbnScore": 3,
    "maxFileSizeBytes": 52428800,          // 50 MiB
    "maxFileAttachments": 10,
    "allowedFileExtensions": [".bmp", ".gif", ".jpg", ".jpeg", ".png"],
    "allowedMediaMimeTypes": ["image/bmp", "image/gif", "image/jpeg", "image/png"],
    "maxMessageLength": 1000,
    "minAge": 18,
    "maxSearchAge": 99,
    "visitRetentionPeriodDays": 30,
    "cruiseRetentionPeriodDays": 30,
    "standardMemberVisitDisplayPeriodDays": 7,
    "standardMemberCruiseDisplayPeriodDays": 7,
    "cruiseMinUpdatePeriodDays": 7,
    "defaultProfileSearchActiveWithinMinutes": 43200,
    "profileSearchIsOnlineNowActiveWithinMinutes": 15,
    "profileSearchIsNewMemberJoinedWithinDays": 30,
    "maximumFilesPerProfile": 500,
    "maximumFilesInMainGallery": 5,
    "maximumUploadsInBatch": 50,
    "contentStatsBatchCountLimit": 10,
    "contentStatsBatchTimeLimitSeconds": 600,
    "minimumDistanceDisplayedMetres": 500,
    "minimumDistanceDisplayedFeet": 1500
  },
  "geolocation": {
    "minUpdateIntervalMinutes": 2,
    "maxUpdateIntervalMinutes": 15,
    "significantChangeDistanceMetres": 99,
    "searchSettingsMetric":   { "distanceSliderValues": [1,2,3,4,5,10,15,25,50,150,400,800], "defaultDistance": 0 },
    "searchSettingsImperial": { "distanceSliderValues": [1,2,3,4,5,10,15,25,50,100,250,500], "defaultDistance": 0 }
  },
  "siteUrls": {
    "recon": "…", "deepLinksRoot": "…", "newSupportRequest": "…", "privacyNotice": "…",
    "profileDistanceUrl": "…<lowerLocationId>/…<higherLocationId>",
    "terms": "…", "membership": "…", "iosAppDownload": "…", "supportHome": "…",
    "helpArticles": {
      "BlockingAnotherMember": "…", "Deactivation": "…", "FavouritesMigration": "…",
      "PasswordSecurity": "…", "PhotoClassification": "…", "VerificationHelp": "…"
    },
    "passwordSecurity": "…", "photoClassification": "…", "verificationHelp": "…", "feedback": "…"
  },
  "socialUrls": {
    "facebook": "…", "instagram": "…", "twitter": "…", "blueSky": "…", "reconVideo": "…",
    "podcast": { "spotify": "…", "spread": "…", "soundcloud": "…", "apple": "…" }
  },
  "googleAnalyticsPropertyId": "…",
  "supportedLanguages": ["en", "de", "es", "fr", "pt"],
  "accountPreferences": {
    "culture": "en-GB", "locale": "en-GB",
    "showMetricDistance": true, "showMetricHeight": true,
    "gmtOffsetSeconds": null, "countryCode2": null
  },
  "pilotUrls": { "feedbackUrl": "…", "bugReportUrl": "…", "pilotFaqsUrl": "…" },
  "iosVersion": null,
  "webVersion": { "isDeprecated": false, "isBlocked": false },
  "isAdminMfaRequired": false,
  "verification": { "helpUrl": "…", "verificationPendingPollingPeriodSeconds": 15 },
  "sponsoredContent": { "primaryAlertPosition": 0, "standardSecondaryInterval": 0, "premiumSecondaryInterval": 0 }
}
```

`distanceSliderValues` are in km (metric) / miles (imperial), and are what
`distanceSliderIndex` indexes into for search — see
[§6.2](#62-the-search-filter-model-client).

### 4.2 `GET accounts/{accountId}/appSettings` **[observed]**

```http
GET /api/account/accounts/{accountId}/appSettings?culture=en&deviceTypeId=1
```

Identical schema, personalised. Two deltas from the anonymous form:

- `accountPreferences` reflects the account (e.g. `showMetricDistance: false`,
  `gmtOffsetSeconds: -25200`, `countryCode2: "us"`).
- `siteUrls.profileDistanceUrl` becomes the gateway-proxied
  `https://www.recon.com/api/location/granularLocations/<lowerLocationId>/distances/<higherLocationId>`
  instead of the direct `https://location.recon.t101api.com/…`.

### 4.3 Preferences **[observed]** / **[client]**

```http
GET /api/account/accounts/{accountId}/preferences?culture=en&deviceTypeId=1   [observed]
PUT /api/account/accounts/{accountId}/preferences?culture=en                  [client]
```
```jsonc
{
  "culture": "en-GB", "locale": "en-GB",
  "showMetricDistance": false, "showMetricHeight": true,
  "gmtOffsetSeconds": -25200, "countryCode2": "us"
}
```

### 4.4 Registration and lifecycle **[client]**

#### Registration — a three-call flow

**1.** `POST accounts/register` (unauthenticated)

```jsonc
{
  "emailAddress": "…",
  "confirmedOver18": true,
  "acceptedTermsAndConditions": true,
  "acceptedPrivacyNotice": true,
  "acceptedMemberCodeOfConduct": true,
  "appInstallationId": "<uuid>"
}
```

ℹ️ `acceptedMemberCodeOfConduct` is effectively hardcoded `true` — the client assigns it
from itself, so it always ships the constructor default regardless of user input. That
looks like a copy-paste bug, but it means you should just send `true`.

Failure is signalled by the presence of `error.title` rather than by a distinct shape.

**2.** `POST accounts/passwordValidation` (unauthenticated) — `{emailAddress, password}`

⚠️ Returns **`200` even on failure**, with a shape unrelated to the normal error
envelope: `{isValid: bool, errors: [{description: string}]}`.

**3.** `POST accounts/register/complete` (unauthenticated)

```jsonc
{
  "emailVerificationId": "<requestId from the emailed link>",
  "verificationCode":    "<token from the emailed link>",
  "password":            "…",
  "profile": { "name": "…", "dateOfBirth": "…", "interests": [1, 5] }
}
```

On success this returns a **full auth payload** — the same DTO as
[§3.2](#32-authenticate-observed). You are logged in directly from registration.

Supporting calls: `POST emailConfirmations/{id}/validate` and
`POST emailConfirmations/{id}/confirmEmailAddress`, both `{verificationCode}`; username
availability via `POST antiAbuse/profileNameChecks` → `{isValid, errorMessage}`.

The client's registration step sequence, for reference: Start 0, Email 1, CheckInbox 2,
ConfirmEmail 3, LinkExpired 4, Password 5, BirthDate 6, Name 7, Interests 8, Photo 9,
Welcome 10.

#### Password and email

```jsonc
POST accounts/{accountId}/changePassword      { "previousPassword": "…", "password": "…" }
POST accounts/{accountId}/changeEmailAddress  { "emailAddress": "…", "currentPassword": "…" }
POST accounts/generatePasswordResetLink       { "emailAddress": "…" }        ○
POST accounts/{accountId}/resetPassword       { "token": "…", "password": "…" }  ○
GET  accounts/{accountId}/passwordResets/{urlEncodedToken}/emailAddress          ○
```

⚠️ **Inconsistent field naming:** the old password is `previousPassword` when changing
the password, but `currentPassword` when changing the email.

`password` is validated client-side against `limits.minPasswordLength` and
`limits.minimumZxcvbnScore`. After a successful change the client force-logs-out.

#### GDPR data subject access requests

```
POST accounts/{accountId}/dataSubjectAccessRequests                  body: null
POST accounts/{accountId}/dataSubjectAccessRequest/resendEmail       body: null
POST accounts/{accountId}/dataSubjectAccessRequests/{token}/download { accountId, password }
GET  accounts/{accountId}/dataSubjectAccessRequestStatus
```

⚠️ The first two take a literal `null` body — the account id travels in the path only.
Only the download step authenticates with a password in the body.

Errors: `t101ErrorCode` `102000061` → already requested, `300000001` → link expired.

#### Everything else

Unauthenticated endpoints are marked ○.

| Method | Path | Body | Notes |
|---|---|---|---|
| ○ POST | `accounts/register` | registration payload | Field list not traced (reactive form in a lazy chunk) |
| ○ POST | `accounts/register/complete` | payload | |
| ○ POST | `accounts/passwordValidation` | `{emailAddress, password}` | Rejects breached passwords (`101000050`) |
| ○ POST | `accounts/generatePasswordResetLink` | `{emailAddress}` | |
| ○ POST | `accounts/{accountId}/resetPassword` | `{token, password}` | |
| ○ GET | `accounts/{accountId}/passwordResets/{urlEncodedToken}/emailAddress` | — | → `{emailAddress}` |
| ○ POST | `emailConfirmations/{token}/validate` | `{verificationCode}` | |
| ○ POST | `emailConfirmations/{token}/confirmEmailAddress` | `{verificationCode}` | → `true` |
| POST | `emailConfirmation/{token}/confirmEmailAddress` | payload | ⚠️ **singular** `emailConfirmation`. Both spellings exist in the bundle; this one looks like a latent bug. |
| POST | `accounts/{accountId}/changePassword` | password payload | |
| POST | `accounts/{accountId}/changeEmailAddress` | payload | echoes back |
| PUT | `accounts/{accountId}/verificationStats` | `null` | |
| POST | `accounts/{accountId}/deactivate` | `null` | Reversible |
| POST | `accounts/{accountId}/delete` | payload | Permanent |
| POST | `accounts/{accountId}/dataSubjectAccessRequests` | `null` | GDPR DSAR |
| POST | `accounts/{accountId}/dataSubjectAccessRequest/resendEmail` | `null` | |
| POST | `accounts/{accountId}/dataSubjectAccessRequests/{requestId}/download` | payload | |
| GET | `accounts/{accountId}/dataSubjectAccessRequestStatus` | — | |

---

## 5. `profile`

Base `https://www.recon.com/api/profile/`.

### 5.1 Fetch a profile **[observed]**

```http
GET /api/profile/profiles/{profileId}/{version}?culture=en&imageSizes=102,104
```

The trailing path segment is the **profile version number**, not an image size.
Requesting `/1` on a profile whose current version is higher returns `302 Found`
redirecting to `/{currentVersion}`, so `/1` is the idiomatic "give me current". Follow
redirects.

⚠️ A widespread misreading: a URL like `/profiles/{id}/661` means *version 661*. `661`
is **not** an image size — see [§5.4](#54-image-sizes).

```jsonc
{
  "detailUrl": ".../profiles/{id}/detail/{version}",  // null on official profiles
  "primaryImageFiles": [
    { "imageSize": 100, "url": "https://media.recon.t101api.com/profiles/{id}/files/{fileId}.jpg?size=100" }
    // one entry per size requested via imageSizes; [] when the profile has no avatar
  ],
  "membershipLevelId": 1,          // 0 on official profiles
  "id": "<uuid>",
  "version": 661,                  // monotonic; also the ETag
  "name": "<display name>",
  "dateOfBirth": null,             // always null over the wire
  "age": 35,                       // 0 on official profiles
  "heightCm": 170,
  "shortText": "<headline>",
  "longText": null,                // always null here; lives on detailUrl
  "ethnicityId": 9,
  "positionId": 2,
  "bodyTypeId": 4,
  "bodyHairId": 5,
  "safeSexId": 4,
  "roleId": 2,
  "location": { "locationId": 1729962, "locationUrl": ".../api/location/locations/1729962" },
  "lastUpdatedDate": "<iso8601>",  // "0001-01-01T00:00:00Z" on official profiles
  "interests": [31, 15],           // enum ids, max 10
  "createdDate": "<iso8601>",
  "isPublic": true,
  "isOfficial": false,
  "rowVersion": null               // always null on read; required on PATCH
}
```

All the `*Id` fields resolve against the lookup tables in
[recon-enums.md](./recon-enums.md).

Variants **[client]**:

```http
GET profiles/{profileId}?noCache=true
GET profiles/{profileId}?includeAllProperties=true&noCache=true
GET profiles/{profileId}/detail/{version}[?noCache=true]
GET officialProfiles/{profileId}                  # [observed] — system/blog personas
GET profiles/name/{profileName}                   # lookup by profile name
```

#### Dereferencing the embedded URLs

Profile responses carry HATEOAS-style absolute URLs. Two are worth following:

**`detailUrl`** → the long bio, split out so it can be cached and invalidated
independently by `version`. The client caches it for 15 minutes.

```jsonc
{ "id": "<uuid>", "longText": "…", "version": 661 }
```

**`location.locationUrl`** → resolves a `locationId` to display names.

```jsonc
{ "shortLocationName": "…", "longLocationName": "…", "ttlMinutes": 1440 }
```

`ttlMinutes` is the server telling you how long to cache it. If `locationUrl` is absent
the client simply shows no location rather than falling back to anything.

### 5.2 Edit a profile **[client]**

```http
PUT   /api/profile/profiles/{profileId}?culture=en     # full replace
PATCH /api/profile/profiles/{profileId}?culture=en     # RFC-6902 JSON Patch
POST  /api/profile/?culture=en                         # create (ProfileDetail body)
```

JSON-Patch ops accepted: `add`, `copy`, `remove`, `replace`, `test`. The client only ever
sends `replace`, always including `/rowVersion` for optimistic concurrency. The five
patch sets it uses:

| Client action | Paths |
|---|---|
| Edit bio | `/shortText`, `/longText`, `/rowVersion` |
| Rename | `/name` |
| Edit details | `/dateOfBirth`, `/positionId`, `/ethnicityId`, `/heightCm`, `/bodyTypeId`, `/bodyHairId`, `/roleId`, `/rowVersion` |
| Edit interests | `/interests`, `/rowVersion` |
| Toggle public | `/isPublic`, `/rowVersion` |

Note `/safeSexId` is readable but absent from every patch set — the web UI offers no
control for it.

### 5.3 Lookup tables **[observed]**

```http
GET /api/profile/{table}?culture=en
```

`table` ∈ `bodyhairs`, `bodytypes`, `ethnicities`, `interests`, `positions`, `safesexes`,
`roles` → `{ "data": [{ "id": 1, "name": "…" }], "totalRecords": n }`.

`GET /api/profile/validHeightsCm` is the exception: `data` is a **bare int array**, not
objects.

Full values in [recon-enums.md](./recon-enums.md). All are `public, max-age=86400` —
cache them.

### 5.4 Image sizes

`imageSizes` is a CSV of size codes; the server returns one `files[]`/`primaryImageFiles[]`
entry per requested code. On single-file endpoints the parameter is singular: `size=<code>`.

**Only codes 100–104 exist.** The web client selects them by viewport:

| Viewport | thumbnail | large |
|---|---|---|
| Desktop / wide | `102` | `104` |
| Tablet / handset | `101` | `103` |

`100` is hardcoded wherever a fixed small rendition is wanted (gallery lists, conversation
content). Observed payloads: `size=100` ≈ 4–13 kB, `size=102` ≈ 143–162 kB.

⚠️ Pixel dimensions for each code are **not** present anywhere in the bundle. And to
repeat [§5.1](#51-fetch-a-profile-observed): **`661` is not a size**, it is a profile
version.

---

## 6. `profileSearch` — the nearby / explore grid

Base `https://www.recon.com/api/profileSearch/`.

### 6.1 Execute a search **[client]**

```http
GET /api/profileSearch/profiles?culture=en&myProfileId={me}&sortProperty=distance&<filters>
```

| Param | Required | Notes |
|---|---|---|
| `myProfileId` | yes | Your profile UUID |
| `sortProperty` | yes | **Only `distance` is ever sent.** Other accepted values are server-side and unknown. |
| *(filters)* | no | See [§6.2](#62-the-search-filter-model-client) |

⚠️ **Two properties make this endpoint unusual:**

1. **It is not paginated.** There is no `skip`/`take`/cursor. The server returns the
   entire result set and the client slices locally (page size 20). Expect large responses.
2. **Results are stubs.** Each item is only:

   ```jsonc
   { "profileId": "<uuid>", "profileUrl": "…/profiles/{id}/{version}", "distanceMetres": 1234, "date": "<iso8601>" }
   ```

   To render a grid you must then `GET` each `profileUrl` individually — a two-phase
   fetch. The version is embedded in `profileUrl`'s last path segment.

The client also overwrites the response's `totalRecords` with `data.length`.

### 6.2 The search filter model **[client]**

Serialisation rules, from the client's filter→query serialiser:

- Keys are sorted alphabetically; `null`/`undefined` are dropped.
- Arrays are comma-joined.
- **Three keys are client-only and never sent**: `distanceSliderIndex`, `isOnlineNow`,
  `isNewMember`. They are translated into `radiusMetres`, `activeWithinMinutes`, and
  `joinedWithinDays` respectively.

| Query param | Type | Notes |
|---|---|---|
| `name` | string | Name search (also available as `profiles/name/{profileName}`) |
| `ageMin` / `ageMax` | int | `ageMin` defaults to 18; max is `limits.maxSearchAge` (99) |
| `heightCmMin` / `heightCmMax` | int | **premium only** |
| `latitude` / `longitude` | float | |
| `radiusMetres` | int | metric: `1000 × sliderValue`; imperial: `1609 × sliderValue` |
| `activeWithinMinutes` | int | default `43200`; `15` for "online now" |
| `joinedWithinDays` | int | `30` for "new member", else omitted |
| `isPremium` | bool | **premium only** |
| `hasVisibleImages` | bool | **premium only** |
| `isExplore` | bool | `true` for the Explore list |
| `positionIds` | int[] | |
| `roleIds` | int[] | |
| `interestIds` | int[] | |
| `safeSexIds` | int[] | |
| `bodyTypeIds` | int[] | **premium only** |
| `bodyHairIds` | int[] | **premium only** |
| `ethnicityIds` | int[] | **premium only** |

**Premium gating** is enforced client-side before the request: for a free or logged-out
user the client nulls `isPremium`, `isOnlineNow`, `isNewMember`, `joinedWithinDays`,
`hasVisibleImages`, `heightCmMin`, `heightCmMax`, empties `bodyTypeIds`, `bodyHairIds`,
`ethnicityIds`, and forces `activeWithinMinutes` to
`limits.defaultProfileSearchActiveWithinMinutes`. Whether the server also enforces this
is untested.

### 6.3 Saved default filters **[observed]**

```http
GET    /api/profileSearch/profiles/{profileId}/defaultFilters?culture=en
PUT    /api/profileSearch/profiles/{profileId}/defaultFilters?culture=en
DELETE /api/profileSearch/profiles/{profileId}/defaultFilters?culture=en
```

```jsonc
{
  "distanceSliderIndex": 11, "ageMin": null, "ageMax": 70,
  "isPremium": null, "isNewMember": null, "isOnlineNow": null,
  "hasVisibleImages": null, "heightCmMin": null, "heightCmMax": null,
  "positionIds": [5,4,3,2,1,6], "interestIds": [], "bodyTypeIds": [],
  "bodyHairIds": [], "ethnicityIds": [], "safeSexIds": [], "roleIds": [1,2]
}
```

Every scalar is nullable; `null` means "no filter". Note this DTO *does* persist the
client-only `distanceSliderIndex`.

### 6.4 Social graph listings **[client]**

| Method | Path | Query | Response |
|---|---|---|---|
| GET | `profiles/name/{profileName}` | — | `{id}` |
| GET | `profiles/{profileId}/friends` | `myProfileId?` | `{data: {mutual: [], nonMutual: []}}` |
| GET | `profiles/{profileId}/friends/count` | `myProfileId?` | `{mutualCount, otherCount}` |
| GET | `profiles/{profileId}/followers` | `myProfileId?` | `{data: {mutual, nonMutual}}` |
| GET | `profiles/{profileId}/followers/count` | `myProfileId?` | count |
| GET | `profiles/{profileId}/followings` | `myProfileId?`, `noCache=true` when self | `{data: {mutual, nonMutual}}` |
| GET | `profiles/{profileId}/followings/count` | as above | count |
| GET | `events/{eventId}/attendees/count` | — | `{value: int}` |
| GET | `profiles/{profileId}/events/{eventId}/attendees` | — | `{data, totalRecords}` |

⚠️ Naming asymmetry to watch: `followers`/`followings` (plural) live here on
**profileSearch**, while `followers/count` and `following/count` (singular) also exist on
**profileRelations** ([§7](#7-profilerelations)).

---

## 7. `profileRelations`

Base `https://www.recon.com/api/profileRelations/`. All list endpoints are
`no-store, no-cache` and take no paging parameters — one observed call returned all 113
records in a single 6.4 kB response.

Recon's social primitives: **block**, **cruise** (a like/favourite), **visit** (a profile
view), **friend** (mutual, request-based), and **follow** (one-way).

### 7.1 Blocks

| Method | Path | Body | Tag |
|---|---|---|---|
| PUT | `profiles/{me}/blocks/{targetProfileId}` | Block DTO | [client] |
| DELETE | `profiles/{me}/blocks/{targetProfileId}` | — | [client] |
| GET | `profiles/{me}/blocked` | — | [observed] |

```jsonc
// GET blocked
{ "data": [ { "profileName": "…", "profileId": "<uuid>", "profileUrl": "…" } ], "totalRecords": 12 }
```

This is the only relations endpoint that embeds `profileName`; the cruise and visit lists
give you a URL to dereference instead. Limit: `appSettings.limits.blocks` (50).

### 7.2 Cruises

| Method | Path | Body | Tag |
|---|---|---|---|
| PUT | `profiles/{me}/cruisedProfiles/{targetProfileId}` | `null` | [client] |
| DELETE | `profiles/{me}/cruisedProfiles/{targetProfileId}` | — | [client] |
| GET | `profiles/{me}/cruisedProfiles` | — | [observed] |
| GET | `profiles/{me}/cruisedByProfiles` | — | [observed] |
| GET | `profiles/{me}/cruisedByProfiles/count` | — | [observed] |
| POST | `profiles/{me}/logCruisedByViewed` | `{viewedDate}` | [client] |

```jsonc
// GET cruisedProfiles / cruisedByProfiles — identical shape
{
  "mostRecentDate": "<iso8601>",
  "data": [ { "date": "<iso8601>", "profileId": "<uuid>", "profileUrl": "…" } ],
  "totalRecords": 113
}
```

```jsonc
// GET .../count
{ "withinStandardDisplayPeriod": 3,   // 7-day window  (limits.standardMemberCruiseDisplayPeriodDays)
  "withinPremiumDisplayPeriod": 11,   // 30-day window (limits.cruiseRetentionPeriodDays)
  "sinceLastViewed": 2 }
```

Re-cruising the same profile is throttled by `limits.cruiseMinUpdatePeriodDays` (7); the
rejection body carries a `title` containing a `{cruiseMinUpdatePeriod}` token.

### 7.3 Visits

| Method | Path | Body | Tag |
|---|---|---|---|
| PUT | `profiles/{me}/visitedProfiles/{targetProfileId}` | `null`; `?source=2` when arriving from v2 | [client] |
| GET | `profiles/{me}/visitorProfiles` | — | [client] |
| GET | `profiles/{me}/visitorProfiles/count` | — | [observed] |
| POST | `profiles/{me}/logVisitorsViewed` | `{viewedDate}` | [client] |
| GET | `profiles/{me}/listViewStats` | — | [client] — see below |

Same envelopes as cruises. Retention: `limits.visitRetentionPeriodDays` (30), standard-tier
display window 7 days.

**`listViewStats`** returns the "last viewed" watermarks used to highlight unseen entries
in the cruise and visitor lists:

```jsonc
{ "cruisedByLastViewedDate": "<iso8601|null>", "visitorsLastViewedDate": "<iso8601|null>" }
```

Compare each list entry's `date` against the matching watermark; newer (or a `null`
watermark) means unseen. The write side is `logCruisedByViewed` / `logVisitorsViewed`,
whose `{viewedDate}` the client takes from the list response's `mostRecentDate`.

ℹ️ The client reads only these two fields, so the response may carry more. Notifications
are **not** covered here — they use the separate `feed/…/logNotificationsViewed`.

⚠️ Visits are suppressed when the viewer has **stealth mode** on — which is the
`showVisits: false` preference in [§7.6](#76-relation-preferences-client).

### 7.4 Friends **[client]**

| Method | Path | Body |
|---|---|---|
| PUT | `profiles/{profileId}/friendRequests/{targetProfileId}` | `null` |
| DELETE | `profiles/{profileId}/friendRequests/{targetProfileId}` | — |
| GET | `profiles/{profileId}/friendRequests` | — → `{total, data: [FriendRequest]}` |
| POST | `profiles/{profileId}/friendRequestResponse/{targetProfileId}/accept` | `null` |
| POST | `profiles/{profileId}/friendRequestResponse/{targetProfileId}/reject` | `null` |
| DELETE | `profiles/{profileId}/friends/{targetProfileId}` | — |

```jsonc
// FriendRequest
{ "requestingProfileId": "<uuid>", "targetProfileId": "<uuid>", "requestDate": "<iso8601>",
  "requestingProfileUrl": "…", "targetProfileUrl": "…" }
```

Limit `limits.friends` (500); errors `103000002` (your limit) / `103000009` (theirs).

### 7.5 Followers **[client]**

| Method | Path |
|---|---|
| PUT | `profiles/{profileId}/followers/{targetProfileId}` |
| DELETE | `profiles/{profileId}/followers/{targetProfileId}` |
| GET | `profiles/{profileId}/followers/count` → `{count}` |
| GET | `profiles/{profileId}/following/count` → `{count}` |

The `DELETE` serves double duty — the client calls it both to unfollow and to remove a
follower. Limit `limits.following` (500).

### 7.6 Relation preferences **[client]**

```http
GET   profiles/{me}/preferences
PUT   profiles/{me}/preferences
PATCH profiles/{me}/preferences     # e.g. [{"op":"replace","path":"/showVisits","value":false}]
```
```jsonc
{ "profileId": "<uuid>", "showVisits": true,
  "cruiseEmail": true, "cruisePushNotification": true,
  "friendRequestEmail": true, "friendRequestPushNotification": true,
  "followerPushNotification": true }
```

`showVisits: false` **is** stealth mode.

### 7.7 Legacy favourites migration **[client]**

```http
GET  v2Favourites/{profileId}/canImport    → {value: bool}
POST v2Favourites/{profileId}/import       → true
```

---

## 8. `messaging`

Base `https://www.recon.com/api/messaging/`. Everything is keyed by **your** `profileId`,
which appears in the path.

**Send is REST.** The SignalR hub exposes a `SendChatMessage` method, but it is dead code
in the web client — sending goes over REST and SignalR is receive-plus-typing only.

**Recon has no reactions, no message edits, and no per-message deletes.** Only whole
conversations and individual attachments can be deleted. Bridges should map those
capabilities to no-ops.

### 8.1 List conversations **[observed]**

```http
GET /api/messaging/profiles/{me}/conversations?culture=en&isHidden=false
```

Unpaged — one observed response returned all 873 conversations in 188 kB.

```jsonc
{
  "data": [{
    "id": "<uuid>",
    "name": null,                       // null for 1:1
    "isGroup": false,
    "isOfficial": false,                // true for system/blog threads
    "isArchived": false,
    "thumbnailMediaMetadataId": "",     // "" in every observed record
    "thumbnailUrl": null,
    "lastActivityDate": "<iso8601>",
    "participantCount": 2,
    "participants": [ { "profileId": "<uuid>", "profileUrl": "…" } ],
    "createdDate": "<iso8601>",
    "lastMessageExcerpt": "<plain-text preview>",
    "contentUrl": "…/conversations/{id}/content",
    "participantsUrl": "…/conversations/{id}/participants",
    "userInfo": { "isFlagged": false, "unreadMessageCount": 0 },
    "isReadOnly": false                 // true exactly for isOfficial threads
  }],
  "totalRecords": 873
}
```

⚠️ **`participants` excludes you** — its length is `participantCount - 1`. For a 1:1 that
means exactly one entry, the peer.

The client derives "hidden" as `!lastActivityDate`.

### 8.2 Fetch message history **[observed]**

```http
GET /api/messaging/profiles/{me}/conversations/{cid}/content
      ?culture=en&imageSizes=100,104&take=20&before=<iso8601>
```

| Param | Notes |
|---|---|
| `take` | Page size. Web client uses 20. |
| `imageSizes` | CSV of size codes to hydrate attachment URLs at |
| `before` | **Pagination cursor** — omit for the first page |

⚠️ **Pagination is a `before` cursor, not `skip`/`offset`.** The value is the
**`createdDate` (ISO 8601) of the oldest message you already hold** — not an id, not an
index. An empty `data` array means end of history.

⚠️ **`totalRecords` here is the page size, not the conversation total.** A 10-record page
reports `totalRecords: 10`. Do not use it for paging arithmetic.

Results are ordered **newest first**.

```jsonc
{
  "data": [{
    "id": "<uuid>",
    "senderProfileId": "<uuid>",   // ⚠️ REST name; SignalR calls this `profileId`
    "text": "…",
    "isFlagged": false,
    "attachments": [],
    "isRead": true,
    "displayAsHtml": false,
    "markupTypeId": null,          // official-thread rendering hint; null everywhere observed
    "createdDate": "<iso8601>",
    "contentTypeId": 1             // ⚠️ 1 = text. This is the real field — NOT `messageType`
  }],
  "totalRecords": 10
}
```

⚠️ **`contentTypeId` vs `messageType`.** The wire field is **`contentTypeId`**. The web
client's mapper reads a `messageType` property, but that is only ever populated on
locally-constructed outgoing messages — no server response in the capture contains it. A
client that unmarshals `messageType` will silently get zero for every message.

⚠️ **`senderProfileId` vs `profileId`.** REST uses `senderProfileId`. The SignalR
`ReceiveMessage` event uses **`profileId`** for the same concept, and `messageText`
instead of `text`. The two transports genuinely disagree; see
[recon-realtime.md](./recon-realtime.md).

**Attachment element** — every observed message had `attachments: []`, so this shape is
**[client]**-derived:

```jsonc
{
  "id": "<uuid>",
  "mediaMetadataId": "<uuid>",
  "thumbnailUrl": "…",
  "files": [ { "imageSize": 100, "url": "…" } ],   // ordered small → large
  "downloadUrl": "…",
  "isRestricted": false
}
```

### 8.3 Single message **[client]**

```http
GET /api/messaging/profiles/{me}/conversations/{cid}/messages/{messageId}?culture=en&imageSizes=100,104
```

Returns one bare message object. Its main use is hydrating a SignalR `ReceiveMessage`
event when `attachmentCount > 0`, since the realtime payload omits attachments.

### 8.4 Create a conversation **[client]**

```http
POST /api/messaging/profiles/{me}/conversations?culture=en
```
```jsonc
{
  "participants": [ { "ProfileId": "<other>" }, { "ProfileId": "<me>" } ],
  "isGroup": false, "isOfficial": false, "isArchived": false
}
```

⚠️ **`ProfileId` is PascalCase here**, while the surrounding keys are camelCase. This and
`LastMessageReadDate` ([§8.7](#87-mark-read-observed)) are the only two PascalCase bodies
in the entire API.

**Conflict handling:** if the conversation already exists the server responds `409` (or a
3xx) with a `Location` header. Follow it to retrieve the existing conversation. That
header is explicitly listed in `access-control-expose-headers`.

Alternatively, look it up directly **[client]**:

```http
GET /api/messaging/profiles/{me}/conversations?culture=en&profileId={me}&otherProfileId={other}
```

Note `{me}` appears twice — in the path *and* as a query parameter. Returns the standard
collection envelope; an empty `data` means "no conversation yet", which is not an error.

### 8.5 Send a message **[client]**

Three forms exist.

**Text (JSON):**

```http
POST /api/messaging/profiles/{me}/conversations/{cid}/messages?culture=en
Content-Type: application/json
```
```json
{ "text": "hello", "files": [], "attachments": [] }
```

`files` and `attachments` are always serialised as empty arrays — present, never omitted.
There is **no `contentTypeId`** in the request; the server assigns `id`, `contentTypeId`,
and `senderProfileId`.

**Inline media (multipart)** — the path the web UI actually uses for images and GIFs:

```http
POST /api/messaging/profiles/{me}/conversations/{cid}/messages?culture=en&imageSizes=100,104
Content-Type: multipart/form-data; boundary=…
```

| Part | Type | Notes |
|---|---|---|
| `text` | string | Caption; may be empty |
| `attachments` | string | A **JSON-encoded string array** of pre-uploaded media ids. Usually `[]`. Always written. |
| `files` | file, **repeated** | One part per binary. Set `filename` and a `Content-Type` of `image/jpeg`, `image/png`, `image/gif`, or `image/bmp`. |

The response is a message with `attachments[]` populated. GIFs go through this same path —
there is no Tenor/Giphy integration.

**By reference (JSON):** for media already uploaded to the `media` service:

```json
{ "text": "…", "files": [], "attachmentIds": ["<mediaFileId>"] }
```

Note the key is `attachmentIds` here, not `attachments`.

Constraints come from `appSettings.limits`: `maxMessageLength` 1000,
`maxFileAttachments` 10, `maxFileSizeBytes` 52428800, `allowedMediaMimeTypes`.

### 8.6 Delete **[client]**

```http
DELETE /api/messaging/profiles/{me}/conversations/{cid}?culture=en
DELETE /api/messaging/profiles/{me}/conversations/{cid}/attachments/{attachmentId}?culture=en
```

Whole conversation, or a single attachment. **No per-message delete exists.**

### 8.7 Mark read **[observed]**

```http
POST /api/messaging/profiles/{me}/conversations/{cid}/logMessagesRead?culture=en
Content-Type: application/json
```
```json
{ "LastMessageReadDate": "2026-09-13T18:06:37.76Z" }
```

⚠️ **PascalCase key.** Returns `204 No Content` with an empty body.

### 8.8 Recent attachments **[observed]**

```http
GET /api/messaging/profiles/{me}/recentAttachments?culture=en&imageSizes=102,104&nocache=<epoch-ms>
```
```jsonc
{ "data": [ { "id": "<mediaMetadataId>", "files": [ { "imageSize": 102, "url": "…" } ] } ],
  "totalRecords": 24 }
```

⚠️ URLs here point at the **gateway** (`https://www.recon.com/api/media/…`), not the
public CDN — chat media is auth-gated. Note also the lowercase `nocache` parameter, which
differs from the `noCache` used elsewhere.

### 8.9 Messaging preferences and contacts **[client]**

```http
GET /api/messaging/profiles/{me}/contacts?culture=en
GET /api/messaging/profiles/{me}/preferences?culture=en
PUT /api/messaging/profiles/{me}/preferences?culture=en
```
```jsonc
{ "profileId": "<uuid>", "messageEmail": true, "messagePushNotification": true }
```

---

## 9. `media` — galleries

Base `https://www.recon.com/api/media/`. A "gallery" is Recon's album. All calls take
`deviceTypeId=1`.

### 9.1 Read **[client]**

| Method | Path | Query |
|---|---|---|
| GET | `profiles/{profileId}/galleries` | `imageSizes`, `sortProperty=2`, `includeRestricted?`, `noCache=true` (self) |
| GET | `profiles/{profileId}/galleries/{galleryId}` | `imageSizes=100`, `noCache=true` (self) |
| GET | `profiles/{profileId}/galleries/{galleryId}/fileProperties` | `imageSizes`, `sortProperty`, `isVerified`, `includeRestricted`, `ignoreDeviceRestrictions`, `noCache` |
| GET | `profiles/{profileId}/fileProperties` | `imageSizes`, `sortProperty=2`, `isVerified`, `includeRestricted`, `galleryTypeId?`, `noCache`, `ignoreDeviceRestrictions` |
| GET | `events/{eventId}/fileProperties` | `galleryTypeId=5`, `isVerified`, `imageSizes=101,104` |
| GET | `profiles/{profileId}/files/{mediaId}.jpg` | `size`, `deviceTypeId` → binary |

**Gallery DTO:** `{id, name, entityId, galleryType, imageCount, coverImageId, coverImageFiles, url}`.
The synthetic "All Photos" album is represented client-side with `id: null`.

**MediaFileProperties DTO:**

```jsonc
{
  "id": "<uuid>", "caption": "…",
  "classificationId": 0, "classificationDate": "<iso8601>",
  "dimensions": { "widthPixels": 1200, "heightPixels": 1600 },
  "fileSizeInBytes": 234567, "fileType": "…",
  "files": [ { "imageSize": 100, "url": "…?size=100" } ],
  "galleryPositions": [ { "galleryId": "<uuid>", "galleryTypeId": 1, "position": 1 } ],
  "isRestricted": false, "uploadDate": "<iso8601>",
  "masterHash": "…", "originalHash": "…", "rotationDegrees": 0
}
```

`sortProperty` takes `2` (gallery lists, all-photos, gallery files) and `3` (main
gallery). The meaning of each is server-side and undetermined.

### 9.2 Write **[client]**

| Method | Path | Body |
|---|---|---|
| POST | `profiles/{profileId}/files` | **multipart**: `file`, `galleryId`, `galleryTypeId` |
| POST | `profiles/{profileId}/files/delete` | id list |
| DELETE | `profiles/{profileId}/galleries/{galleryId}/files/{fileId}` | — |
| PUT | `profiles/{profileId}/galleries/{galleryId}/files/{fileId}/position` | `{ "value": 1 }` |
| POST | `conversations/{conversationId}/files` | multipart |
| DELETE | `conversations/{conversationId}/files/{fileId}` | — |

Setting a photo as the main one is `position = 1` in the primary gallery.

⚠️ `conversations/{cid}/files` exists but the **web UI does not use it** — chat images go
inline as multipart on the send endpoint ([§8.5](#85-send-a-message-client)). Both paths
appear to work; the inline one is the proven one.

⚠️ **There is no create/rename/delete-gallery endpoint** in the web client. Galleries
appear to be server-provisioned.

Upload limits from `appSettings.limits`: `maxFileSizeBytes` 52428800,
`maximumFilesPerProfile` 500, `maximumFilesInMainGallery` 5, `maximumUploadsInBatch` 50,
`minImageResolution` 400, extensions `.bmp .gif .jpg .jpeg .png`.

### 9.3 Media CDN **[observed]**

```
https://media.recon.t101api.com/profiles/{profileId}/files/{fileId}.jpg?size={n}[&deviceTypeId=1]
https://media.recon.t101api.com/dvrts/{dvrtsrId}/files/{fileId}.jpg?size={n}
https://media.recon.t101api.com/dvrts/{dvrtsrId}/files/{fileId}          # no extension, no size
https://www.recon.com/api/media/profiles/{profileId}/files/{fileId}.jpg?culture=en&size={n}&deviceTypeId=1
```

- **CDN host: unauthenticated**, `public, max-age=2592000, immutable`, served by
  CloudFront. Avatar and advertiser media live here.
- **Gateway host: requires `Authorization: Bearer`**, `max-age=86400, private, immutable`.
  Chat attachments live here.

There is no URL signing. Use the absolute URLs the API returns verbatim rather than
constructing them.

---

## 10. `feed`

Base `https://www.recon.com/api/feed/`.

```http
GET  /api/feed/profiles/{profileId}/feeds/notifications?culture=en&noCache=true   [observed]
GET  /api/feed/profiles/{profileId}/feeds/home?culture=en                         [client]
GET  /api/feed/feedItems/{feedItemId}?culture=en                                  [client]
POST /api/feed/profiles/{me}/logNotificationsViewed?culture=en                    [client]  {viewedDate}
```

```jsonc
{
  "data": [{
    "feedItemId": "5229342",        // ⚠️ numeric, but a STRING
    "feedItemUrl": "https://www.recon.com/api/feed/feedItems/5229342",
    "profileUrl": "…/profiles/{id}/532",   // may carry a stale version segment
    "isSelfActor": false,
    "actionDate": "<iso8601>",
    "isRead": true,
    "feedItemTypeId": 3             // only 3 observed (cruise notification)
  }],
  "totalRecords": 7
}
```

The dereferenced **FeedItem** adds **[client]**: `standardText`, `selfActorText`,
`standardSubText`, `selfActorSubText`, `buttonText`, `isPinned`, `additionalData`.

The feed name is a path segment, so other feeds beyond `notifications` and `home` may
exist.

---

## 11. `dvrt` — sponsored content & broadcast messages

Base `https://www.recon.com/api/dvrt/`. ("dvrt"/"dvrtsr" = advert/advertiser.) This
service is entirely absent from most public notes on Recon, yet it drives the official
message threads that show up in the inbox.

### 11.1 Broadcast / official messages **[observed]**

```http
GET  /api/dvrt/profiles/{me}/bulkMessages?culture=en&deviceTypeId=1
POST /api/dvrt/profiles/{me}/bulkMessages/{id}/read?culture=en        {}
DELETE /api/dvrt/profiles/{me}/bulkMessages/{id}/delete?culture=en
```

```jsonc
{
  "data": [{
    "id": "<uuid>",
    "message": "…",          // markdown-ish: \n, **bold**, [link](url)
    "mediaUrl": "https://media.recon.t101api.com/dvrts/{id}/files/{fileId}",  // no extension, no size
    "deviceTypeId": 1, "priority": 1, "languageId": 1,
    "isAdult": false,
    "startDate": "<iso8601>",
    "isRead": false,
    "isOfficial": false,
    "senderUrl": "…",        // isOfficial=false → /api/dvrt/dvrtsrs/{id}
                             // isOfficial=true  → /api/profile/officialProfiles/{id}?culture=En
    "linkUrl": "…",
    "subject": "…"
  }],
  "totalRecords": 3
}
```

⚠️ `senderUrl` is a **discriminated** pointer — which service it targets depends on
`isOfficial`. And note the inconsistent `?culture=En` capitalisation on the official
variant.

### 11.2 Advertiser **[observed]**

```http
GET /api/dvrt/dvrtsrs/{dvrtsrId}?culture=en
```
```jsonc
{ "id": "<uuid>", "name": "…", "isOfficial": false,
  "officialProfileUrl": null,
  "mediaFiles": [ { "imageSize": 100, "url": "…/dvrts/{id}/files/{fileId}.jpg?size=100" } ] }
```

### 11.3 Impression telemetry **[observed]**

```http
POST /api/dvrt/contentStats?culture=en    → 204
```
```jsonc
{ "stats": [ { "contentId": "<uuid>", "advertZoneId": null,
               "isImpression": true, "isClick": false, "isOpen": false } ],
  "accountId": "<uuid>", "appInstallationId": "<uuid>", "deviceTypeId": 1 }
```

Batch bounds: `limits.contentStatsBatchCountLimit` 10,
`limits.contentStatsBatchTimeLimitSeconds` 600. A read-only client has no reason to call
this.

### 11.4 Adverts, news, preferences **[client]**

| Method | Path | Notes |
|---|---|---|
| GET | `dvrts` | filter params, same serialiser as search |
| GET | `admin/advertisers/{advertiserId}/campaigns/{campaignId}/adverts/{advertId}` | admin |
| GET | `news` | `skip`, `take` — **both required** |
| GET | `news/{id}` | also serves v2→v3 id translation via `.v3Id` |
| GET/PUT/PATCH | `profiles/{profileId}/preferences` | `{targetedOptin}` |

**Advert DTO:** `{advertZoneId, advertiser: {id, name, isOfficial, isActive, officialProfileId, mediaMetadataId, mediaFiles[]}, altText, deviceTypeId, id, isAdult, mediaUrl, priority, text, url}`.

**News DTO:** `{id, body, mediaFiles, metaDescription, metaKeywords, metaTitle, publishedDate, title, url, slug}`.

---

## 12. `location`

```http
PUT /api/location/profiles/{profileId}/geolocation?culture=en                                  [client]
GET /api/location/granularLocations/{lowerLocationId}/distances/{higherLocationId}?culture=en  [client]
GET /api/location/locations/{locationId}?culture=en                                            [client]
```

```jsonc
// GeoLocation body
{ "appInstallationId": "<uuid>", "latitude": 0.0, "longitude": 0.0,
  "lookup": { "longLocationName": "…", "shortLocationName": "…" },
  "profileId": "<uuid>", "recordedDate": "<iso8601>",
  "accuracyMetres": 25 }          // device-sourced only
```

The distance endpoint returns `{distanceMetres}` and requires the two location ids
**sorted lower-first**. Its URL template comes from `appSettings.siteUrls.profileDistanceUrl`.

Update cadence from `appSettings.geolocation`: min 2 min, max 15 min, significant-change
threshold 99 m.

ℹ️ Geocoding in the web app goes to **Mapbox**, not Recon, using a token embedded in the
bundle. Out of scope here.

---

## 13. `event` **[client]**

| Method | Path | Query / body |
|---|---|---|
| GET | `events` | `skip`, `take` (appended only when `take` is truthy) |
| GET | `events` | `isPast=true&skip&take` |
| GET | `events/{eventId}` | — |
| GET | `events/{eventId}/attendees/{profileId}` | → `{statusId}` |
| PUT | `events/{eventId}/attendees/{profileId}` | `{statusId}` |
| GET | `events/v2/{v2Id}` | → `{v3Id}` |

**Event DTO:** `{id, name, location, startDate, endDate, showAttendance,
thumbnailImageFiles, url, isSponsored, sponsorName, country, photosBy, mediaCount,
content, slug}`.

⚠️ The accepted `statusId` values (going / interested / not going) are **not resolvable**
from the bundle.

Client page sizes: event list 24, article profiles 12, article photos 30.

---

## 14. `membership` **[client]**

```http
GET /api/membership/profiles/{profileId}/membershipStatus?culture=en
```
```jsonc
{ "membershipLevelId": 1, "profileId": "<uuid>",
  "startDate": "<iso8601>", "expiryDate": "<iso8601>",
  "isRecurring": true, "recurringBilling": { } }
```

`membershipLevelId` 0 = free/official, ≥1 = premium. The client only ever tests `> 0`, so
higher tiers may exist without distinct client behaviour.

---

## 15. `payment` **[client]**

| Method | Path | Query / body |
|---|---|---|
| GET | `products` | → `{membershipProducts: [Product]}` |
| GET | `profiles/{profileId}/orders/{orderId}` | `includeProcessed=true` |
| POST | `profiles/{profileId}/orders` | `?productId=<id>`, body `{}` |
| POST | `profiles/{profileId}/orders/{orderId}/initiate` | `{}` → `{paymentUrl}` |
| DELETE | `profiles/{profileId}/recurringBilling` | cancel auto-renew |

**Product DTO:** `{id, description, currencyIso, price, priceWithCurrency, days, months,
isRecurring, savingsPercentage}`.

`initiate` returns a `paymentUrl` the client redirects the browser to.

---

## 16. `verification` **[client]**

| Method | Path | Query / body | Tag |
|---|---|---|---|
| GET | `accounts/{accountId}/verificationRequirement` | — | [observed] |
| POST | `singleUseTokens` | `null` → `{token}` | [client] |
| POST | `accounts/{accountId}/verifications` | `?verificationType=<n>&applicationId=3`, body `null` | [client] |
| POST | `accounts/{accountId}/verifications/{verificationId}/getStatus` | `null` → `{id, isComplete, isSuccessful}` | [client] |
| PUT | `accounts/{accountId}/verificationStats` | `{token: "<jwt>"}` | [client] |

```jsonc
// GET verificationRequirement  [observed]
{ "requiredVerificationStatus": 0, "requiredVerificationUrgency": 0,
  "anonymousAuthToken": "<opaque>" }
```

`requiredVerificationStatus` doubles as the `verificationType` argument:

| Value | Meaning |
|---|---|
| `0` | Already verified / not required |
| `1` | Age verification |
| `2` | Identity verification |

`verifications` returns `{url, verificationId, verificationStatusUrl}`; the client opens
`url` and polls `verificationStatusUrl` every
`appSettings.verification.verificationPendingPollingPeriodSeconds` (15) seconds.

---

## 17. `pushNotification` **[client]**

```http
PUT    /api/pushNotification/appInstallations/{appInstallationId}/deviceToken?culture=en
DELETE /api/pushNotification/appInstallations/{appInstallationId}/deviceToken?culture=en
```
```jsonc
{ "appInstallationId": "<uuid>", "applicationId": 3, "deviceTypeId": 1,
  "deviceToken": "<fcm token>", "accountId": "<uuid>" }
```

Tokens are Firebase Cloud Messaging. Irrelevant to a server-side client, which should use
SignalR instead.

---

## 18. `antiAbuse` **[client]**

```http
POST /api/antiAbuse/profileNameChecks?culture=en      ← unauthenticated
```
```json
{ "profileName": "…", "profileId": null }
```

Username rule enforced client-side: `^[a-zA-Z0-9]{4,20}$`.

---

## 19. `contentReview` **[client]**

```http
GET  /api/contentReview/reportCategories/?culture=en      # ⚠️ note the trailing slash
POST /api/contentReview/profiles/{profileId}/report?culture=en
```

```jsonc
// GET reportCategories
{ "data": [ { "id": 13, "name": "Other", "priority": 0, "order": 0, "detail": "…" } ] }
```

```jsonc
// POST report
{ "conversationMessageIds": ["<uuid>"],
  "isProfileContent": false,
  "mediaMetaDataIds": ["<uuid>"],      // ⚠️ capital D — inconsistent with mediaMetadataId elsewhere
  "reportCategoryId": 13,
  "reportReason": "…",
  "reportedByAccountId": "<uuid>" }
```

`reportCategories.other = 13` is the only id hardcoded in the client; fetch the rest.

---

## 20. Known gaps

Honest inventory of what this document genuinely cannot tell you. Everything else above
is either observed on the wire or read out of the client's own implementation.

| Gap | Why, and what was tried |
|---|---|
| **`imageSize` → pixel dimensions** | The client treats size codes as opaque tokens and lets the server pick the rendition. Searched for a code→dimensions map, `srcset`/`sizes` generation, width/height constants adjacent to size selection, and CSS keyed on size codes — none exist. The photo DTO's `dimensions.{widthPixels,heightPixels}` is the *original upload's* size, not a per-rendition size. Resolving this requires fetching one media URL at each of 100–104 and measuring. |
| **`classificationId` values ≥ 1** | The client only ever distinguishes `0` (pending review) from non-zero. No enum, no other comparison, no rejected/removed class anywhere in the bundle. |
| **`participantsUrl` response body** | Mapped onto the conversation model but **never dereferenced** — there is exactly one occurrence in the bundle, the assignment itself. The inline `participants[]` array is `{profileId, profileUrl}` per element, so the URL plausibly returns the same, but that is inference. |
| **`contentTypeId` values beyond `1`** | Only text was observed on the wire, and the client never reads the field at all, so it has no mapping to inspect. |
| **`sortProperty` values beyond `distance`** | Hardcoded at all three search call sites. No constant, no enum, no alternative anywhere. Whether the server accepts others is unknowable from the client. |
| **Whether `profileSearch/profiles` supports paging** | The client sends no `skip`/`take`/`offset`/`limit` and pages entirely in memory. These are not "supported but unused" — they are simply absent. The event service *does* use `?skip=&take=`, so the codebase knows the idiom and chose not to use it here. |
| **`refreshTokens` on the wire** | Never observed; the captured session re-authenticated instead. Path, body, and the "sent unauthenticated" detail are client-derived. |
| **Non-empty `attachments[]`** | Every observed message had an empty array, so the element shape is client-derived. |
| **Group conversations** | No `isGroup: true` record exists in the capture. `name` and `participantCount > 2` behaviour is untested. |
| **`membershipLevelId` beyond 0/1** | Only `> 0` and `== 1` are ever compared. ⚠️ Note this implies a real UI inconsistency: a tier ≥ 2 would be treated as premium (`> 0`) but get no badge (`== 1` false), suggesting such tiers are unused or unhandled. |
| **`discovery` service** | Registered in the base-URL table with **zero call sites** in the production build. The URL interceptor can rewrite hosts from a `discoBaseUrls` table in `localStorage`, but nothing in this bundle populates it — presumably seeded by the legacy v2 site. |

### Recently closed

For the record, these were open in earlier revisions and are now resolved: the error
envelope — now confirmed as RFC 7807 ProblemDetails plus Recon extensions, from a real
error response ([§1.8](#18-errors-observed)) — and its full code list, `markupTypeId`,
`feedItemTypeId`, `galleryTypeId`, event `statusId`, `deviceTypeId`, the media
`sortProperty` semantics, `listViewStats`, the `detailUrl` / `locationUrl` / `feedItemUrl`
response shapes, and the registration / change-password / DSAR request bodies
([§4.4](#44-registration-and-lifecycle-client)).

---

## 21. Minimum viable client

The shortest path to reading and sending messages:

1. `POST account/appInstallations` — remember `id`. **`applicationId` must be `3`.**
2. `POST account/accounts/authenticate` — remember `accessToken` (**strip the
   `"Bearer "` prefix**), `refreshToken`, `accountId`, `sessionId`, `profileIds[0]`.
3. `GET account/appSettings` — read the real limits instead of hardcoding them.
4. Send `Authorization: Bearer <bare jwt>` and `culture=en` on everything.
   Refresh via `refreshTokens` before the 15-minute expiry (3-minute grace).
5. `GET messaging/profiles/{me}/conversations?isHidden=false` — the peer is
   `participants[0]` (the list excludes you).
6. `GET …/conversations/{cid}/content?take=20&imageSizes=100,104`, then page backwards
   with `before=<createdDate of oldest held>`. Results are newest-first.
7. Resolve peers with `GET profile/profiles/{peerId}/1` (follow the 302) and use
   `primaryImageFiles[].url` verbatim.
8. Connect SignalR for realtime — see [recon-realtime.md](./recon-realtime.md).
9. Send with `POST …/messages` — `{text, files: [], attachments: []}` for text,
   multipart for images.
10. `POST …/logMessagesRead` with `{"LastMessageReadDate": "<iso>"}` to mark read.
