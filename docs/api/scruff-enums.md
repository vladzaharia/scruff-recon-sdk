# SCRUFF enums and wire values

Every numeric identifier the SCRUFF API uses. The API is unusually enum-heavy — profile
attributes, grid sorting, moderation states, and the realtime channel are all integer
codes — so this page collects them in one place.

Companion to [`scruff.md`](./scruff.md) and [`scruff-realtime.md`](./scruff-realtime.md).
Tags follow the same [provenance convention](./scruff.md#provenance-tags); nearly
everything here is **[client]**, read from the app's own enum declarations and its
resource tables, which makes these values authoritative.

> ⚠️ **Read this before hand-typing any table below.** The app stores several profile
> enums as *paired* arrays — one of display strings, one of integer values — and in two
> cases the display order deliberately does **not** match the numeric order. If you
> transcribe by position you will get those wrong. Both traps are flagged in
> [§2](#2-profile-attributes).

---

## 1. Messaging and media

### `message_type` — `ChatMessageType`

| Value | Name | Notes |
|---|---|---|
| `0` | Unset | |
| `1` | Text | `message` holds the body |
| `2` | Image | full-size photo |
| `3` | Video | |
| `4` | Location | `location` part |
| `5` | Emoji | |
| `6` | Gif | `message` holds a GIF reference, not bytes |
| `7` | HlsVideo | `manifest_url` + `manifest_cookies` |
| `8` | Reaction | `reaction` + `reacted_to` |
| `9` | Typing | |
| `12` | Album | `shared_album_id` + `album_share_limit` — this is how album sharing works |

`10` and `11` are absent from this build.

### `media_type` — `ChatMediaType`

Query parameter on `GET /app/chat/media`.

| Value | Meaning |
|---|---|
| `0` | Image |
| `1` | Video |

### `media_behavior` — `MediaBehavior`

**Complete — exactly two values.**

| Value | Meaning |
|---|---|
| `0` | Normal |
| `1` | Single view (disappearing) |

The app's parser is defensive: any unknown integer degrades to `0`. Sent as a multipart
field on `POST /app/chat` **only for media messages** — omitted entirely for text and
album messages.

### `media_type` on album images — `MediaType`

Different enum from `ChatMediaType` above, despite the similar name.

| Value | Name | MIME |
|---|---|---|
| `0` | Unknown | |
| `1` | Image | `image/jpeg` |
| `2` | Video | `video/mp4` |
| `3` | Gif | `image/gif` |
| `4` | HlsVideo | |
| `5` | Binary | `application/octet-stream` |

### `media_type` on moments — `MomentMediaType`

| Value | Meaning |
|---|---|
| `0` | Image |
| `1` | HLS video |

---

## 2. Profile attributes

These are the values a profile carries and a search filters on. Each comes from a paired
string/integer resource table, so the numeric values are explicit rather than positional.

### `relationship_interests[]` — `RelationshipInterest`

| Value | Label |
|---|---|
| `0` | Unset |
| `1` | Friendships |
| `2` | Relationships |
| `3` | Random play / NSA |
| `4` | Dates |
| `5` | Chat only |
| `6` | Networking |

ℹ️ **`looking_for` is the legacy predecessor of this field** — a single integer rather
than an array. The app has **no enum and no UI for it**; it reads it off the profile and
writes it straight back untouched. Every other enum-typed field is converted through a
typed enum on the way in and out; `looking_for` and `flavors` are the only two passed
through raw. **Echo whatever the server sent and never synthesise a value.** The
numbering is most likely this table or an ancestor of it, but that is inference — the
legacy vocabulary is not in the binary.

### `body_hair` — `BodyHair`

| Value | Label |
|---|---|
| `0` | Unset |
| `1` | Smooth |
| `2` | Some hair |
| `3` | Hairy |
| `4` | Very hairy |

### `ethnicity` — `Ethnicity`

| Value | Label |
|---|---|
| `0` | Unset |
| `1` | Asian |
| `2` | Black |
| `3` | Hispanic / Latino |
| `4` | South Asian |
| `5` | Middle Eastern |
| `6` | Pacific Islander |
| `7` | White |
| `8` | Multi-racial |
| `9` | Native American |

ℹ️ Value `4`'s internal key is `Indian` while its display string is "South Asian" — a
rename that never reached the identifier.

### `relationship_status` — `RelationshipStatus`

| Value | Label |
|---|---|
| `0` | Unset |
| `1` | Single |
| `2` | Dating |
| `3` | Partnered |
| `4` | Engaged |
| `5` | Married |
| `6` | Open relationship |
| `7` | In a relationship |
| `8` | Widowed |
| `9` | Polyamorous relationship |

⚠️ **Trap.** The app's picker displays these in the order `…, 7, 9, 8` — Polyamorous
appears before Widowed. If you transcribe the displayed list positionally you will swap
`8` and `9`. The values above are the wire values.

### `sex_preferences[]` — `SexPreference`

| Value | Label |
|---|---|
| `0` | Unset |
| `1` | Top |
| `2` | Bottom |
| `3` | Versatile |
| `4` | Oral only |
| `5` | Fetish |
| `6` | No sex |
| `7` | Side |

⚠️ **Same trap.** `Side` was added later and carries value `7`, but the picker displays
it fourth, in the order `1, 2, 3, 7, 4, 5, 6`.

### `sex_safety_practices[]` — `SexSafetyPractice`

| Value | Label |
|---|---|
| `0` | Unset |
| `1` | Condoms |
| `2` | I am taking PrEP |
| `3` | Treatment as prevention |
| `4` | I am undetectable |
| `5` | Let's discuss |

### `hiv_status` — `HivStatus`

| Value | Label |
|---|---|
| `0` | Unset |
| `1` | Positive |
| `2` | Positive, undetectable |
| `3` | Negative |
| `4` | Let's discuss |

### `vaccinations[]` — `Vaccination`

| Value | Label |
|---|---|
| `0` | Unset |
| `1` | Let's discuss |
| `2` | COVID-19 |
| `3` | Mpox |
| `4` | Meningitis |
| `5` | HPV |
| `6` | Hepatitis A+B |

### `testing_reminder_frequency` — `TestingReminderFrequency`

| Value | Label |
|---|---|
| `0` | Unset |
| `1` | Every month |
| `2` | Every 3 months |
| `3` | Every 6 months |

### `accepts_nsfw_content` — `AcceptsNsfwContent`

| Value | Label |
|---|---|
| `0` | Unset |
| `1` | Always |
| `2` | Not at first |
| `3` | Never |

### `community[]` and `community_interests[]` — `Community`

The "tribes" list. `community` is what a profile *is*; `community_interests` is what it
is *into*. Both use this numbering, contiguous 1–39.

| Value | Label | Value | Label |
|---|---|---|---|
| `1` | Bear | `21` | Otter |
| `2` | Military | `22` | Sturdy |
| `3` | Jock | `23` | Pretty |
| `4` | Muscle | `24` | Standard |
| `5` | Leather | `25` | Slim |
| `6` | Geek | `26` | Everyman |
| `7` | College | `27` | Clean cut |
| `8` | Transgender | `28` | Has place |
| `9` | Twink | `29` | Likes older |
| `10` | Poz | `30` | Likes younger |
| `11` | Bear chaser | `31` | Slim muscular |
| `12` | Daddy | `32` | Slim smooth |
| `13` | Daddy chaser | `33` | Middle aged |
| `14` | Discreet | `34` | Femme |
| `15` | Queer | `35` | Cute |
| `16` | Bisexual | `36` | Puppy |
| `17` | Chub | `37` | Cat |
| `18` | Drag | `38` | Dom |
| `19` | Chaser | `39` | Sub |
| `20` | Guy next door | | |

⚠️ **`11` and `13` are marked deprecated** (Bear chaser, Daddy chaser), superseded by
`19 Chaser`. Still accepted on the wire; don't offer them in a picker.

⚠️ **Values 22–39 are region-gated to Korea** and ship with Korean display strings only —
they are not localised to English anywhere in the app. The English labels above are
translations for your reference; a client that surfaces these to a non-Korean user using
the app's own strings will show Hangul.

ℹ️ **`flavors[]` is not this list.** It is the legacy tribes field that `community`
replaced, and like `looking_for` the app round-trips it without an enum. Do not conflate
them. Separately, the singular **`flavor` query parameter** is something else again — the
app-family identifier in [§3](#3-client-identity).

### `browse_mode` — `BrowseMode`

| Value | Meaning |
|---|---|
| `0` | Unset |
| `1` | Bear mode |

### `my_rating` / `his_rating` / `rating` — `ProfileRating`

| Value | Meaning |
|---|---|
| `0` | Unknown |
| `1` | Not my type |
| `2` | Maybe |
| `3` | Definitely |

### `verified_status` — `VerificationStatus`

| Value | Meaning |
|---|---|
| `0` | Unverified |
| `1` | Verified |
| `2` | Pending |

### `inclusion_reason` — `InclusionReason`

Why a profile appeared in a grid.

| Value | Meaning |
|---|---|
| `0` | Unset |
| `1` | Unseen positive |
| `2` | Toplist new member |
| `3` | Back catalog |
| `4` | Nearby |
| `5` | Reminder |
| `6` | Collaborative filter |

### `urls[].service` — `ProfileUrlService`

| Value | Service | Value | Service |
|---|---|---|---|
| `0` | Unset | `6` | PSN |
| `1` | Personal | `7` | Xbox Live |
| `2` | Facebook | `8` | Switch |
| `3` | Twitter | `9` | Steam |
| `4` | Instagram | `10` | TikTok |
| `5` | Airbnb | `11` | Bluesky |

### `explicit_content_visibility` / `suggestive_content_visibility` — `ContentVisibility`

| Value | Meaning |
|---|---|
| `0` | Unset |
| `1` | Hide |
| `2` | Blur |
| `3` | Show |

### Age bands

Used in filter UI. No integer array — these are display bands over `age_in_years`:
18–25, 26–35, 36–45, 46–55, 56–65, 66–75, 76+.

---

## 3. Client identity

### `flavor` — `AppFlavor`

Perry Street Software runs three apps off one backend.

| Value | App |
|---|---|
| `1` | SCRUFF |
| `2` | Jack'd |
| `3` | GROWLr |

Sent as a standard identity parameter on every request. **Not** the same as a profile's
`flavors[]` array ([§2](#2-profile-attributes)).

### `device_type` — `DeviceType`

| Value | Platform |
|---|---|
| `0` | Unknown |
| `1` | iPhone |
| `2` | Android |
| `3` | iPad |
| `4` | Windows Phone |

Also the fourth component of the request signature.

### `client_version` composition

`client_version` is **derived from the semver**, not free-form:

```
client_version = sprintf("%d.%02d%02d", major, minor, patch)
```

So `8.16.0` → `8.1600`, and a hypothetical `8.7.3` → `8.0703`. The raw semver goes out
separately as `client_semver`.

⚠️ **The app is not self-consistent here.** The domain-fronting probes (`/ping`,
`/ping_ip`), the captcha web view, and the connection diagnostics all send the **raw
semver** (`8.16.0`) as `client_version` instead of the packed form. Either is evidently
accepted on those endpoints.

### `unit_type` — `UnitSystem`

| Value | Meaning |
|---|---|
| `0` | Default (locale) |
| `1` | Metric |
| `2` | US |

### `push_environment`

`POST /app/push` takes exactly two values: **`"debug"`** and **`"production"`**. The
selector is a build-config flag; release builds always send `production`.

---

## 4. Grids and filters

### `query_sort_type` — `QuerySortType`

| Value | Ordering |
|---|---|
| `0` | Distance |
| `1` | Time |
| `2` | Online |
| `3` | Distance + newness |

### `online` filter — `OnlineMode`

| Value | Meaning |
|---|---|
| `0` | Unset |
| `1` | Online now |
| `2` | Last month |
| `3` | Last day |
| `4` | New member |
| `5` | Last hour |

⚠️ Not monotonic in recency — `5` (last hour) is tighter than `3` (last day). Treat these
as opaque tokens, not a scale.

### `image` filter — `ImageFilter`

| Value | Meaning |
|---|---|
| `0` | Unset |
| `1` | Any photo |
| `2` | Face pic |

### `folder_type` — `FavoriteFolderType`

| Value | Meaning |
|---|---|
| `1` | Frequent chats |
| `2` | Moments |

`null` for user-created folders.

### `block_type` — `BlockType`

| Value | Meaning |
|---|---|
| `0` | Block |
| `1` | Hide |

Set via `POST /app/block`'s `hide` boolean: `hide=true` → Hide, `hide=false` → Block.

### `inbox_style`

The app sends the integer literal **`2`** and nothing else — it is the sole occurrence in
the binary. It selects the server's response format; format `2` is the one that returns
per-message `version` fields, which the sync protocol depends on.

**[unverified]** — formats `0` and `1` are not referenced by any code path, so their
shapes are unknown. Send `2`.

### `free_features`

A JSON array literal sent as a single query/form value, e.g.
`["read_receipt","unsend_messages"]`, or `[]` when empty.

The client is declaring *"these normally-paid features are currently free for me"*, so
the server applies the matching entitlement to that request. It exists so a remote-config
rollout — which reaches clients before the server-side gate flips — isn't rejected by Pro
gating. Values are `UpsellFeature` keys ([§5](#5-membership-and-store)); the realistic
rollout set is `read_receipt`, `nearby_filters`, `extra_search_filters`,
`unsend_messages`, `stealth_mode`, `album_management_save_to_album`.

Sent on exactly two endpoints: `GET /app/chat` and `PUT /app/chat` with `unsent=true`.

ℹ️ Note the trust model: this is client-supplied and unsigned, so the server must be
cross-checking it against its own rollout state. Sending `[]` is always safe.

---

## 5. Membership and store

### `account_tier.tier` — `AccountTierName`

`"free"` | `"pro"` — strings, not integers.

### `pro_type.type` — `ProTypeName`

`"free_trial"` | `"paid_subscription"` | `"pro_pass"`

### `store_id` — `StoreId`

`"apple"` | `"google_play"` | `"admin"` | `"free_trial"` | `"stripe"`

### `LegacyStoreType`

| Value | Store | Value | Store |
|---|---|---|---|
| `0` | Unset | `4` | Free trial |
| `1` | Apple | `5` | Windows |
| `2` | Google Play | `6` | SCRUFF |
| `3` | Admin | `7` | Stripe |

### `AccountFeatureType`

| Value | Feature |
|---|---|
| `0` | Unset |
| `1` | No ads *(deprecated)* |
| `2` | More guys |
| `3` | Hi-res |
| `4` | Messaging |
| `5` | Hi-res profile album |
| `6` | SCRUFF Pro |
| `8` | Pro Pass |

### `SubscriberType`

| Value | Meaning |
|---|---|
| `0` | Unset |
| `1` | Free |
| `2` | Pro |

### `stripe_environment` — `StripeEnvironment`

`"production"` | `"test"` | `"sandbox"`

### `BoostState`

| Value | Meaning |
|---|---|
| `-1` | Initialize |
| `0` | None |
| `1` | Active |
| `2` | Ended |

### `UpsellFeature` — the paywall vocabulary

51 string keys. These name every feature behind Pro, and double as the `free_features`
vocabulary ([§4](#4-grids-and-filters)).

`add_favorites_limit`, `favorite_folders`, `block_users_limit`, `profile_browsing_limit`,
`grid_sorting`, `unsend_messages`, `mark_all_chats_as_read`, `share_private_album_limit`,
`share_private_album_in_chat`, `combined_search_filters`, `extra_search_filters`,
`album_management_create`, `album_management_move`, `album_management_reorder`,
`album_management_save_to_album`, `album_management_save_to_device`,
`album_management_open_collection`, `send_recent_media_limit`, `send_album_media_limit`,
`send_multiple_media`, `match_stacks_limit`, `stealth_mode`, `profile_notes`,
`disable_ratings_requests`, `hide_from_global_grid`, `overnight_mode`,
`customizable_alerts`, `device_password`, `nearby_browsing_limit`,
`search_browsing_limit`, `woofs_browsing_limit`, `looks_browsing_limit`,
`albums_browsing_limit`, `mutual_matches_browsing_limit`, `unread_inbox_browsing_limit`,
`recent_inbox_browsing_limit`, `favorites_browsing_limit`, `inbox_browsing_limit`,
`chat_history_limit`, `venture_profiles_browsing_limit`, `event_profiles_browsing_limit`,
`hashtag_profiles_browsing_limit`, `partner_picker_browsing_limit`, `nearby_filters`,
`nearby_filters_tags`, `location_search_limit`, `read_receipt`,
`you_woofd_browsing_limit`, `you_looked_browsing_limit`, `unknown`.

### `PurchaseState`

| Value | Meaning |
|---|---|
| `0` | Unspecified |
| `1` | Purchased |
| `2` | Pending |

---

## 6. Albums and moments

### `album_type` — `AlbumType`

| Value | Meaning |
|---|---|
| `0` | Archive album |
| `1` | Recent album |
| `2` | Private album |
| `3` | Profile synthetic |
| `4` | Chat synthetic |

### `target_types[]` — `MomentTargetGroup`

Audience targeting for a Moment.

| Value | Group |
|---|---|
| `1` | Recently chatted |
| `2` | You unlocked |
| `3` | They unlocked |
| `4` | You woofed |
| `5` | They woofed |
| `8` | Nearby |
| `9` | Mutual |

`6` and `7` are absent from this build.

### `classification_status` / `classification_outcome`

| `classification_status` | Meaning |
|---|---|
| `0` | Unmoderated |
| `2` | Moderated |

| `classification_outcome` | Meaning |
|---|---|
| `-1` | Rejected |
| `2` | Approved |
| `3` | Suggestive |
| `4` | Explicit |

### `flag[reason]` — `FlagMomentReason`

**Moments only.** Profile reports do not use this — see
[§8](#8-moderation-and-reporting).

| Value | Reason |
|---|---|
| `125` | Inappropriate media |
| `126` | Drugs |
| `127` | Spam |
| `128` | Commercial |
| `129` | Underage |
| `130` | Harassment |
| `131` | Impersonation |
| `133` | Inappropriate tap-in |

ℹ️ The sparse, high-numbered range suggests these are a slice of one global reason-id
space shared with profile reports.

---

## 7. Media renditions and photo state

### Rendition constraint suffixes

Passed as `fullsize_constraint` / `thumbnail_constraint` (singular, on `GET /app/profile`)
or `full_size_constraints[]` / `thumbnail_constraints[]` (plural, on album endpoints).

| Fullsize | Thumbnail |
|---|---|
| `-fullsize` | `-thumbnail` |
| `-fullsize-small` | `-thumbnail-small` |
| `-fullsize-400K` | `-thumbnail-300` |
| `-fullsize-800K` | `-thumbnail-600` |
| `-fullsize-1600K` | |
| `-fullsize-6400K` | |
| `-original-50000K` | |

A photo's `etags` map enumerates which renditions actually exist for it.

### Quality enums

| `ImageFullsizeQuality` | Target | | `ThumbnailQuality` | Target |
|---|---|---|---|---|
| `1` | 25 K | | `1` | 75 × 75 |
| `2` | 200 K | | `2` | 150 × 150 |
| `3` | 400 K | | `3` | 300 × 300 |
| `4` | 800 K | | `4` | 600 × 600 |
| `5` | 1600 K | | | |

Fullsize targets are byte budgets; thumbnail targets are pixel dimensions.

### `moderation_state` — `PhotoModerationState`

| Value | Meaning |
|---|---|
| `-2` | Admin rejected |
| `-1` | Rejected |
| `0` | Unset |
| `1` | Pending |
| `2` | Accepted |

### `violation` — `PhotoModerationViolationReason`

| Value | Reason | Value | Reason |
|---|---|---|---|
| `0` | Unset | `29` | Non-SCRUFF member visible |
| `5` | Nudity | `30` | No member visible |
| `10` | Crotch | `31` | Overlay |
| `11` | Sexual | `40` | Harassment |
| `12` | Impersonation | `41` | Drugs |
| `16` | Underage | `42` | Entrapment |
| `23` | No face pic | `43` | Commercial |
| `24` | Offensive | `44` | Spam |
| `27` | Close-up | `54` | Underwear |
| `28` | Collage | `115` | Other |

### `crop_source` — `ProfilePhotoCropSource`

| Value | Meaning |
|---|---|
| `0` | Unset |
| `1` | User |
| `2` | Face detection service |

---

## 8. Moderation and reporting

### `reason` in a banned-terms rejection — `BannedTermType`

Returned in the structured body of a `430` response.

| Value | Meaning |
|---|---|
| `0` | Unset |
| `1` | Prohibited |
| `2` | Pedo |
| `3` | Commercial |
| `4` | Profane |

### Profile report reasons

⚠️ **Profile reports have no client-side enum.** Unlike Moments, the reason list is
delivered by the server as a form definition and rendered dynamically — see
[scruff.md §14](./scruff.md#14-miscellaneous-client). The app contains the renderer and
the submission format, but not the reason vocabulary.

For orientation, the moderation reason *strings* shipped in the app (used for photo
rejections, and overlapping the report vocabulary) are: abuse/harassment, advertising or
solicitation, close-up, collage, commercial, crotch visible or emphasised, drugs,
entrapment, harassment, impersonation, inappropriate media, inappropriate/offensive
tap-in, no face visible, no member visible, non-SCRUFF member visible, visible nudity,
offensive, overlay on photo, sexual content, spam, spambot/scammer, underage, underwear,
unknown.

---

## 9. Alerts, events, and Venture

### `alert_type` — `ServerAlertType`

| Value | Type | Value | Type |
|---|---|---|---|
| `0` | System notice | `10` | Explorer |
| `1` | Warning | `11` | Suspended |
| `2` | Event | `12` | Upgrade required |
| `3` | Survey | `13` | Advanced survey |
| `4` | News | `14` | Discount |
| `5` | Account notice | `15` | Trial promotional offer |
| `6` | Promotion | `16` | Trial introductory offer |
| `7` | Tip | `17` | Paid boost |
| `8` | Free trial | `18` | In-grid |
| `9` | Travel warning | `19` | Local health |

### `navigation_type` — `ServerAlertNavigationType`

| Value | Target | Value | Target |
|---|---|---|---|
| `0` | Unset | `5` | Deep link |
| `1` | Store | `6` | Support |
| `2` | External browser | `7` | App store |
| `3` | Event | `8` | Profile editor |
| `4` | Advanced survey | `9` | Room listing |

### `display_location` / `aspect_ratio`

| `display_location` | Meaning | | `aspect_ratio` | Ratio |
|---|---|---|---|---|
| `0` | Tray | | `0` | 1 : 1 |
| `2` | Interstitial | | `1` | 1 : 1.5 |
| | | | `2` | 1 : 2 |

### `EventQuerySortType`

| Value | Meaning | Sent as |
|---|---|---|
| `0` | All | — |
| `1` | Popular | `hot=1` |
| `2` | Sponsored | `featured=1` |

`EventDistanceTier` is `0`–`7`.

### `category` on a trip — `TripCategory`

| Value | Meaning |
|---|---|
| `0` | Unset |
| `1` | Business |
| `3` | Vacation |
| `4` | Pride |
| `5` | Total *(aggregate, not a real category)* |

⚠️ `2` is retired and absent.

### `Currency`

| Value | Code | Value | Code |
|---|---|---|---|
| `0` | Unset | `4` | JPY |
| `1` | USD | `5` | CHF |
| `2` | GBP | `6` | AUD |
| `3` | EUR | `7` | CAD |

`RoomCost`: `0` Unset, `1` Free, `2` Paid. `RoomListingStyle`: `0` Unset, `1` Featured,
`2` Standard.

---

## 10. Verification and video chat

### `AgeVerificationStatus`

| Value | Meaning |
|---|---|
| `1` | Required |
| `2` | Underage |
| `3` | Pending |

### `source` on face liveness — `FaceLivenessSource`

| Value | Meaning |
|---|---|
| `1` | Profile verification |
| `2` | Sign-up |

### `status` on a video chat room — `VideoChatRoomStatus`

Strings, not integers: `"waiting"`, `"accepted"`, `"active"`, `"ended"`, `"failed"`.

### `CaptchaType`

| Value | Meaning |
|---|---|
| `0` | Web |
| `1` | API |

---

## 11. Discovery and UI

### `style` on a discover card — `DiscoverCardLayout`

`"even_six"`, `"big_corner"`, `"user_carousel"`, `"multi_tile"`, `"individual"`, `"alert"`.

### `stack_type` — `RecommendationStackType`

| Value | Meaning |
|---|---|
| `1` | New users nearby |
| `2` | Most woofed nearby |

### In-grid banner placements — `InGridBannerLocationTarget`

`"browse:nearby"`, `"browse:discover:see_more"`, `"browse:search"`, `"cruised:woofs"`,
`"cruised:viewers"`, `"discover:feed"`, `"cruised:recent:you_woofd"`,
`"cruised:recent:you_looked"`.

### `RemoteConfigChannel`

`"alpha"`, `"beta"`, `"prod"`.

### `TicketEditorType`

| Value | Meaning | Value | Meaning |
|---|---|---|---|
| `0` | Forgot email | `5` | Benevolads |
| `1` | Technical issues | `6` | Feedback |
| `2` | Violations and suspensions | `7` | Translation error |
| `3` | Paid SCRUFF Pro | `8` | GDPR |
| `4` | Guidelines violation | `9` | Moments |
| | | `100` | Gender identity or pronoun |

---

## 12. Push notifications

### `type` in a push payload — `PushNotificationType`

Strings: `unknown`, `woof`, `chat`, `match`, `matches_available`, `server_alert`,
`album_share`, `ticket_updated`, `video_chat`, `moment`, `moments_available`, `campaign`.

### Android notification channel ids

`100_chat`, `200_woof`, `301_album_share`, `400_match`, `500_matches_available`,
`600_support`, `700_news`, `900_file_download`, `1000_video_chat`, `1100_moment`,
`1200_campaign`.

---

## 13. Realtime `class` codes

The realtime channel's 87 event-class codes live in
[scruff-realtime.md §4](./scruff-realtime.md#4-class-codes-client), since they are only
meaningful alongside the frame envelope and the REST paths they correlate to.

---

## 14. HTTP status codes

SCRUFF overloads the 4xx private-use range with application-specific meanings, and that
vocabulary *is* its error model — there is no error body. The full table is in
[scruff.md §1.5](./scruff.md#15-errors-client).

---

## 15. Server-driven limits

Unlike Recon, SCRUFF ships **no default limits in the client**. Every page-size and
cap is delivered by remote config; when a key is absent the client simply omits the
parameter and lets the server decide. There is no fallback integer anywhere in the app.

Fetched from `GET /app/account/remote_config` → `{"beta": {…}, "production": {…}}`,
selected by `RemoteConfigChannel`.

| Remote-config key | Applies to |
|---|---|
| `limit_get_nearby_users` | `GET /app/location` (nearby) |
| `limit_get_search_users` | `GET /app/location` (search) |
| `limit_get_looks_v2` | `GET /app/viewers/incoming` |
| `limit_get_you_looked` | `GET /app/viewers/outgoing` |
| `limit_get_woofs_v2` | `GET /app/woofs/incoming` |
| `limit_get_you_woofd` | `GET /app/woofs/outgoing` |
| `limit_get_mutual_match_users_v2` | `GET /app/grid/matches_mutual` |
| `limit_get_match_stacks` | `GET /app/grid/matches` |
| `limit_get_inbox_chats` | `GET /app/inbox/stream` |
| `limit_get_chat_history_messages` | `GET /app/chat` |
| `limit_get_private_album_images` | `GET /app/albums/images` |
| `limit_hide_block_user_v2` | `POST /app/block` |
| `limit_add_favorite_user` | `POST` / `PUT /app/favorite` |
| `limit_add_share_album` | `POST /app/chat` (album share) |

⚠️ The last three are a **different kind of limit**. They are not page sizes — they are
the client *declaring the free-tier cap it is currently enforcing*, so the server can
return a matching `402`/`403` consistent with the client's own UI. See
[scruff.md §10](./scruff.md#10-social-relations).

The remaining 17 grid modules — albums received, unread/recent inbox, albums
unlocked-for, RSVP, favorites, hashtags, blocks, partner picker, all Venture grids, all
Moments grids, film voters — send **no `limit` at all**.

Values observed leaking through telemetry in one capture: `limit_get_nearby_users` 100,
`limit_hide_block_user_v2` 150, `limit_get_looks_v2` 25, `limit_get_woofs_v2` 15,
`limit_add_favorite_user` 80. Treat these as a snapshot, not a contract — fetch them.
