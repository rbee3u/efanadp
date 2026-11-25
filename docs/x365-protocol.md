# X365 Protocol Reference

Adapter-side reference from sanitized efanrev findings. The current shape follows
efanapp `1-0-71`: the X365 header names the real destination and TCP has no inner
framing. Change protocol claims only with fresh APK evidence; keep operational
workarounds in [troubleshooting](troubleshooting.md).

## Protocol Generations

Through `1-0-64`, the app opened `multipath.internal:1` and carried TCP inside a
13-byte-framed multipath layer whose hello named the real target. `1-0-71` removed
that layer. efanadp implements the `1-0-71` shape only; the older multipath
framing is no longer part of the adapter.

## Node URI

See [configuration](configuration.md#node-uri-fields) for URI fields. UUIDs decode
to 16 request bytes. URI parsing requires `sni`; only programmatic nodes can fall
back to `host` for the REALITY server name.

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
6       16    request identifier (account UUID for tunneled traffic)
22      2     destination port, big endian
24      var   typed destination address
```

Destination address encoding:

```text
IPv4:   0x01 addr[4]
Domain: 0x02 len[1] name[len]
IPv6:   0x03 addr[16]
```

`1-0-71` chooses this 16-byte value by connection kind: a bootstrap identifier
for its version, config, and connectivity calls, and the account UUID from
`access_token` for every tunneled connection and datagram. efanadp only makes
tunneled requests, so it uses the node URI UUID for TCP and UDP.

For TCP the destination is the real target, so a domain request is exactly
`26 + len(domain)` bytes before payload. A sanitized `example.com:443` request:

```text
58333635 01 01 00112233445566778899aabbccddeeff 01bb 02 0b "example.com"
```

The server acknowledges successful setup with:

```text
"X365" 0x00
```

This five-byte response precedes any target data. Any other magic or non-zero
status is rejected.

## TCP Path

TCP uses the direct X365 command path:

1. Open the xhttp + REALITY stream.
2. Send a command `0x01` request naming the real target host and port.
3. Read the 5-byte `X365 0x00` response.
4. Relay raw bytes in both directions.

There is no inner hello, frame, ACK, or close record. The adapter consumes the
acknowledgement before returning the stream, so the first relayed response byte
is target data. Closing the stream ends the flow.

## UDP Path

UDP uses a direct X365 command path. `1-0-71` still emits command `0x02`, but
only for payload-free public-DNS reachability probes; the app never sends a
datagram payload in that build. The `uint16_be` prefix is therefore not exercised
by 1.0.71, but it is the only boundary a byte stream carrying datagrams can use,
so efanadp retains it. The adapter's relay is what actually sends payloads.

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
- UDP/443 is dropped. Earlier releases deliberately avoided X365 UDP/443 for
  QUIC; 1.0.71 only showed payload-free datagram probes, so there is still no
  fresh evidence that X365 forwards QUIC. Forwarding stays disabled until an APK
  observation proves it.

## Local SOCKS Mapping

For TCP `CONNECT`, efanadp writes the SOCKS target directly into the X365
request.

For UDP `UDP_ASSOCIATE`, efanadp parses the standard SOCKS5 UDP envelope:

```text
RSV[2] FRAG[1] ATYP[1] DST.ADDR DST.PORT[2] DATA
```

The adapter strips this local SOCKS envelope before writing the X365 UDP
datagram payload, and rebuilds the SOCKS envelope around each response payload.
