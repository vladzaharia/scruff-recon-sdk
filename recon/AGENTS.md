# AGENTS.md — sdk/recon

Go client for the Recon (recon.com) v3 API. Protocol reference:
[`docs/api/recon.md`](../../docs/api/recon.md).

## Read this before touching the auth code

**`authenticate` and `refreshTokens` return `accessToken` already prefixed with
`"Bearer "`.** Adding your own prefix produces `Bearer Bearer eyJ…`, and then:

- the **profile** service accepts it — so a first smoke test passes;
- the **messaging** and **SignalR** services reject it with an **empty-body 401**.

`stripBearer` is applied both on store and on read. Do not "simplify" it away. There is a
test for every variant; if you change the auth path, run it.

The asymmetry continues into realtime: the SignalR `access_token` **query parameter**
takes the **bare** JWT, while the `JoinConversations` hub **argument** takes it **with**
the prefix. Both forms appear on one connection. This is not a bug in our code.

## Other things that will bite

| Trap | Reality |
|---|---|
| `applicationId` | Must be the **integer `3`**. A UUID makes the server reject the whole request model — an easy mistake since nearly every other id here is a UUID. |
| Pagination | `before=<ISO createdDate of the oldest message you hold>`. There is **no** `skip`/`offset`/`page`. |
| `totalRecords` on history | The size of the **returned page**, not the conversation total. Never use it for paging arithmetic. We deliberately don't return it from `Messages`. |
| Message type field | The wire field is **`contentTypeId`**. `messageType` is what the web client *writes* on locally-constructed outgoing messages and never receives — unmarshalling it silently yields 0. |
| Realtime vs REST naming | Realtime sends `profileId` + `messageText`; REST sends `senderProfileId` + `text`, for the same two concepts. `MessageEvent` normalises to the REST spelling. |
| Image sizes | Only **100–104** exist. `661` in a profile URL is a **version** path segment, not a size. |
| `participants[]` | **Excludes you.** Length is `participantCount - 1`, so a 1:1 thread has exactly one entry: the peer. `Conversation.Peer()` relies on this. |
| PascalCase bodies | `{"ProfileId": …}` on create-conversation and `{"LastMessageReadDate": …}` on mark-read are the **only** two non-camelCase bodies in the API. They are correct. |
| `NewConversationCreated` | You are **not** auto-subscribed to threads created after you joined. `Realtime` invokes `JoinConversation` for you; without that, every message in the thread is silently missed. |

## Capabilities Recon does not have

There is **no reaction API, no message edit, and no per-message delete**. Only whole
conversations and individual attachments can be deleted. Do not add methods for these;
bridges should map those capabilities to no-ops.

## Base URL discipline

Every network call must honour `WithBaseURL`. The unauthenticated helpers
(`CreateAppInstallation`, `Authenticate`, `RefreshTokens`, `AppSettingsAnonymous`) take
`...Option` for exactly this reason — an earlier version hardcoded the production URL,
and a unit test pointed at `httptest` **dialled recon.com for real**. If you add a
function that builds its own transport, route it through `unauthTransport(cfg)`.

## Testing

- `go test ./...` — unit tests, no network. Must stay that way.
- `go test -tags integration ./... -v` — staged smoke test against a real account, gated
  on credentials. Read-only unless `SMOKE_ALLOW_WRITES=1`.

The integration test is the only place that verifies **token refresh**, because that
endpoint was derived from the web client and has never been observed in a capture. If
stage `11_token_refresh` fails with an auth error, the inferred path is likely wrong —
treat that as a finding, not a flaky test.

## Do not

- Log anything. No logger, no `fmt.Print`. The bridge owns logging.
- Add `SendChatMessage` over SignalR. It exists on the hub but is dead code in the web
  client, its middle arguments are unresolvable from the minified bundle, and sending
  over REST works.
