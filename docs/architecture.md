# Architecture

[ADR 0001](adr/0001-local-x365-adapter-boundary.md) explains the product and trust
boundary; [ADR 0002](adr/0002-config-and-session-lifecycle.md) explains session
replacement and configuration lifetime.

## Package Responsibilities

| Package | Owns |
| --- | --- |
| `cmd/efanadp` | Process entry point |
| `internal/app` | CLI dispatch, dependency wiring, config loading, settings I/O, log sink |
| `internal/config` | YAML normalization, typed config errors, node parsing, templates |
| `internal/tui` | Node selection/switching, connection projection, terminal interaction |
| `internal/session` | Offline preparation, service cancellation/completion, event delivery |
| `internal/proxy` | SOCKS5 lifecycle, TCP/UDP relay, target cache, connection accounting |
| `internal/x365` | Node URI, protocol frames, multipath TCP, direct UDP, xhttp + REALITY |
| `internal/sniff` | Display-only Shadowsocks target inspection |
| `internal/datagram` | Message-oriented I/O contract |
| `internal/human` | Shared display formatting |

`make lint` enforces production boundaries through depguard: core packages cannot
import application/UI layers; relay layers cannot import config or session;
`proxy` cannot import `x365`; `sniff` cannot import relay, carrier, or YAML code.
Tests may cross those boundaries. The TUI receives node data and events directly.

## Runtime Flow And Ownership

1. `app.Run` resolves and loads one config, validates the log destination, and
   creates the logger.
2. The TUI selects a node. `session.Prepare` calls `x365.NewClient` to validate
   authentication offline before stopping an existing session.
3. A switch cancels and awaits the old session, then starts the prepared
   replacement with `proxy.ServeSOCKS`. Session IDs reject stale UI messages.
4. Normal quit or terminal failure cancels and awaits the service before the
   logger closes. A second quit skips waiting for a stuck teardown.

The session owns the network task and event queue; the TUI owns connection state
and display timing. See [`session.Service`](../internal/session/session.go) for
cancellation and event-consumption contracts. `app` owns logger lifetime and
[settings persistence](configuration.md#remembered-settings).

## I/O And Error Contracts

TCP uses `io.ReadWriteCloser`; UDP uses [`datagram.Conn`](../internal/datagram/conn.go)
to preserve message boundaries. These interfaces and
[`proxy.Upstream`](../internal/proxy/server.go) define buffer, closure, and error
contracts.

Config failures with startup hints carry `config.Error.Kind`. YAML fields and
legacy aliases stay in `config`; sniffing receives normalized values.

## SOCKS5 Server

The server accepts no-auth `CONNECT` and `UDP_ASSOCIATE`, rejects fake-IP
targets in `198.18.0.0/15`, and applies the [UDP policy](x365-protocol.md#udp-path).

Each client has a handler goroutine. Cancellation closes the listener and
clients, then awaits handlers. Descriptor/buffer exhaustion retries with
exponential backoff from 5 ms to 1 s; other accept errors stop the service.

## TCP Relay

A CONNECT uses the [X365 multipath TCP path](x365-protocol.md#tcp-path).

ACKs must not block reads behind a flow-controlled upload; see
[`ackQueue`](../internal/x365/multipath_ack.go). Graceful close has a bounded
wait before closing the stream to release stalled writes.

## UDP Relay

A UDP association owns the local socket and per-destination streams for one
TCP control connection. It accepts only the TCP peer's IP, enforces a declared
non-zero UDP source port, and otherwise pins the first valid source port.

Each target host/port gets one cached direct X365 UDP stream. Background opens
keep handshakes from blocking other destinations; queued datagrams drain before
later packets use the stream. See [drop reasons](logging.md#event-lines) for
queue and target limits.

Targets idle for 90 seconds expire, checked every 15 seconds; later datagrams
reopen them. See [logging](logging.md#event-lines) for response-send failures.

## Transport Lifetime

Each stream owns an xhttp + REALITY transport and its dialed sockets. Closing
only idle HTTP/2 connections is insufficient while RoundTrip still considers a
stream active; explicit socket closure releases its read loop.

The opening deadline covers outer transport setup and the X365 request write.
TCP also waits for the HTTP/X365 response; UDP defers it to the target reader,
where idle expiry bounds an unanswered stream. Successful opening disarms the
deadline, while caller cancellation still ends the transport.

## Observability

Connection events feed the TUI and use the same `source` and `target` terms as
[logs](logging.md). The session queue preserves opens/closes and coalesces
pending label updates by connection ID. It does not wait for the UI; the TUI
applies each batch before rendering. Undrained lifecycle events can grow with
connection churn; there is no fixed queue limit.

Sniffed targets affect labels only; forwarding does not depend on sniffing
success. [Logging](logging.md#event-lines) defines UDP observation limits.
