# Logging

Runtime log fields are configured in `config.yaml`; see
[`configuration.md`](configuration.md) for accepted values. Privacy rules for
sharing logs live in [`privacy.md`](privacy.md).

## Outputs

Runtime logs are written to the configured `log.output`. The default is:

```text
logs/app.log
```

`log.output` also accepts `stdout` and `stderr`. File outputs rotate at 100 MiB
and keep 4 rotated files for up to 30 days, so the log directory stays bounded
at about 500 MiB. `log.format` can be `text` or `json`, and `log.level` can be
`debug`, `info`, `warn`/`warning`, or `error`.

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
[udp] close idle target=203.0.113.10:39281 idle=1m30s
```

An idle `close idle` line means a cached UDP destination went quiet past the
target idle timeout and its upstream stream was released. The next datagram for
that destination reopens it, so this is routine cleanup rather than an error.

A `UDP_ASSOCIATE` relay accepts datagrams only from the TCP control client's IP.
A non-zero source port in the request is enforced; otherwise the first valid
datagram pins the source port. Datagrams from any other source are dropped,
warned once per association, and never forwarded:

```text
[udp] drop datagram from=127.0.0.1:61000 reason=not-associated-client client=127.0.0.1:57522
```

Every drop reason is warned about once per association, and the datagram named
in that first line is the one that triggered it. Malformed packets, fragments,
UDP/443, fake IPs, and foreign sources can repeat for every datagram a client
keeps sending; a broken client or a browser retrying QUIC would otherwise write
its send rate straight into the log file. Later drops for a reason already
warned about are silent, and still dropped:

```text
[udp] drop malformed SOCKS5 datagram from=127.0.0.1:61000 err=SOCKS5 UDP packet too short: 1
[udp] drop fragmented SOCKS5 datagram frag=1 target=203.0.113.10:39281
[udp] drop target=203.0.113.10:443 reason=x365-udp-443-disabled
[udp] drop fake-ip target=198.18.0.1:53
[udp] drop target=203.0.113.10:39281 reason=target-not-ready
[udp] drop target=203.0.113.10:39281 reason=target-limit
[udp] drop response target=203.0.113.10:39281 reason=client-write err=... sendto: message too long
```

`target-not-ready` means a datagram arrived for a destination whose stream was
still opening, and the queue of datagrams waiting on that open was already full.
Opens run in the background so a handshake never blocks other destinations, and
only one runs per destination at a time. Datagrams that arrive while an open is
in flight queue onto it rather than being dropped, so a stub resolver's A and
AAAA queries sent together both reach the destination on the first lookup. The
queue has room for a handful; past that the client is outrunning a destination
that is not answering yet, and later datagrams are dropped instead of spending
memory to deliver a burst the client has already given up on. UDP clients
retry.

`target-limit` means the association already has 64 distinct destinations open
or opening. New destinations are dropped so one client cannot create unbounded
background handshakes and cached streams; existing destinations keep working.

`client-write` means a response arrived from the upstream but the send to the
local client failed. The destination stays cached and the relay continues with
the next response, because the association socket is shared by every destination
and the failure belongs to the one datagram. An upstream response at the X365
maximum reaches this: the added SOCKS envelope pushes it past the 65507-byte
datagram limit and the send reports `EMSGSIZE`.

An `open target=` line is logged every time a destination opens, including a
reopen after the idle sweep closed it, so a `close idle` line always has a
matching reopen when traffic returns. The `(+N more)` count in the connection
label counts destinations the association currently holds, not every one it has
ever opened.

When one association carries several inner destinations through a single
Shadowsocks server, the connection label can only hold one at a time. Each
distinct chain is reported once, and the label settles on the most recently
discovered one. The first time the label moves between chains, that is noted
once:

```text
[udp] label oscillation old=203.0.113.10:39281->a.example:5353 new=203.0.113.10:39281->b.example:5353
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
