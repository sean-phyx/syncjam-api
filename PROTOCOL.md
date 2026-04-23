# SyncJam Protocol v1

Shared-playback sync for self-hosted Subsonic-compatible music
servers (Navidrome, Airsonic, Astiga, etc.). Sidecar service that
coordinates a group of clients so every member of a session hears
the same song at the same position.

## Design principles

1. **Stateless relative to the media server.** SyncJam never stores
   track data, library metadata, or audio bytes. It relays control
   messages between clients who already have their own connection to
   the media server.
2. **No new identity system.** Authentication delegates to the user's
   media server. SyncJam only retains the verified user identity for
   the duration of a WebSocket connection.
3. **Single media server per deployment.** The server URL is baked
   into the sidecar via the `SUBSONIC_URL` env var. Clients on
   different Subsonic servers need separate SyncJam deployments.
   This closes an SSRF vector (clients can't coerce auth lookups at
   arbitrary URLs) and matches the realistic "friends share one
   Navidrome" use case.
4. **In-memory state.** Session state lives in the server process
   only. Restart = all sessions end. Listening sessions are
   ephemeral by nature.

## Transport

A single WebSocket per client, at `GET /ws`. All messages are JSON
objects with a `type` field discriminating the message kind. All
messages must parse as valid JSON and include a non-empty `type`.

## Authentication

The first message over a new WebSocket must be `hello`. The server
does not accept any other message until authentication succeeds.

### `hello` (client → server)

```json
{
  "type": "hello",
  "auth": { /* see below */ }
}
```

**Subsonic credentials, password-derived:**

```json
"auth": {
  "username": "alice",
  "token": "<md5(password + salt)>",
  "salt": "<random-hex>"
}
```

**Subsonic credentials, API-key:**

```json
"auth": {
  "apiKey": "<navidrome-api-key>"
}
```

The sidecar verifies by calling `GET {SUBSONIC_URL}/rest/ping.view`
with the provided credentials as query parameters. If the Subsonic
response status is `ok`, auth succeeds and the server records the
username (or a truncated API-key fingerprint for the key form).

### `hello_ack` (server → client)

```json
{
  "type": "hello_ack",
  "userId": "<broker-local-user-id>",
  "displayName": "alice"
}
```

`userId` is a SyncJam-local opaque identifier used in subsequent
`member_*` messages. It doesn't leak the backend username.

### `error` (server → client)

```json
{
  "type": "error",
  "code": "auth_failed" | "invalid_message" | "session_not_found" | "not_in_session" | "backend_unreachable" | "not_authenticated",
  "message": "<human-readable>"
}
```

`error` in response to `hello` causes the server to close the
WebSocket after sending.

## Session lifecycle

A session is a short-lived coordination context for a group of
authenticated clients. Since all authenticated clients share the
sidecar's single configured media server, sessions don't carry any
backend or server-URL metadata.

### `create_session` (client → server)

```json
{ "type": "create_session" }
```

### `session_created` (server → client)

```json
{
  "type": "session_created",
  "sessionId": "<uuid>",
  "inviteCode": "XYZ9QR",
  "state": { /* SessionState */ },
  "members": [ { "userId": "...", "displayName": "..." } ]
}
```

The creator is automatically a member.

### `join_session` (client → server)

```json
{
  "type": "join_session",
  "inviteCode": "XYZ9QR"
}
```

Invite codes are 6-char URL-safe tokens, case-insensitive.

### `session_joined` (server → joining client)

Same shape as `session_created`.

### `member_joined` / `member_left` (server → other members)

```json
{ "type": "member_joined", "userId": "...", "displayName": "..." }
```

```json
{ "type": "member_left", "userId": "..." }
```

When the last member leaves, the session is destroyed.

### `leave_session` (client → server)

```json
{ "type": "leave_session" }
```

## Playback state

### SessionState payload

```json
{
  "trackId": "<subsonic-track-id>",
  "positionMs": 87500,
  "playing": true,
  "anchorServerTimeMs": 1710000000000
}
```

Clients compute the current playback position as:

- If `playing`: `currentPositionMs = positionMs + (serverTimeNow - anchorServerTimeMs)`
- If `!playing`: `currentPositionMs = positionMs`

`serverTimeNow` is the client's *local* clock adjusted by its
server-clock offset (see clock sync below).

### `play`, `pause`, `seek`, `track_change` (client → server)

Any member may propose a state change. The server accepts the
change, stamps it with its own `anchorServerTimeMs`, and broadcasts
`state_changed` to all members.

```json
{ "type": "play", "positionMs": 87500 }
```

```json
{ "type": "pause", "positionMs": 89200 }
```

```json
{ "type": "seek", "positionMs": 140000 }
```

```json
{ "type": "track_change", "trackId": "...", "positionMs": 0 }
```

### `state_changed` (server → all members)

```json
{
  "type": "state_changed",
  "state": { /* SessionState */ },
  "issuedBy": "<userId>"
}
```

`issuedBy` lets clients show "alice paused" style UI. Clients apply
the new state regardless of who issued it (the issuer echoes it
back as a confirmation).

## Clock synchronisation

Clients maintain a running estimate of `serverTime - clientTime` so
they can interpret `anchorServerTimeMs` against their local clock.

### `clock_ping` (client → server)

```json
{ "type": "clock_ping", "clientSendTimeMs": 1710000001234 }
```

### `clock_pong` (server → client)

```json
{
  "type": "clock_pong",
  "clientSendTimeMs": 1710000001234,
  "serverRecvTimeMs": 1710000001298,
  "serverSendTimeMs": 1710000001299
}
```

Client computes offset on receipt:

```
clientRecvTimeMs = Date.now()
roundTripMs      = clientRecvTimeMs - clientSendTimeMs
oneWayLatencyMs  = roundTripMs / 2
serverMidpointMs = (serverRecvTimeMs + serverSendTimeMs) / 2
offsetMs         = serverMidpointMs - (clientSendTimeMs + oneWayLatencyMs)
serverTimeNow    = Date.now() + offsetMs
```

Client should ping every ~3 seconds and maintain a sliding window
of recent offsets; use the median for stability. Reject samples
with `roundTripMs` > 2s.

## Errors

All errors use the `error` message type. Error codes:

| Code | Meaning |
|---|---|
| `auth_failed` | `hello` creds rejected by the Subsonic server |
| `backend_unreachable` | Subsonic server returned 5xx or was unreachable during auth |
| `invalid_message` | Message didn't parse or didn't match schema |
| `not_authenticated` | Client sent a message before `hello` succeeded |
| `session_not_found` | Invite code didn't match any active session |
| `not_in_session` | Playback command sent without being in a session |

## Versioning

This document describes protocol v1. Breaking changes bump the major
version and ship at a different WebSocket path (`/ws/v2`).
Backwards-compatible additions can land on `/ws` without a version
bump; clients should ignore unknown message types and fields.

## Non-goals for v1

- Jellyfin or non-Subsonic backends. The architecture supports
  adding a new backend later (add a verifier port + implementation,
  branch in the hello handler), but v1 is Subsonic-only.
- Queues / playlists.
- Voice or text chat.
- Recording session history.
- Cross-server sessions (clients on different Subsonic servers).
- Per-member permissions (read-only members etc).
- End-to-end encryption of control messages (TLS at transport layer
  via a reverse proxy is the expected deployment).
