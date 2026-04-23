# SyncJam

> **Status: v0.1.0 — alpha. Protocol is draft. No production consumers
> exist yet. End-to-end sync has not been empirically verified. Don't
> rely on this for anything you care about.**

Shared-playback sync broker for self-hosted Subsonic-compatible music
servers (Navidrome, Airsonic, Astiga, …). Meant to sit alongside your
media server; clients that implement the SyncJam protocol create
listening sessions where every member hears the same track at the
same position.

The goal is a sidecar any Subsonic client can integrate once the
protocol settles. Today, no client speaks SyncJam — the reference
consumer (Feishin fork or PR) is pending. A single-file browser test
client ships in `test-client/` to prove the protocol end-to-end.

## What it is

- **Control plane only.** No audio, no library, no track metadata.
  A WebSocket broker that relays play/pause/seek commands and keeps
  clients on a synchronised timeline via a clock-offset exchange.
- **No user system.** Auth delegates to the Subsonic server's
  `ping.view`. No credentials stored.
- **Single server per deployment.** Configured via `SUBSONIC_URL` at
  deploy time. Clients on different servers need separate deployments.
- **In-memory state.** Sessions live in the process. Restart = sessions
  end. Listening sessions are ephemeral.

## Architecture

Clean Architecture, Go:

```
cmd/syncjam/              entry point, DI wiring
internal/
  domain/                 entities + sentinel errors, stdlib only
  application/            Broker use case + Verifier/Notifier ports
  infrastructure/
    subsonic/             Subsonic ping.view verifier (implements port)
  delivery/
    websocket/            WS handler, per-connection client, registry
```

`infrastructure` and `delivery` both implement interfaces defined in
`application`. `application` depends only on `domain`. `domain` has
no internal dependencies.

## Running

```bash
SUBSONIC_URL=http://navidrome:4533 go run ./cmd/syncjam
# or
docker build -t syncjam .
docker run -p 8787:8787 -e SUBSONIC_URL=http://navidrome:4533 syncjam
```

See `docker-compose.example.yml` for Navidrome + SyncJam side-by-side.

## Configuration

| Env var | Default | Required | Meaning |
|---|---|---|---|
| `SUBSONIC_URL` | — | yes | Base URL of the Subsonic server auth is delegated to. |
| `PORT` | `8787` | | HTTP + WS listen port. |
| `ALLOWED_ORIGINS` | *(any)* | | Comma-separated WS origin patterns. Empty = accept any origin. Set this if exposing publicly. |

## Smoke test

`test-client/index.html` is a standalone browser client. Open two
tabs, authenticate both against the Subsonic server SyncJam is pointed
at, create a session in one and paste the invite code into the other.
Playing the `<audio>` element in either tab should propagate to the
other within ~100-250ms after the clock-offset loop settles (~10s).

If you run this end-to-end and hit a bug, opening an issue with the
browser logs and server log tail is more useful than any other
contribution you could make right now.

## Protocol

See [`PROTOCOL.md`](./PROTOCOL.md) for the wire spec. Summary of a
client lifecycle:

1. Open WS to `wss://<syncjam>/ws`.
2. Send `hello` with the user's Subsonic credentials.
3. On `hello_ack`, start a `clock_ping` loop (~3s interval) to
   estimate `serverTime - clientTime`.
4. `create_session` or `join_session` by invite code.
5. On local play/pause/seek, send the matching message; on inbound
   `state_changed`, apply it to the local audio element.

## License

MIT.
