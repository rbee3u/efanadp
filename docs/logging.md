# Logging

Runtime log fields are configured in `config.yaml`; see
[`configuration.md`](configuration.md) for accepted values. Privacy rules for
sharing logs live in [`privacy.md`](privacy.md).

## Outputs

Runtime logs are written to the configured `log.output`. The default is:

```text
logs/app.log
```

`log.output` also accepts `stdout` and `stderr`. File outputs rotate at 100 MiB.
`log.format` can be `text` or `json`, and `log.level` can be `debug`, `info`,
`warn`/`warning`, or `error`.

The TUI uses stdout for rendering, so keep the default file output or use
`stderr` if you want terminal logs while the TUI is running.

Logs can contain local addresses, targets, and sniffed inner hosts. Keep them
out of git and redact them before sharing.

## Event Lines

In text format, the logger wraps each message with timestamp and level fields.
The message body logs each TCP stream on open and close. If Shadowsocks sniffing
resolves an inner target, an update line is logged too:

```text
[tcp #1] open source=127.0.0.1:53241 target=203.0.113.10:39281
[tcp #1] update source=127.0.0.1:53241 target=203.0.113.10:39281->www.example.org:443
[tcp #1] close source=127.0.0.1:53241 target=203.0.113.10:39281->www.example.org:443 time=420ms up=593B down=3.4KiB
```

UDP associations are logged as `udp`. If a UDP payload is Shadowsocks traffic to
a configured landing server, the adapter also tries to sniff the inner UDP
target:

```text
[udp #31] open source=127.0.0.1:57522 target=UDP_ASSOCIATE 0.0.0.0:0
[udp] open target=203.0.113.10:39281
[udp] sniff target=203.0.113.10:39281->stun.example.org:3478
[udp #31] update source=127.0.0.1:57522 target=203.0.113.10:39281->stun.example.org:3478
[udp] close idle target=203.0.113.10:39281 idle=90.0s
```

An idle `close idle` line means a cached UDP destination went quiet past the
target idle timeout and its upstream stream was released. The next datagram for
that destination reopens it, so this is routine cleanup rather than an error.

A `UDP_ASSOCIATE` relay is pinned to the first source that sends a valid
datagram. Datagrams from any other local source are dropped, warned once per
association, and never forwarded:

```text
[udp] drop datagram from=127.0.0.1:61000 reason=not-associated-client client=127.0.0.1:57522
```

A UDP destination reads its X365 response lazily, on the first packet back from
the upstream, so a destination the server refuses surfaces as a read-response
warning rather than an open failure:

```text
[udp] read response target=203.0.113.10:39281 err=bad X365 response: 5836353031
```

Routine endings — association close, service shutdown, and the idle sweep — stay
quiet on this path.

Fields:

- `source`: the local SOCKS client, usually mihomo.
- `target`: the target from the SOCKS request. When optional Shadowsocks
  sniffing is configured and succeeds, it is shown as
  `landing-host:port->inner-host:port`.
- `time`: connection lifetime.
- `up`: bytes sent from the local client to the remote target.
- `down`: bytes received from the remote target.

When a stream ends with an error, the close line adds an `err=...` suffix.
Benign cases such as `EOF` or client timeouts are expected when a client opens a
speculative connection and abandons it.

If the process runs out of file descriptors or socket buffers, accepting new
clients fails until connections close. The adapter keeps the listener running and
warns once per retry:

```text
[efanadp] accept failed, retrying in 5ms: accept tcp 127.0.0.1:10865: too many open files
```

Repeated lines with a growing delay mean the descriptor limit is the bottleneck,
not the node. Raise the limit or reduce concurrent connections.

Because this adapter sits behind mihomo, it usually cannot see the original
browser process name. Check mihomo's own connection panel for that.
