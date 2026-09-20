# Recon web app — full bundle decomposition

Static decomposition of the recon.com Angular bundle (`main.a600a03ce454f4ba.js` +
lazy chunks). All 179 REST calls + 4 PATCH live in `main`. Complements
`docs/recon-api.md` (captured traffic) with the complete, code-derived surface,
including endpoints never exercised at runtime and the exact send bodies.

## Base URLs (`baseUrls.<key>.route`, all `https://www.recon.com/api/<svc>/`)

`account`, `dvrt` (advertising), `antiAbuse`, `contentReview`, `discovery` (unused),
`event`, `feed`, `location`, `media`, `messaging`, `membership`, `payment`,
`profileRelations`, `profileSearch`, `profile`, `pushNotification`, `signalR`,
`verification`. Media CDN: `media.recon.t101api.com`.

Global interceptors add `culture=en` to every `/api/*` call, `deviceTypeId=1` to
media calls, `ngsw-bypass=true` where noted, and `Authorization: Bearer <jwt>`.

## Messaging — exact send/receive (authoritative)

**Send transport is REST POST.** The SignalR `SendChatMessage` hub method exists but
is dead code; the web UI sends via REST. SignalR is receive + typing only.

- **Text:** `POST messaging/profiles/{me}/conversations/{cid}/messages` JSON body
  `{ "text": "...", "files": [], "attachments": [] }`. The server assigns id,
  messageType, senderProfileId. (NOT `contentTypeId`.)
- **Image/media (inline, UI-proven):** `POST .../messages?imageSizes=100,{large}`
  as `multipart/form-data` with fields `text` (string), `attachments` (JSON id
  list, usually empty), and a **repeated `files` part** per binary image
  (JPG/GIF/BMP/PNG). Server stores bytes and returns the Message with populated
  `attachments[]`. GIFs use this same path (no Tenor/Giphy).
- **Image by reference:** JSON `{ "text": "...", "files": [], "attachmentIds": [<mediaFileId>...] }`
  referencing files already on the media service.
- **Start a DM:** `POST messaging/profiles/{me}/conversations` body
  `{ participants:[{ProfileId:<other>},{ProfileId:<me>}], isGroup:false, isOfficial:false, isArchived:false }`
  (note PascalCase `ProfileId`). On conflict, follow the `location` response header
  to the existing conversation. Lookup: `GET conversations?profileId={me}&otherProfileId={other}`.
- **History:** `GET .../conversations/{cid}/content?take={N}&imageSizes=100,{large}&before={ISO}`
  where **`before` = the `createdDate` (ISO) of the oldest message already held**
  (NOT an id or skip). Empty page = end.
- **Mark read:** `POST .../conversations/{cid}/logMessagesRead` body `{ "LastMessageReadDate": "<ISO>" }`.
- **Delete:** conversation `DELETE .../conversations/{cid}`; attachment
  `DELETE .../conversations/{cid}/attachments/{aid}`. **No per-message delete, no
  edit, no reactions** exist in Recon.
- **Typing:** SignalR `SendTypingStatus {profileId, isTyping, conversationId}`.

### Message DTO (`fromDto`)
`{ id, senderProfileId, text, attachments[], files[], isRead, messageType (0=normal), markupTypeId (official-only rendering hint), createdDate }`.
Attachment item: `{ id, mediaMetadataId, thumbnailUrl, files:[{url}] (small→large), downloadUrl, isRestricted }`.
Conversation DTO: `{ id, name, isGroup, isOfficial, isArchived, isReadOnly, lastActivityDate, participantCount, participants:[{profileId, profileUrl}], lastMessageExcerpt, userInfo:{unreadMessageCount}, thumbnailMediaMetadataId }`.

### Realtime SignalR (receive)
`ReceiveMessage {conversationId, messageId, senderProfileId, messageText, sentDate, attachmentCount}` — if `attachmentCount>0`, hydrate via `GET .../messages/{messageId}?imageSizes=...`. Also `NewConversationCreated`, `ReceiveReadReceipt {conversationId, readByEveryoneDate}`, `ReceiveTypingStatus`, `ReceiveUnreadConversationCount`.

