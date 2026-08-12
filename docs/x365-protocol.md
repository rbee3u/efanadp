# X365 Protocol Reference

Related docs: [`setup.md`](setup.md) covers operation,
[`configuration.md`](configuration.md) covers runtime input,
[`architecture.md`](architecture.md) maps protocol pieces back to code, and
[`privacy.md`](privacy.md) covers sanitization rules.

This document is the adapter-side implementation reference distilled from the
sanitized efanrev findings. It intentionally avoids private node data, REALITY
keys, captures, and account-specific values.

The current baseline is confirmed across efanapp `1-0-27` and `1-0-28`.
Update this file only when a fresh APK finding changes protocol bytes, frame
layouts, request headers, or invariants. Keep operational workarounds in
[`troubleshooting.md`](troubleshooting.md).

## Node URI

Decrypted nodes are `x365://` URIs:

```text
x365://<uuid>@<server>:<port>?path=<path>&host=<host>&sni=<sni>&pbk=<public-key>&sid=<short-id>[&fp=<fingerprint>]
```

Fields used by the adapter:

- `uuid`: user material. Strip dashes, hex-decode, and use the resulting 16
  bytes in X365 request frames.
- `server` and URI `port`: TCP endpoint for the outer transport.
- `path`: HTTP/2 POST path for xhttp.
- `host`: HTTP authority/Host header and default HTTPS request host.
- `sni`: REALITY TLS server name. The current URI parser requires this field;
  the lower-level transport falls back to `host` only for programmatic nodes.
- `pbk`: REALITY X25519 public key, base64url encoded, 32 bytes after decode.
- `sid`: REALITY short ID, hex encoded, at most 8 bytes.
- `fp`: optional uTLS fingerprint. The observed/default behavior is Chrome-like.

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

That is 5 bytes total. Any other magic or non-zero status is treated as a bad
X365 response.

## TCP Path

The observed app does not send ordinary TCP streams by opening an X365 request
directly to the final target. It opens a TCP-ish X365 stream to the multipath
control endpoint, then carries the real TCP target inside multipath frames.

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

Close uses type `0x03` with the current write sequence and no payload. The
current adapter implements the observed minimum needed for stable SOCKS TCP
relay; retransmit and window behavior remain intentionally minimal.

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

Write rules:

- Maximum payload length is `0xffff`.
- The length prefix is two bytes, big endian.
- The prefix is not part of the returned payload byte count.
- A failed frame write is terminal for that stream because retrying after a
  partial length prefix or payload would corrupt every following boundary.

Read rules:

- Read exactly two bytes for the payload length.
- Read exactly that many payload bytes.
- The adapter allocates a 65535-byte buffer for SOCKS UDP relay, so a valid X365
  datagram fits without protocol-level fragmentation.

Current adapter policy:

- SOCKS5 `UDP_ASSOCIATE` opens one cached direct X365 UDP stream per destination
  host/port inside an association.
- SOCKS5 UDP fragments (`FRAG != 0`) are rejected; no reassembly is implemented.
- UDP/443 is dropped. Static analysis found the app deliberately avoids X365
  UDP/443 for QUIC and falls back to TCP, so forwarding UDP/443 through this
  path is disabled until a future APK proves otherwise.

## Local SOCKS Mapping

For TCP `CONNECT`, efanadp maps the SOCKS target to the multipath hello target.
Local DNS behavior matters: direct tests should use `socks5h://` so the target
domain reaches the adapter instead of a local fake IP.

For UDP `UDP_ASSOCIATE`, efanadp parses the standard SOCKS5 UDP envelope:

```text
RSV[2] FRAG[1] ATYP[1] DST.ADDR DST.PORT[2] DATA
```

The adapter strips this local SOCKS envelope before writing the X365 UDP
datagram payload, and rebuilds the SOCKS envelope around each response payload.

## Regression Checklist

When efanrev analyzes a future APK, efanadp only needs protocol changes if one
of these invariants changes:

- X365 magic/version/command layout.
- UUID material offset or size.
- Address type values or address encoding.
- Success response shape.
- `multipath.internal:1` control target.
- Multipath frame header, frame type values, hello payload, or sequence/ACK
  behavior.
- Direct UDP command value or `uint16_be(payload_len) payload` datagram wrapper.
- REALITY/xhttp handshake inputs or HTTP/2 request shape.
