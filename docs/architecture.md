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

The accept loop survives descriptor and buffer exhaustion. Every proxied
connection holds an upstream socket and every cached UDP destination holds
another, so a busy client can push the process to its limit while the listener
stays healthy. Those accept failures are logged and retried with a backoff
between 5ms and 1s instead of stopping the service; any other accept error still
ends it.

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

ACKs never wait for the frame write lock. A data frame write parks in the
upstream flow-control window for as long as the destination is slow to drain, and
it holds that lock while it does, so an ACK issued from the read path would stall
the download direction and the two directions would wedge against each other.
An ACK therefore goes out inline when the write path is idle, and is otherwise
handed to a per-connection sender goroutine. ACKs are cumulative, so the queue
keeps only the newest sequence.

The graceful multipath close frame is also bounded. An idle write lock does not
guarantee that the HTTP/2 write itself can progress, so shutdown waits up to 100
ms for the frame and then closes the underlying stream to release it. A stalled
peer therefore cannot hold a SOCKS handler or node switch open indefinitely.

## UDP Relay Path

For SOCKS5 `UDP_ASSOCIATE`, the adapter:

1. Creates a local UDP relay socket tied to the TCP association.
2. Parses SOCKS5 UDP envelopes from the client.
3. Drops fragments, fake IP targets, and UDP/443.
4. Requires the UDP source IP to match the TCP control client, honors a non-zero
   source port declared by the `UDP_ASSOCIATE` request, then pins the association
   to the first valid source port when the request used port zero. Datagrams from
   any other source are dropped.
5. Opens one cached direct X365 UDP stream per target host/port, in the
   background so a handshake never blocks other destinations, up to 64 live or
   opening destinations per association.
6. Sends only the datagram payload through the X365 stream.
7. Rebuilds SOCKS5 UDP envelopes around response payloads.
8. Closes cached targets that stay idle past the UDP target idle timeout.

The direct X365 UDP stream sends command `0x02` to the actual UDP target and
frames each datagram as `uint16_be(payload_len) payload`. Its X365 response is
read lazily, on the first read from the target, so a refused destination is
reported by the target relay rather than by the open call.

A cache hit marks its target active while the association lock is held, so the
idle sweep cannot close a destination that an in-flight datagram is about to
reuse. Steady traffic to one resolver makes that the common case.

A cache miss opens the destination in the background rather than on the client
relay loop, carrying the datagram that triggered it along with any that arrive
while it is opening. That loop is the association's only reader, so an open
running on it would delay every other destination behind a TCP dial, a REALITY
handshake, and the X365 request write, and the kernel receive buffer drops
datagrams once it fills. A destination the server refuses would repeat it: the
open itself succeeds because the X365 response is read lazily, the first target
read then fails and detaches it, and the next datagram opens it again. Only one
open runs per destination at a time, and datagrams arriving for a destination
that is still opening queue onto that open rather than being dropped. The queue
has room for a handful; past that the client is outrunning a destination that
is not answering yet, and datagrams are dropped with reason `target-not-ready`
instead of spending memory to deliver a burst the client has already given up
on.

The association also caps the combined number of open and opening destinations
at 64. Without that bound, a burst of unique destinations starts one TCP dial
and REALITY handshake per datagram before any of them can finish, then retains a
stream, relay goroutine, and read buffer for each successful open. Further new
destinations are dropped with reason `target-limit`; traffic to destinations
already counted by the association continues normally.

Client pinning follows RFC 1928: a `UDP_ASSOCIATE` relay belongs to the client
that created it. The TCP peer IP is authoritative, and a non-zero port in the
request narrows the accepted endpoint further; the usual unspecified port is
pinned by the first valid datagram. Without those checks, another process or a
remote host could claim the relay first and receive its inbound responses.

Cached targets expire because each one owns an xhttp + REALITY stream, a relay
goroutine, and a read buffer, while an association only ends when its client
closes the TCP control connection. Expiry keeps a long-lived association that
talks to many destinations from holding every stream for its whole lifetime; a
later datagram for an expired destination just reopens it.

## Transport

Every X365 stream is carried over xhttp + REALITY:

- TCP dial goes to the node server and port.
- REALITY uses uTLS and patches the ClientHello session ID.
- HTTP/2 sends a `POST` to `https://<host><path>`.
- `Content-Type`, `User-Agent`, `Referer`, and `Host` mirror the observed client
  shape described in `x365-protocol.md`.

The HTTP response must be `200 OK`; after that, the request body and response
body are treated as a bidirectional byte stream.

Opening a stream is deadline-bounded as a whole, not just at the TCP dial.
`net/http2` has no response-header timeout, so a node that completes TCP and
REALITY but never answers the X365 request would otherwise block the caller with
no deadline, pinning a SOCKS handler, its client connection, a goroutine, and the
stream until the service stops. The open deadline covers the REALITY handshake,
the response headers, the X365 request write, and the X365 response read; on
expiry it closes the stream, which unblocks whichever of those steps was waiting.
A stream that opens successfully is never closed by this deadline, and a
cancellation from the caller is still reported as a cancellation.

## Observability

The `proxy` package emits `Event` values for open, update, and close.
The TUI consumes these events to render active/closed counts and connection
rows. Logs use the same `source` and `target` terms as the table.

Events cross into the TUI through a queue rather than a bounded channel, and the
update loop applies a whole backlog per render. Producers run on connection
goroutines and must never block on the UI, so a bounded channel would have to
discard, and a discarded close event is unrecoverable: nothing else tells the
model a connection ended, so its row would stay active and the closed counter
would under-count until the next node switch. Opens and closes are therefore
always queued, while a pending update for the same connection is replaced in
place, since an update only carries that connection's newest target label. Queue
growth is bounded by live connections plus the churn between two renders.

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
