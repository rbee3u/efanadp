# X365 Protocol Reference

Adapter-side reference from sanitized efanrev findings, with a baseline confirmed
across efanapp `1-0-27` and `1-0-28`. Change protocol claims only with fresh APK
evidence; keep operational workarounds in [troubleshooting](troubleshooting.md).

## Node URI

See [configuration](configuration.md#node-uri-fields) for URI fields. UUIDs decode
to 16 request bytes. URI parsing requires `sni`; only programmatic nodes can
fall back to `host` for the REALITY server name.

## Outer Transport

Every X365 stream is carried over xhttp + REALITY:

1. Dial TCP to `<server>:<port>`.
2. Perform a REALITY-flavored TLS handshake with uTLS.
3. Start an HTTP/2 `POST` to `https://<host><path>`.
4. Write X365 bytes to the request body and read response bytes from the HTTP
   response body.

Headers used by the current adapter:

```text
Content-Type: application/grpc
User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36
Referer: https://<host><path>
Host: <host>
```

The HTTP response must be `200 OK`. After that, the request body and response
body behave as a bidirectional byte stream.

## X365 Request

Before any TCP or UDP payload, the client writes one X365 request frame:

```text
offset  size  field
0       4     ASCII "X365"
4       1     version, always 0x01
5       1     command: 0x01 TCP-ish, 0x02 UDP-ish
6       16    user UUID material
22      2     destination port, big endian
24      var   typed destination address
```

Destination address encoding:

```text
IPv4:   0x01 addr[4]
Domain: 0x02 len[1] name[len]
IPv6:   0x03 addr[16]
```

The server acknowledges successful setup with:

```text
"X365" 0x00
```

Any other magic or non-zero status is rejected.

## TCP Path

TCP uses a multipath control stream; the hello frame carries the final target.

Open sequence:

1. Open the xhttp + REALITY stream.
2. Send an X365 request with command `0x01` to `multipath.internal:1`.
3. Read the 5-byte `X365 0x00` response.
4. Generate a random 16-byte multipath stream ID.
5. Send a multipath hello frame naming the real target host and port.
6. Relay application bytes through multipath data frames.

Multipath frame header:

```text
offset  size  field
0       1     frame type
1       8     sequence number, big endian
9       1     reserved, observed 0x00
10      3     payload length, big endian uint24
13      var   payload
```

Known frame types:

```text
0x00 hello/control
0x01 data
0x03 close
0x05 ACK
```

Hello payload for opening the real target:

```text
offset  size  field
0       16    random multipath stream ID
16      1     kind, observed 0x00
17      1     flags, observed 0x01
18      2     real target port, big endian
20      1     real target host length
21      var   real target host bytes
```

Data frames use type `0x01`. The client-side write sequence starts at `0` and
advances by the payload length after each data frame. On inbound data frames,
the adapter ACKs `frame.seq + len(frame.payload)` with a type `0x05` frame.

Close uses type `0x03` with the current write sequence and no payload. The adapter
discards inbound ACK, hello, and unknown frames; it implements no multipath
retransmission or send window.

## UDP Path

UDP uses a direct X365 command path rather than the multipath TCP path.

Open sequence for one UDP destination:

1. Open a fresh xhttp + REALITY stream.
2. Send an X365 request with command `0x02` to the actual UDP target host and
   port.
3. The first read consumes the same 5-byte `X365 0x00` response.
4. Each UDP datagram is then length-prefixed on the stream.

Datagram boundary:

```text
uint16_be(payload_len) payload
```

Payloads may be empty and are limited to 65535 bytes. Returned byte counts
exclude the prefix. Reads consume the full datagram, discarding any bytes beyond
the caller's buffer. Failed frame writes are terminal: retrying a partial prefix
or payload would corrupt subsequent boundaries.

Current adapter policy:

- SOCKS5 `UDP_ASSOCIATE` opens one cached direct X365 UDP stream per destination
  host/port inside an association.
- SOCKS5 UDP fragments (`FRAG != 0`) are rejected; no reassembly is implemented.
- UDP/443 is dropped. Static analysis found the app deliberately avoids X365
  UDP/443 for QUIC and falls back to TCP, so forwarding UDP/443 through this
  path is disabled until a future APK proves otherwise.

## Local SOCKS Mapping

For TCP `CONNECT`, efanadp maps the SOCKS target to the multipath hello target.

For UDP `UDP_ASSOCIATE`, efanadp parses the standard SOCKS5 UDP envelope:

```text
RSV[2] FRAG[1] ATYP[1] DST.ADDR DST.PORT[2] DATA
```

The adapter strips this local SOCKS envelope before writing the X365 UDP
datagram payload, and rebuilds the SOCKS envelope around each response payload.
