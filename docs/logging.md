# Logging

See [configuration](configuration.md#logging) for log settings and
[privacy](privacy.md) before sharing logs.

## Outputs

The default output is `logs/app.log`. Files rotate at 100 MiB, retaining up to
4 backups for up to 30 days. `stdout` and `stderr` are also supported, but
terminal logs may disrupt the TUI; prefer file output.

## Event Lines

Text output prefixes messages with timestamp and level. TCP streams log opens,
closes, and target updates from Shadowsocks sniffing:

```text
[tcp #1] open source=127.0.0.1:53241 target=203.0.113.10:39281
[tcp #1] update source=127.0.0.1:53241 target=203.0.113.10:39281->www.example.org:443
[tcp #1] close source=127.0.0.1:53241 target=203.0.113.10:39281->www.example.org:443 time=420ms up=593B down=3.4KiB
```

UDP logs cover associations, cached destinations, and sniffed inner targets:

```text
[udp #31] open source=127.0.0.1:57522 target=UDP_ASSOCIATE 0.0.0.0:0
[udp] open target=203.0.113.10:39281
[udp] sniff target=203.0.113.10:39281->stun.example.org:3478
[udp #31] update source=127.0.0.1:57522 target=203.0.113.10:39281->stun.example.org:3478
[udp] close idle target=203.0.113.10:39281 idle=1m30s
```

Drop warnings are limited to once per reason per association:

```text
[udp] drop datagram from=127.0.0.1:61000 reason=not-associated-client client=127.0.0.1:57522
```

| Reason or message | Meaning |
| --- | --- |
| `not-associated-client` | Violates the association's [source binding](architecture.md#udp-relay). |
| `malformed SOCKS5 datagram` | Invalid SOCKS5 UDP envelope. |
| `fragmented SOCKS5 datagram` | Fragments are unsupported. |
| `x365-udp-443-disabled` | UDP/443 is disabled by [protocol policy](x365-protocol.md#udp-path). |
| `fake-ip` | The target is in mihomo's `198.18.0.0/15` range. |
| `target-not-ready` | The opening target's 8-datagram queue is full. |
| `target-limit` | 64 distinct targets are already open or opening; existing targets continue. |
| `client-write` | Sending a response locally failed; keep the target unless the association socket closed. |

A maximum-size X365 response plus its SOCKS envelope can exceed the local UDP
packet limit and produce `client-write` with `EMSGSIZE`.

`close idle` releases a destination; `open target=` includes reopens on later
datagrams. A label's `(+N more)` counts other cached destinations when that target
opens; it is not a live count. Each
association reports at most 64 distinct sniffed chains; rediscovered chains do
not update the label. The first change between sniffed chains also warns:

```text
[udp] label oscillation old=203.0.113.10:39281->a.example.org:5353 new=203.0.113.10:39281->b.example.org:5353
```

The first target read validates the deferred X365 response, so upstream refusal
appears as a read-response warning:

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

Reduce concurrent connections; raise the file limit only for descriptor exhaustion.

Because this adapter sits behind mihomo, it usually cannot see the original
browser process name. Check mihomo's own connection panel for that.