## Complete non-messaging endpoint surface (by service)

**account/auth:** `POST accounts/authenticate`; `POST accounts/{aid}/sessions/{sid}/refreshTokens`; `DELETE accounts/{aid}/sessions/{sid}` (logout); register/`register/complete`/`passwordValidation`; `emailConfirmations/{token}/validate`+`/confirmEmailAddress`; `changePassword`/`changeEmailAddress`; `generatePasswordResetLink`/`{aid}/resetPassword`/`passwordResets/{token}/emailAddress`; `deactivate`/`delete`; `GET appSettings` + `GET accounts/{aid}/appSettings`; `POST appInstallations` + `PUT appInstallations/{id}`; `GET/PUT accounts/{aid}/preferences`; GDPR DSAR endpoints.

**profile:** `GET profiles/{id}/{version|1}` (+ noCache / includeAllProperties variants); `PUT/PATCH profiles/{id}`; enum lookups `bodyhairs, bodytypes, ethnicities, interests, positions, safesexes, roles, validHeightsCm`.

**profileRelations:** blocks (`PUT/DELETE profiles/{me}/blocks/{id}`, `GET blocked`), cruises (`PUT/DELETE cruisedProfiles/{id}`, `GET cruisedProfiles`, `cruisedByProfiles`+`/count`, `logCruisedByViewed`), visits (`PUT visitedProfiles/{id}`, `GET visitorProfiles`+`/count`, `logVisitorsViewed`), friends (`PUT/DELETE/GET friendRequests`, `friendRequestResponse/{id}/{accept|reject}`, `DELETE friends/{id}`), followers (`PUT/DELETE followers/{id}`, counts), prefs (`GET/PUT/PATCH preferences`), v2Favourites import.

**profileSearch:** `GET profiles?myProfileId={me}&sortProperty=distance&<filter>` (nearby/explore/name), `profiles/name/{username}`, `defaultFilters` (GET/PUT/DELETE), friends/followers/followings (+counts), event attendees.

**feed:** `GET profiles/{pid}/feeds/notifications`, `.../feeds/home`, `POST logNotificationsViewed`.

**media:** galleries (`GET profiles/{id}/galleries[/{gid}]`), `fileProperties` variants, `POST profiles/{me}/files` (upload), `POST conversations/{cid}/files` (chat file — defined, UI uses inline), `DELETE conversations/{cid}/files/{fid}`, `PUT galleries/{gid}/files/{fid}/position`, `DELETE galleries/{gid}/files/{fid}`, blob fetch `{fileUrl}`, event gallery.

**membership:** `GET profiles/{pid}/membershipStatus`. **payment:** products, orders (create/initiate/status), `DELETE recurringBilling`. **event:** list (upcoming/past), `{eventId}`, RSVP `PUT attendees/{pid}`, v2→v3 id. **location:** `PUT profiles/{pid}/geolocation`, distance, location detail. **verification:** singleUseTokens, verificationRequirement, verifications (start/getStatus), verificationStats. **pushNotification:** `PUT/DELETE appInstallations/{id}/deviceToken` (FCM). **antiAbuse:** `POST profileNameChecks`. **contentReview:** `GET reportCategories`, `POST profiles/{pid}/report`. **advertising/dvrt:** dvrts, contentStats, news, bulk/official messages (`GET profiles/{me}/bulkMessages`, read/delete).

## Connector-relevant corrections vs earlier notes
- Send body is `{text, files, attachments}` (or `attachmentIds`), never `contentTypeId`.
- Pagination cursor is `before=<createdDate ISO>`, not skip/offset.
- Recon has **no reactions, no message edits, no per-message deletes** — map those Beeper capabilities to no-ops.
- Report/block/friend/follow/cruise/visit are available if the connector wants to expose moderation or presence-adjacent features.
