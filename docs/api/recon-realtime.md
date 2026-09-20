# Recon realtime — SignalR reference

Recon's push channel is a standard **ASP.NET Core SignalR 8.0** hub over WebSocket, using
the JSON hub protocol. Frames are JSON objects separated by the ASCII record separator
`0x1E`.

**Sources.** The handshake, negotiate response, and several server events were captured
live; the full hub surface and the exact payload field names were read out of the web
client bundle. Tags follow the convention in [`recon.md`](./recon.md#provenance-tags):
**[observed]** on the wire, **[client]** from client source.

**Realtime is receive-only in practice.** The hub exposes a `SendChatMessage` method, but
the web client never calls it — messages are sent over REST. Use SignalR for inbound
messages, typing, read receipts, and unread counts; use REST for everything you initiate.
The one exception is typing, which *is* sent over the hub.

---

## 1. Connecting

### 1.1 Negotiate **[observed]**

```http
POST https://www.recon.com/api/signalR/hubs/signalr/negotiate
       ?access_token=<bare jwt>&negotiateVersion=1&ngsw-bypass=true
Content-Length: 0
X-Requested-With: XMLHttpRequest
X-SignalR-User-Agent: Microsoft SignalR/8.0 (8.0.17; Unknown OS; Browser; Unknown Runtime Version)
```

⚠️ **No `Authorization` header.** SignalR authenticates purely from the `access_token`
query parameter, and that value must be the **bare JWT with no `Bearer ` prefix**. This
is the opposite of the REST convention — see
[recon.md §3.4](./recon.md#34-attaching-the-token-and-the-bearer-bearer-trap).

`200 OK`:

```jsonc
{
  "negotiateVersion": 1,
  "connectionId": "<22-char base64url>",
  "connectionToken": "<22-char base64url>",   // ← distinct from connectionId
  "availableTransports": [
    { "transport": "WebSockets",       "transferFormats": ["Text", "Binary"] },
    { "transport": "ServerSentEvents", "transferFormats": ["Text"] },
    { "transport": "LongPolling",      "transferFormats": ["Text", "Binary"] }
  ]
}
```

⚠️ Because `negotiateVersion: 1`, you must pass **`connectionToken`** — not
`connectionId` — as the `id` parameter on the WebSocket URL. Mixing them up is a silent
connection failure.

The response also sets a `v3_Signalr` cookie. The browser client deletes it immediately
after the connection starts; it is not needed.

### 1.2 WebSocket upgrade **[observed]**

```http
GET wss://www.recon.com/api/signalR/hubs/signalr
      ?access_token=<bare jwt>&id=<connectionToken>&ngsw-bypass=true
Sec-WebSocket-Version: 13
Sec-WebSocket-Extensions: permessage-deflate; client_max_window_bits
Origin: https://www.recon.com
```

→ `101 Switching Protocols`.

The JWT appears **only in the upgrade URL**. There is no in-band re-authentication, so
when the 15-minute token expires you must obtain a fresh one, re-negotiate (the
`connectionToken` is single-use), and reconnect.

### 1.3 Handshake **[observed]**

Frames below are shown with `␞` standing in for the `0x1E` terminator.

```
C→S   {"protocol":"json","version":1}␞
S→C   {}␞
```

The server's `{}` is the handshake acknowledgement. It arrives as a raw binary frame
containing the three bytes `7b 7d 1e`. An error would instead come back as
`{"error":"…"}`.

### 1.4 Join **[observed]**

Immediately after the handshake:

```jsonc
C→S   {"type":1,"target":"JoinConversations","invocationId":"0",
       "arguments":[{"profileId":"<uuid>","accessToken":"Bearer <jwt>"}]}␞
```

⚠️ **The `accessToken` hub argument DOES take the `"Bearer "` prefix**, even though the
`access_token` query parameter on the same connection must not. Both forms appear on one
connection. This asymmetry is the second half of the `Bearer Bearer` trap.

The server replies with a burst:

```jsonc
S→C   {"type":1,"target":"JoinedConversations","arguments":[{"data":[],"totalRecords":null}]}␞
S→C   {"type":1,"target":"ReceiveUnreadConversationCount","arguments":[{"count":85}]}␞
S→C   {"type":3,"invocationId":"0","result":null}␞
```

ℹ️ `JoinedConversations` is a **free full conversation sync** pushed right after joining.
It carries the same envelope as the REST conversation list. Many clients discard it; if
you handle it you can skip an initial REST fetch. In the capture it arrived empty, so
treat `data` as possibly-empty.

---

## 2. Frame protocol

Every frame is JSON terminated by `0x1E`. A single WebSocket message may contain several
frames, so split on the separator rather than assuming one frame per message.

| `type` | Meaning | Handling |
|---|---|---|
| `1` | Invocation | Dispatch on `target` |
| `3` | Completion | Result of one of your invocations, matched by `invocationId` |
| `6` | Ping | Reply with `{"type":6}` |
| `7` | Close | Server is closing; reconnect |

**Invocation shape:**

```jsonc
{ "type": 1, "target": "MethodName", "invocationId": "0", "arguments": [ { /* … */ } ] }
```

`invocationId` is optional. The web client omits it on fire-and-forget calls
(`SendTypingStatus`) and sets it on `JoinConversations`, which is how it gets the
`type: 3` completion above.

**Keepalive.** The client sends `{"type":6}` every **15 seconds**; the server's timeout
is 30 seconds. Respond to inbound pings with the same frame.

**Read limit.** Allow a generous frame size; the reference client caps reads at 10 MiB.

---

## 3. Client → server

All four hub methods below are **[client]**-sourced except `JoinConversations`, which was
observed.

| Target | Arguments | Notes |
|---|---|---|
| `JoinConversations` | `{profileId, accessToken}` — single object | **[observed]** Required after every connect |
| `JoinConversation` | `{profileId, conversationId, accessToken}` — single object | Call after `NewConversationCreated` |
| `SendTypingStatus` | `{profileId, isTyping, conversationId}` — single object | Fire-and-forget |
| `SendChatMessage` | `(profileId, <arg>, <arg>, new Date().toISOString())` — **positional** | ⚠️ Dead code in the web client; send over REST instead |

⚠️ **Calling conventions are inconsistent.** Three methods take a single object argument;
`SendChatMessage` takes positional arguments. Since `SendChatMessage` is unused and its
middle two parameters could not be resolved from the minified bundle, treat it as
unusable and send via `POST messaging/…/messages`.

Every object-form method repeats `accessToken` **with** the `"Bearer "` prefix.

---

## 4. Server → client

| Target | Payload | Tag |
|---|---|---|
| `JoinedConversations` | `{data: [Conversation], totalRecords: int\|null}` | **[observed]** |
| `ReceiveMessage` | see [§4.1](#41-receivemessage) | **[client]** |
| `ReceiveTypingStatus` | `{conversationId, profileId, isTyping}` | **[client]** |
| `NewConversationCreated` | `{conversationId}` | **[client]** |
| `ReceiveReadReceipt` | `{conversationId, readByEveryoneDate}` | **[observed]** |
| `ReceiveUnreadConversationCount` | `{count: int}` | **[observed]** |
| `ReceiveUnreadCruisesCount` | `{count: int}` | **[client]** |
| `ReceiveUnreadVisitsCount` | `{count: int}` | **[client]** |
| `ReceiveUnreadNotificationsCount` | `{count, newNotification: {isSelfActor, feedItem}, removedNotificationIds}` | **[client]** |

### 4.1 `ReceiveMessage`

```jsonc
{
  "conversationId": "<uuid>",
  "messageId": "<uuid>",
  "profileId": "<uuid>",     // ⚠️ the SENDER. Not `senderProfileId`.
  "messageText": "…",        // ⚠️ not `text`
  "sentDate": "<iso8601>",
  "attachmentCount": 0
}
```

⚠️ **The most common Recon integration bug.** The realtime payload names the sender
`profileId` and the body `messageText`, while the REST message DTO uses
`senderProfileId` and `text` for the same two concepts. The two transports genuinely
disagree; a client that reuses one struct for both will silently get an empty sender or
empty body on every realtime message.

This is settled, not guessed. The web client's `ReceiveMessage` handler **renames the
fields as it maps them**: it reads the event's `profileId` into its own
`senderProfileId` property, and the event's `messageText` into its own `text` property.
So `senderProfileId` and `text` are the client's *internal* names, and `profileId` /
`messageText` are what actually cross the wire. The sibling `ReceiveTypingStatus`
handler reads `profileId` off its event too, corroborating the naming.

⚠️ **The payload is lightweight and carries no attachments.** When `attachmentCount > 0`
you must hydrate over REST:

```http
GET /api/messaging/profiles/{me}/conversations/{cid}/messages/{messageId}?culture=en&imageSizes=100,104
```

ℹ️ A downstream quirk worth knowing if you are porting the web client's logic: its
`Message.fromSocket` mapper stuffs the **conversation** id into the message's `id` field
and keeps the real message id in `messageId`. That is a client-side wart, not a protocol
feature.

### 4.2 `NewConversationCreated`

```jsonc
{ "conversationId": "<uuid>" }
```

You are **not** automatically subscribed to conversations created after you joined. On
receipt, invoke `JoinConversation` with the new id, then fetch the conversation over REST
if you need its metadata. Skipping this means silently missing all messages in that
thread.

### 4.3 `ReceiveReadReceipt` **[observed]**

```jsonc
{ "conversationId": "<uuid>", "readByEveryoneDate": "2026-09-13T18:06:37.76Z" }
```

A watermark, not a per-message flag: every message in the conversation with
`createdDate <= readByEveryoneDate` has been read by the other participant.

### 4.4 Unread counters **[observed / client]**

`ReceiveUnreadConversationCount` fires on connect and on every change (85 → 84 after a
read in the capture). The cruises, visits, and notifications counters follow the same
`{count}` shape and are passed through verbatim by the client.

---

## 5. Reconnection and token expiry

SignalR's own automatic-reconnect is **not** used by the web client, and it cannot be:
the JWT is embedded in the negotiate and upgrade URLs, so a reconnect needs a fresh
handshake.

The loop is:

1. On close or read error, wait a backoff interval.
2. Ensure a valid access token (refresh if within the 3-minute grace window).
3. `POST …/negotiate` again — `connectionToken` is single-use.
4. Reopen the WebSocket, redo the protocol handshake, re-invoke `JoinConversations`.

The web client polls this every `signalRRefreshTimeMs` (15 000 ms) and calls
`refreshJwt()` when the token is invalid. Its connection-state enum is
`0` disconnected, `1` connected, `4` reconnect-pending, `5` connecting.

⚠️ **Frames can be lost across a reconnect.** The hub has no backlog, no acknowledgements,
and no resume token. After reconnecting, reconcile over REST — re-fetch the conversation
list and any thread whose `lastActivityDate` moved. Treat SignalR as a low-latency
*trigger*, with REST as the source of truth.

Practical defaults: 15 s ping, exponential backoff with jitter starting around 5 s,
re-negotiate every attempt.

---

## 6. Worked example

A complete minimal session:

```
POST /api/signalR/hubs/signalr/negotiate?access_token=<jwt>&negotiateVersion=1&ngsw-bypass=true
  → {"connectionToken":"abc…", …}

GET  wss://www.recon.com/api/signalR/hubs/signalr?access_token=<jwt>&id=abc…&ngsw-bypass=true
  → 101

C→S  {"protocol":"json","version":1}␞
S→C  {}␞
C→S  {"type":1,"target":"JoinConversations","invocationId":"0",
      "arguments":[{"profileId":"<me>","accessToken":"Bearer <jwt>"}]}␞
S→C  {"type":1,"target":"JoinedConversations","arguments":[{"data":[],"totalRecords":null}]}␞
S→C  {"type":1,"target":"ReceiveUnreadConversationCount","arguments":[{"count":85}]}␞
S→C  {"type":3,"invocationId":"0","result":null}␞

     … every 15 s …
C→S  {"type":6}␞

     … on inbound traffic …
S→C  {"type":1,"target":"ReceiveMessage","arguments":[{"conversationId":"…","messageId":"…",
      "profileId":"<sender>","messageText":"hi","sentDate":"…","attachmentCount":0}]}␞
```

---

## 7. Known gaps

| Gap | Why |
|---|---|
| **`ReceiveMessage` on the wire** | Never fired during the capture — no message arrived in those four sessions. Field names are bundle-derived but corroborated by the sibling typing handler. |
| **`SendChatMessage` arguments 2 and 3** | Unresolvable in the minified bundle. Dead code in the client; use REST. |
| **`JoinedConversations` with data** | Observed only as an empty array. |
| **Attachment hydration payload** | Follows from the REST single-message endpoint, itself unobserved with non-empty attachments. |
| **Error frames** | No `{"error":…}` handshake failure or `type: 7` close was captured. |
| **`ReceiveUnread*Count` siblings** | Only the conversation counter was observed; the other three are bundle-derived. |
