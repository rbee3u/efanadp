# Architecture Notes

This document captures behavior that is easy to lose when only reading the
README. Keep it synchronized with code changes that affect runtime flow,
package boundaries, relay behavior, or operational limits.

## Package Responsibilities

- `cmd/efanadp`: process entry point.
- `internal/app`: command-line dispatch and manual dependency wiring. With no
  arguments it launches the TUI. The `config-reference` subcommand regenerates
  the committed config template.
- `internal/config`: YAML config parsing, template UUID insertion,
  committed template generation from private config input, runtime fields, and
  node list construction.
- `internal/tui`: Bubble Tea model, node list, running view, connection table,
  persisted settings, logging sink, and service lifecycle.
- `internal/proxy`: SOCKS5 server, connection events, connection stats,
  multipath TCP relay, UDP_ASSOCIATE, UDP target caching, and local SOCKS UDP
  envelopes.
- `internal/x365`: X365 node parsing, request construction, frame bytes,
  multipath stream setup, direct UDP stream setup, xhttp + REALITY transport,
  and Shadowsocks target sniffing.
- `internal/human`: small formatting helpers shared by UI and maintenance
  commands.

## Runtime Flow

```text
cmd/efanadp.main
  -> app.Run
  -> app command dispatcher
  -> tui.Run
    -> load settings from settings.json in the current working directory
    -> resolve --config/-c, settings configPath, or config.yaml
    -> config.Load resolved YAML file
    -> select node
    -> x365.Client
    -> proxy.ServeSOCKS
      -> accept SOCKS5 clients
      -> CONNECT through multipath TCP, or UDP_ASSOCIATE through direct X365 UDP
```

The TUI has two primary states:

- Node list: choose a parsed `x365://` node.
- Running: show current service metadata and active connection counters.

When switching nodes, the current service keeps running until the new node is
confirmed. Config file changes are applied by restarting `efanadp`.

## Config And Settings

`config.Load` decodes YAML config values and parses:

- Plain `proxies` maps from decrypted configs.
- Committed `proxies_tmpl` + `proxies_uuid` templates from
  `config.reference.yaml`.
- Optional runtime fields `listen` and `log`.
- Optional `sniff.shadowsocks` entries.

The TUI stores only the last successful config path and node name in
`settings.json` in the current working directory. It does not persist node
contents. The startup config path priority is explicit CLI path, then settings,
then `config.yaml`.

## SOCKS5 Server

`proxy.ServeSOCKS` listens on the configured TCP address and handles each client
connection in its own goroutine. The TUI wires an `x365.Client` into
`proxy.Config` as the upstream for TCP and UDP streams.

Supported commands:

- `CONNECT`: relays TCP-like streams through the recovered multipath path.
- `UDP_ASSOCIATE`: binds a local UDP socket and relays non-443 datagrams through
  direct X365 UDP streams.

Unsupported behavior:

- SOCKS authentication methods other than no-auth.
- SOCKS5 UDP fragments (`FRAG != 0`).
- UDP/443, because the observed APK avoids forwarding QUIC through X365 UDP.
- Local mihomo fake IP targets in `198.18.0.0/15`.

When a fake IP target is seen, the adapter rejects the request and points direct
testers toward `socks5h://`, which sends the domain name through SOCKS instead
of resolving it locally.

## TCP Relay Path

For SOCKS5 `CONNECT`, the adapter does not open X365 directly to the requested
target. It:

1. Opens an xhttp + REALITY stream to the node.
2. Sends an X365 TCP command to `multipath.internal:1`.
3. Reads the 5-byte `X365 0x00` success response.
4. Sends a multipath hello frame with a random stream ID and the real target.
5. Relays application bytes through multipath data frames.

Inbound data frames are ACKed by sequence number plus payload length. Close
frames terminate reads with `io.EOF`. Retransmit and window behavior are kept to
the observed minimum needed for stable SOCKS TCP relay.

## UDP Relay Path

For SOCKS5 `UDP_ASSOCIATE`, the adapter:

1. Creates a local UDP relay socket tied to the TCP association.
2. Parses SOCKS5 UDP envelopes from the client.
3. Drops fragments, fake IP targets, and UDP/443.
4. Opens one cached direct X365 UDP stream per target host/port.
5. Sends only the datagram payload through the X365 stream.
6. Rebuilds SOCKS5 UDP envelopes around response payloads.

The direct X365 UDP stream sends command `0x02` to the actual UDP target and
frames each datagram as `uint16_be(payload_len) payload`.

## Transport

Every X365 stream is carried over xhttp + REALITY:

- TCP dial goes to the node server and port.
- REALITY uses uTLS and patches the ClientHello session ID.
- HTTP/2 sends a `POST` to `https://<host><path>`.
- `Content-Type`, `User-Agent`, `Referer`, and `Host` mirror the observed client
  shape described in `x365-protocol.md`.

The HTTP response must be `200 OK`; after that, the request body and response
body are treated as a bidirectional byte stream.

## Observability

The `proxy` package emits `Event` values for open, update, and close.
The TUI consumes these events to render active/closed counts and connection
rows. Logs use the same `source` and `target` terms as the table.

Optional Shadowsocks sniffing can update a target from:

```text
landing-host:port
```

to:

```text
landing-host:port->inner-host:port
```

This is display-only. Relay behavior continues even when sniffing is absent or
fails.

## Maintenance Notes

- Keep protocol byte layouts in `x365-protocol.md`.
- Keep private-material and sanitization guidance in `privacy.md`.
- Keep config shape and template behavior in `configuration.md`.
- Keep user-facing operation and TUI notes in `setup.md`.
- Keep log output behavior and event examples in `logging.md`.
- Keep fake-IP, DNS, loop, QUIC, and UDP/443 symptoms in `troubleshooting.md`.
- Prefer tests with injected dialers/callbacks over live network tests.
- Use the `manual` build tag only for private, opt-in probes.
- Keep docs and tests sanitized; do not derive examples from private configs,
  node URLs, provider domains, captures, or live logs.
- Revisit UDP/443, fragment handling, and multipath retransmit/window behavior
  only with fresh reverse findings or focused tests. Do not change these
  policies based on generic proxy behavior alone.
