# Recon enums and lookup tables

Every numeric identifier the Recon API uses. Enum-heavy APIs are where re-implementers
stall, so this page collects them all in one place.

Companion to [`recon.md`](./recon.md) and [`recon-realtime.md`](./recon-realtime.md).
Tags follow the same [confidence legend](./recon.md#provenance-tags).

Tables in [§1](#1-profile-attribute-tables) are **served by the API** — fetch them rather
than hardcoding. The values below are a snapshot for reference and for offline decoding.

---

## 1. Profile attribute tables

All fetched from `GET /api/profile/{table}?culture=en` → `{data: [{id, name}], totalRecords}`.
Cacheable for 24 hours (`public, max-age=86400`). Names are localised by `culture`.

### `ethnicityId` — `GET /api/profile/ethnicities` **[observed]**

| id | Name |
|---|---|
| 1 | Other |
| 2 | Asian |
| 3 | Black |
| 4 | Hispanic |
| 5 | Latino |
| 6 | Middle Eastern |
| 7 | Mixed Race |
| 8 | South Asian |
| 9 | White |

### `positionId` — `GET /api/profile/positions` **[observed]**

| id | Name |
|---|---|
| 1 | Bottom |
| 2 | Vers Bottom |
| 3 | Versatile |
| 4 | Vers Top |
| 5 | Top |
| 6 | Side |

### `bodyTypeId` — `GET /api/profile/bodytypes` **[observed]**

| id | Name |
|---|---|
| 1 | Average |
| 2 | Athletic |
| 3 | Large |
| 4 | Muscled |
| 5 | Slim |

### `bodyHairId` — `GET /api/profile/bodyhairs` **[observed]**

| id | Name |
|---|---|
| 1 | None |
| 2 | Hairy |
| 3 | Average |
| 4 | Shaved |
| 5 | Some |

⚠️ Not ordinal — `2 Hairy` sorts before `3 Average`. Do not assume the ids imply a scale.

### `safeSexId` — `GET /api/profile/safesexes` **[observed]**

| id | Name |
|---|---|
| 1 | Always |
| 2 | Needs Discussion |
| 3 | Never |
| 4 | Sometimes |

ℹ️ Readable on every profile, but absent from every JSON-Patch set the web client sends —
there is no UI control for it.

### `roleId` — `GET /api/profile/roles` **[observed]**

| id | Name |
|---|---|
| 1 | Dom |
| 2 | Switch |
| 3 | Sub |

### `interests[]` — `GET /api/profile/interests` **[observed]**

A profile carries up to `appSettings.limits.interests` (10) of these.

| id | Name | | id | Name |
|---|---|---|---|---|
| 1 | Recon Men | | 38 | Smokers |
| 2 | Skinheads | | 39 | Gunge |
| 4 | Leather | | 40 | Trackies |
| 5 | Rubber | | 41 | Underwear |
| 6 | Sports Gear | | 42 | Chastity |
| 7 | Military | | 43 | Watersports |
| 8 | Hoods & Masks | | 44 | Impact Play |
| 9 | Muscle | | 45 | Electro |
| 10 | Punks | | 46 | Sneakers & Socks |
| 13 | Bikers | | 47 | ABDL |
| 15 | Bondage | | 48 | Edging |
| 18 | Suits | | 49 | Humiliation |
| 19 | Fisting | | 50 | Dirty |
| 20 | Masters & Slaves | | 51 | Spandex |
| 27 | Boots | | 52 | Hypnosis |
| 30 | Tattoos & Piercings | | | |
| 31 | Bears | | | |
| 34 | Fighting | | | |
| 36 | Feet | | | |
| 37 | Pups & Handlers | | | |

⚠️ **The id space is sparse.** 3, 11, 12, 14, 16, 17, 21–26, 28, 29, 32, 33, and 35 are
retired and absent from the response. Never assume a contiguous range; always fetch.

### `validHeightsCm` — `GET /api/profile/validHeightsCm` **[observed]**

⚠️ Unlike the other tables, `data` is a **bare integer array**, not objects.

```
152, 155, 157, 160, 163, 165, 168, 170, 173, 175, 178, 180, 183,
185, 188, 191, 193, 196, 198, 201, 203, 206, 208, 211, 213, 214
```

The client additionally clamps to `[152, 214]`. Values are a rounded imperial ladder
(5'0" through 7'0"), which is why the spacing is irregular.

---

## 2. Identity and client enums

### `applicationId` **[observed]**

| Value | Meaning |
|---|---|
| `3` | Recon web / v3 client |

⚠️ **Must be the integer `3`** on `POST account/appInstallations`,
`PUT pushNotification/…/deviceToken`, and `POST verification/…/verifications`. Sending a
UUID makes the server reject the whole request model.

### `deviceTypeId` **[client]**

| Value | Meaning |
|---|---|
| `0` | Unknown |
| `1` | Web |
| `2` | iOS |
| `3` | Android (Play) |
| `4` | Android (X) |

The web client always sends `1`, and the JWT's `device_type` claim reads `"Web"`. The
full enum is declared in the client, so the other values are usable.

### `membershipLevelId` **[observed]**

| Value | Meaning |
|---|---|
| `0` | Free, and all official/system profiles |
| `≥1` | Premium |

The client only ever evaluates `membershipLevelId > 0`, so distinct higher tiers may exist
without differing client behaviour. Observed values: `0`, `1`.

### `languageId` **[observed]**

Appears on `dvrt` bulk messages. Only `1` observed. `appSettings.supportedLanguages` lists
the `culture` codes: `en`, `de`, `es`, `fr`, `pt`.

---

## 3. Media enums

### `imageSize` / `size` **[observed]**

**Only 100–104 exist.**

| Code | Role | Typical payload |
|---|---|---|
| `100` | Small thumbnail. Hardcoded wherever a fixed small rendition is wanted. | 4–13 kB |
| `101` | Thumbnail, tablet/handset viewports | not fetched in capture |
| `102` | Thumbnail, desktop/wide viewports | 143–162 kB |
| `103` | Large, tablet/handset viewports | not fetched in capture |
| `104` | Large, desktop/wide viewports | not fetched in capture |

Viewport selection used by the web client:

| Breakpoint | thumbnail | large |
|---|---|---|
| Large / Web / WebLandscape | `102` | `104` |
| Tablet* / Handset* | `101` | `103` |

Usage: `?imageSizes=102,104` on list endpoints (one `files[]` entry per code requested);
`?size=102` on the single-file endpoint.

⚠️ **`661` is not an image size.** It is a profile **version** path segment — see
[recon.md §5.1](./recon.md#51-fetch-a-profile-observed). Passing `661` in `imageSizes`
requests a rendition that does not exist.

⚠️ **Pixel dimensions are unknown.** No code→dimensions mapping exists anywhere in the
bundle. The byte sizes above are the only signal.

### `galleryTypeId` **[client]**

| Value | Meaning |
|---|---|
| `1` | MainProfile |
| `2` | PublicProfile |
| `3` | PrivateProfile |
| `4` | EventCover |
| `5` | PostEvent |
| `6` | ConversationAttachments |
| `7` | News |
| `8` | Micronews |
| `9` | SponsoredLink |
| `10` | BulkMessage |
| `11` | Advertiser |
| `null` | Synthetic client-side "All Photos" view — not a server gallery |

⚠️ `galleryTypeId=5` on `media/events/{id}/fileProperties` is **PostEvent** (the
after-the-fact photo gallery), not the event's cover — that is `4`.

The photo-upload album picker only offers `2 PublicProfile` and `3 PrivateProfile`.
Gallery objects also expose `galleryType.isRestricted`.

### `sortProperty` **[client]**

| Context | Value | Meaning |
|---|---|---|
| `profileSearch/profiles` | `distance` (string) | The only value ever sent. No other accepted value could be found. |
| `media` — main gallery, main profile images | `3` | **Curated gallery position order** — the order the member arranged. Corroborated by `galleryPositions[].position` and `PUT …/files/{id}/position` (setting the main photo writes `position = 1`). |
| `media` — album grid, all-photos, gallery lists, `fileProperties` | `2` | **Upload date, newest first.** The default browse ordering. |

### `classificationId` **[client]**

Photo moderation state on `MediaFileProperties`.

| Value | Meaning |
|---|---|
| `0` | **Pending review / unclassified** — the client renders a "pending review" overlay |
| non-zero | Reviewed |

The client makes exactly one distinction, `== 0` vs `!= 0`. It has no enum, and no
rejected/removed class is referenced, so **[unverified]**: individual values ≥ 1 cannot
be enumerated from the client.

---

## 4. Messaging enums

### `contentTypeId` **[observed]**

| Value | Meaning |
|---|---|
| `1` | Text |

⚠️ **This is the real wire field on message DTOs.** The web client's mapper reads a
`messageType` property, but no server response contains it — see
[recon.md §8.2](./recon.md#82-fetch-message-history-observed). Only `1` was observed;
values for attachment-bearing messages are unknown.

### `markupTypeId` **[client]**

How to render a message body. **Only honoured on official (system/brand) conversations** —
on ordinary member messages the field is ignored and the text renders as plain text.

| Value | Rendering |
|---|---|
| `1` | **Markdown** |
| `2` | **Raw HTML** (injected as innerHTML) |

⚠️ Only these two are handled. An official message arriving with `markupTypeId` of `0`,
`3`, or `null` renders **nothing at all** in the web client — the template has no
fallback branch. `null` in every observed message, because the capture contained no
official-thread content.

### `messageType` **[client]**

Client-side only. The web client writes `0` on locally-constructed outgoing messages and
never receives this field. Ignore it when parsing.

---

## 5. Feed and notification enums

### `feedItemTypeId` **[client]**

Complete and contiguous, 1–24.

| Id | Name | Id | Name |
|---|---|---|---|
| 1 | FriendRequested | 13 | ProfilePhotoUpdated |
| 2 | FriendRequestAccepted | 14 | ProfileSuspended |
| 3 | ProfileFollowed | 15 | ProfileReactivated |
| 4 | ProfileVisited | 16 | EventJoined |
| 5 | ProfileCruised | 17 | EventCreated |
| 6 | ProfileBlocked | 18 | EventCancelled |
| 7 | ProfileUnblocked | 19 | SystemNotification |
| 8 | MediaUploaded | 20 | TermsAndConditionsUpdated |
| 9 | MediaLiked | 21 | NearbyActivity |
| 10 | MediaCommented | 22 | ProfileCreated |
| 11 | MediaShared | 23 | ProfileInterestsAdded |
| 12 | ProfileUpdated | 24 | ProfileTextUpdated |

ℹ️ Two things worth knowing about how these are consumed:

- **The home feed hard-filters to four types** client-side — `8 MediaUploaded`,
  `13 ProfilePhotoUpdated`, `24 ProfileTextUpdated`, `23 ProfileInterestsAdded` — and
  discards everything else. The server sends more than the home feed shows.
- **The notifications panel does not switch on the id for text.** The server supplies the
  copy as `standardText` / `selfActorText` (plus `standardSubText` / `selfActorSubText` /
  `buttonText`), and the client picks the self-actor variant when `isSelfActor` is true.
  The only id special-cased there is `1 FriendRequested`, which gets accept/reject
  buttons. So a re-implementation does **not** need to hardcode per-type strings.

### `socketEventId` **[client]**

⚠️ **Client-side synthetic, never on the wire.** Listed only so you recognise it when
reading the web client or ports of it. Dispatch on the SignalR `target` string instead.

| Value | SignalR target |
|---|---|
| `1` | `JoinedConversations` |
| `2` | `ReceiveMessage` |
| `3` | `ReceiveTypingStatus` |
| `4` | `ReceiveReadReceipt` |
| `6` | `ReceiveUnreadConversationCount` |
| `7` | `ReceiveUnreadCruisesCount` |
| `8` | `ReceiveUnreadVisitsCount` |
| `9` | `ReceiveUnreadNotificationsCount` |

### SignalR connection state **[client]**

| Value | State |
|---|---|
| `0` | Disconnected |
| `1` | Connected |
| `4` | Reconnect pending |
| `5` | Connecting |

---

## 5a. Event enums

### `statusId` — RSVP attendance **[client]**

Sent as `PUT event/events/{eventId}/attendees/{profileId}` with body `{"statusId": n}`,
read back from the matching `GET`.

| Value | Meaning |
|---|---|
| `1` | Going |
| `2` | Not going |

⚠️ **There is no "interested" state.** The web UI is a binary toggle, and unauthenticated
visitors default to `2`. If the backend supports a third value, this client can neither
send nor display it.

## 5b. Advertising enums

### `advertZoneId` **[client]**

Sent on `POST dvrt/contentStats` impression telemetry.

| Value | Zone |
|---|---|
| `2` | WebProfileLargeLeaderboard |
| `3` | WebProfileMobileLeaderboard |
| `4` | WebProfileFeedPrimary |
| `5` | WebProfileFeedSecondary |
| `6` | WebHomeFeedPrimary |
| `7` | WebHomeFeedSecondary |

## 6. Verification enums

### `requiredVerificationStatus` / `verificationType` **[client]**

The same value is returned by `GET verification/accounts/{id}/verificationRequirement` and
then passed straight back as the `verificationType` query parameter.

| Value | Meaning |
|---|---|
| `0` | Already verified, or not required |
| `1` | Age verification |
| `2` | Identity verification |

`requiredVerificationUrgency` is a sibling int with no known table; `0` observed.

---

## 7. Moderation enums

### `reportCategoryId` **[observed]**

Fetch from `GET /api/contentReview/reportCategories/` (note the trailing slash) →
`{data: [{id, name, priority, order, detail}]}`.

| Value | Meaning |
|---|---|
| `13` | Other |

Only `13` is hardcoded in the client. Fetch the rest.

---

## 8. Error codes **[client]**

Numeric codes the web client matches on. The surrounding envelope is RFC 7807 ProblemDetails plus
Recon's own `t101ErrorCode` and `link` members — see
[recon.md §1.8](./recon.md#18-errors-observed).

⚠️ **These meanings are the client's interpretation, not the server's definition.** Each
row describes how the web client renders that code *in one particular flow*. The server
reuses codes across contexts: a live `404` carrying `t101ErrorCode: 901000001` came back
with `title: "Invalid GUID parameter."`, nothing to do with friend requests. Treat the
code as a coarse category, branch on it only alongside the endpoint you called, and
prefer `title` for anything user-facing.

| Code | Meaning | Context |
|---|---|---|
| `101000050` | Password found in breach corpus | Registration, password change |
| `102000040` | Profile blocked | Paywall path |
| `102000050` | Profile not found | Friend requests |
| `102000061` | Data recently requested | GDPR DSAR throttle |
| `103000002` | Your friends limit reached | `limits.friends` (500) |
| `103000009` | Target's friend limit reached | |
| `103000010` | Daily limit reached | Paywall path |
| `300000001` | Request not found | DSAR download → link expired |
| `901000001` | Generic "referenced thing is invalid or gone" | The client renders it as *friend request cancelled* on an accept (`404`), but the server also returns it with `title: "Invalid GUID parameter."` for a malformed id. See the warning below. |
| `301000005` | You have been blocked | Replying to a conversation (on `404`) |
| `777` | Password found in breach corpus | Change **email** only — a near-certain bug, since the identical message uses `101000050` on the change-password path. Handle both. |

---

## 9. Server-side limits **[observed]**

Not enums, but the other thing clients wrongly hardcode. **Fetch these** from
`GET /api/account/appSettings?culture=en&deviceTypeId=1` rather than copying the values
below — they are a snapshot, not a contract. Full response in
[recon.md §4.1](./recon.md#41-get-appsettings-observed--unauthenticated).

| Key | Value | Governs |
|---|---|---|
| `maxMessageLength` | 1000 | Message text |
| `maxFileAttachments` | 10 | Attachments per message |
| `maxFileSizeBytes` | 52428800 (50 MiB) | Upload size |
| `allowedFileExtensions` | `.bmp .gif .jpg .jpeg .png` | Upload type |
| `allowedMediaMimeTypes` | `image/bmp image/gif image/jpeg image/png` | Upload type |
| `interests` | 10 | Profile interests |
| `blocks` | 50 | Block list |
| `friends` | 500 | Friend list |
| `following` | 500 | Follow list |
| `minAge` / `maxSearchAge` | 18 / 99 | Search age range |
| `minImageResolution` | 400 | Upload dimensions |
| `minPasswordLength` / `minimumZxcvbnScore` | 8 / 3 | Password policy |
| `maximumFilesPerProfile` | 500 | Total media |
| `maximumFilesInMainGallery` | 5 | Primary gallery |
| `maximumUploadsInBatch` | 50 | Batch upload |
| `visitRetentionPeriodDays` | 30 | Visit history |
| `cruiseRetentionPeriodDays` | 30 | Cruise history |
| `standardMemberVisitDisplayPeriodDays` | 7 | Free-tier visit window |
| `standardMemberCruiseDisplayPeriodDays` | 7 | Free-tier cruise window |
| `cruiseMinUpdatePeriodDays` | 7 | Re-cruise throttle |
| `defaultProfileSearchActiveWithinMinutes` | 43200 (30 d) | Default search recency |
| `profileSearchIsOnlineNowActiveWithinMinutes` | 15 | "Online now" |
| `profileSearchIsNewMemberJoinedWithinDays` | 30 | "New member" |
| `contentStatsBatchCountLimit` | 10 | Ad telemetry batching |
| `contentStatsBatchTimeLimitSeconds` | 600 | Ad telemetry batching |
| `minimumDistanceDisplayedMetres` | 500 | Distance privacy floor |
| `minimumDistanceDisplayedFeet` | 1500 | Distance privacy floor |

### Distance slider values **[observed]**

`distanceSliderIndex` in the search filter indexes into one of these, by unit preference:

| System | Values |
|---|---|
| Metric (km) | 1, 2, 3, 4, 5, 10, 15, 25, 50, 150, 400, 800 |
| Imperial (mi) | 1, 2, 3, 4, 5, 10, 15, 25, 50, 100, 250, 500 |

Converted to `radiusMetres` as `1000 × value` (metric) or `1609 × value` (imperial).

### Token lifetimes **[observed]**

| Token | Lifetime |
|---|---|
| Access token (JWT) | 900 s (15 min) |
| Refresh token | 14 days |
| Refresh grace window | 3 min (`jwtGracePeriodInMinutes`) |
| SignalR client ping | 15 s |
| SignalR server timeout | 30 s |

---

## 10. JSON-Patch operations **[client]**

Accepted on `PATCH /api/profile/profiles/{id}` and the two `preferences` endpoints:
`add`, `copy`, `remove`, `replace`, `test` (RFC 6902).

The web client only ever emits `replace`, and always includes `/rowVersion` for optimistic
concurrency. See [recon.md §5.2](./recon.md#52-edit-a-profile-client) for the exact patch
sets it uses.
