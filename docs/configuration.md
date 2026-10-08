# Configuration Reference

`config.yaml` is private runtime input; see [privacy](privacy.md).
Use one YAML document and one node source: `proxies`, or `proxies_tmpl` with
`proxies_uuid`.

## Plain Config Example

For the packed alternative, see [Committed Template Format](#committed-template-format).

```yaml
listen: 127.0.0.1:10865
log:
  level: info
  output: logs/app.log
  format: text
sniff:
  shadowsocks:
    - name: landing
      server: 203.0.113.10
      port: 39281
      method: aes-256-gcm
      password: <shadowsocks-password>
proxies:
  example-node:
    - x365://<your-x365-uuid>@203.0.113.10:443?path=/node&host=example.org&sni=example.org&pbk=<public-key>&sid=<short-id>
```

Top-level fields:

- `listen`: local SOCKS5 address, default `127.0.0.1:10865`. Keep it on
  loopback: SOCKS5 uses no-auth.
- `log`: optional runtime logging settings. Defaults to `level: info`,
  `output: logs/app.log`, and `format: text`.
- `proxies`: map of display names to lists of `x365://` node URLs.
- `sniff.shadowsocks`: optional list of first-hop Shadowsocks landing servers
  used only for display-oriented target sniffing.

## Node URI Fields

Decrypted nodes use this form:

```text
x365://<uuid>@<server>:<port>?path=<path>&host=<host>&sni=<sni>&pbk=<public-key>&sid=<short-id>[&fp=<fingerprint>]
```

The adapter requires:

- `uuid`: user material. Dashes are allowed; the cleaned value must decode to 16
  bytes.
- `server` and URI `port`: TCP endpoint for the outer transport.
- `path`: HTTP/2 POST path for xhttp.
- `host`: HTTP authority/Host header and default request host.
- `sni`: required REALITY TLS server name.
- `pbk`: REALITY X25519 public key, base64url encoded, 32 bytes after decode.
- `sid`: REALITY short ID, hex encoded, at most 8 bytes.
- `fp`: optional uTLS fingerprint. Supported values include `firefox`,
  `safari`, `ios`, `edge`, `random`, and `randomized`; unknown or empty values
  use the Chrome-like default.

See [`x365-protocol.md`](x365-protocol.md) for byte-level protocol details.

## Committed Template Format

`config.reference.yaml` stores nodes in `proxies_tmpl` without the account UUID.
See [quick start](../README.md#quick-start) to use it.

`proxies_tmpl` is zlib-compressed, base64-encoded YAML with URLs of the form
`x365://@<server>...`. Loading inserts `proxies_uuid` as userinfo. Legacy
uncompressed base64 YAML is also accepted; unpacked data is limited to 4 MiB.

Follow [template privacy rules](privacy.md#config-template): compression is not
redaction, and payload inspection requires an explicit audit or regeneration task.

Regenerate for config-shape changes or explicit template maintenance. The command
defaults to the paths below; override `--in` and `--out` as needed:

```bash
make run ARGS="config-reference --in config.yaml --out config.reference.yaml"
```

It copies `listen`, emits the default `log` block, strips X365 UUID userinfo,
and replaces private sniffing entries with a commented placeholder.

## Runtime Config Changes

Config changes require a process restart. Startup selects the first non-empty
path: `--config` / `-c`, then `configPath` from `settings.json` in the working
directory, then `config.yaml`. A missing or invalid file fails startup; it does
not trigger fallback to another path.

## Remembered Settings

The app saves `configPath` (including explicit CLI paths) and `nodeName` to
`settings.json` in the working directory when entering the TUI and before starting
a selected session. It saves no node contents; saving does not confirm listener
binding or upstream reachability.

On startup, the TUI attempts to start the node matching the remembered name;
if absent, it clears the name and shows node selection. Names are display labels
and may refer to different endpoints after config edits.

## Logging

`log.level` accepts `debug`, `info`, `warn`, `warning`, and `error`. `warning`
is normalized to `warn`.

`log.output` accepts `stdout`, `stderr`, or a local path. It must not resolve to
the active config file or `settings.json`, which log writes would corrupt.
See [logging](logging.md#outputs) for rotation and terminal output.

`log.format` accepts `text` and `json`.

The legacy `log_path` field is no longer accepted; use `log.output` instead.

## Shadowsocks Target Sniffing

Use `sniff.shadowsocks` when the next hop is a Shadowsocks landing server.
Matching `server`/`port` and valid `method`/`password` let the adapter inspect
initial TCP metadata or UDP datagrams to display the inner target. `cipher`
is a legacy alias for `method`; prefer `method` in new configs.

Supported methods:

- `aes-128-gcm`
- `aes-256-gcm`
- `chacha20-ietf-poly1305`
- `2022-blake3-aes-128-gcm`
- `2022-blake3-aes-256-gcm`

For `2022-blake3-*` methods, `password` must be the base64 PSK used by mihomo.
The `server_key:user_key` EIH form is also accepted.

Omitting sniffing leaves the original SOCKS target display. Entries with missing
`server`, `method` (or legacy `cipher`), or `password`, or an invalid `port`,
fail config loading.
Unsupported methods, unusable keys, unmatched targets, or failed inspection
leave the original label; sniffing never changes forwarding.
