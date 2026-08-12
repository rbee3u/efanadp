# Configuration Reference

`config.yaml` is private runtime input. It is usually produced by the efanrev
reverse workflow and should not be committed, pasted into docs, or used as the
basis for examples. See [`privacy.md`](privacy.md) for sanitization rules.

## Plain Config Example

This is the decrypted config shape. The committed `config.reference.yaml`
template uses `proxies_tmpl` and `proxies_uuid` instead; see
[Committed Template Format](#committed-template-format).

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

- `listen`: optional local SOCKS5 listen address. Defaults to
  `127.0.0.1:10865`.
- `log`: optional runtime logging settings. Defaults to `level: info`,
  `output: logs/app.log`, and `format: text`.
- `proxies`: map of display names to lists of `x365://` node URLs.
- `sniff.shadowsocks`: optional list of first-hop Shadowsocks landing servers
  used only for display-oriented target sniffing.

Older plaintext dumps without the `proxies` wrapper may need to be regenerated
with the current decrypt workflow.

Keep placeholders in committed examples. Use `example.org`, `example.net`,
`203.0.113.10`, and `<your-x365-uuid>` instead of values copied from a private
node. Do not derive examples by editing private node strings.

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
- `sni`: REALITY TLS server name. The config parser currently requires this
  query field.
- `pbk`: REALITY X25519 public key, base64url encoded, 32 bytes after decode.
- `sid`: REALITY short ID, hex encoded, at most 8 bytes.
- `fp`: optional uTLS fingerprint. Supported values include `firefox`,
  `safari`, `ios`, `edge`, `random`, and `randomized`; unknown or empty values
  use the Chrome-like default.

See [`x365-protocol.md`](x365-protocol.md) for byte-level protocol details.

## Committed Template Format

`config.reference.yaml` keeps the node template in `proxies_tmpl` and leaves the
account UUID out of git. To use it:

1. Copy `config.reference.yaml` to `config.yaml`.
2. Replace `proxies_uuid` with your UUID.
3. Start the TUI.

`proxies_tmpl` is zlib-compressed, base64-encoded YAML with each URL written as
`x365://@<server>...`. At load time, `efanadp` base64-decodes the field,
decompresses it, inserts `proxies_uuid` before the `@`, and parses it like a
normal `proxies` block. Older uncompressed base64 templates are accepted for
local migration, but new reference files are written in the compressed format.
Unpacked template data is limited to 4 MiB so malformed compressed input cannot
consume unbounded memory during startup.

Compression and base64 are not redaction. The maintenance command removes X365
UUID userinfo only, so the generated template can still carry node hosts, paths,
public keys, short IDs, and other provider-specific material. Review the
generated file before committing it and keep documentation examples sanitized.

Do not decode, decompress, expand, or quote `proxies_tmpl` during routine docs
or issue triage. Inspect it only when auditing or regenerating the committed
template.
Use [`privacy.md`](privacy.md) as the checklist before sharing or committing
template changes.

When the private `config.yaml` changes, refresh the committed reference with:

```bash
make config-reference
```

The maintenance command reads `config.yaml` and writes `config.reference.yaml` by
default. It copies `listen`, emits the sanitized default `log` block, and writes
UUID-stripped proxy templates; private `sniff.shadowsocks` entries are not
copied, only the commented placeholder is emitted. The command also accepts
explicit paths:

```bash
make config-reference ARGS="--in config.yaml --out config.reference.yaml"
```

## Runtime Config Changes

`efanadp` loads one config file during startup. When `listen`, `log`, node URLs,
or sniffing settings change, stop the process and start it again. Use `--config`
/ `-c` to point a launch at a different YAML file. Without `--config`, the TUI
uses `configPath` from `settings.json` in the current working directory, then
falls back to `config.yaml`.

## Logging

`log.level` accepts `debug`, `info`, `warn`, `warning`, and `error`. `warning`
is normalized to `warn`.

`log.output` accepts `stdout`, `stderr`, or a normal local path. Path outputs are
rotated at 100 MiB per file, keeping 4 rotated files for up to 30 days.

`log.format` accepts `text` and `json`.

The legacy `log_path` field is no longer accepted; use `log.output` instead.
See [`logging.md`](logging.md) for output behavior and event examples.

## Shadowsocks Target Sniffing

The `sniff.shadowsocks` block is optional. It is only needed when `efanadp` is
the first hop of a chain and the next hop is a Shadowsocks landing server. With
a matching `server`/`port` and the correct `method`/`password`, the adapter
decrypts only the first Shadowsocks metadata chunk to display the inner target.
`cipher` is accepted as a legacy alias for `method`; committed examples should
prefer `method`.

Supported methods:

- `aes-128-gcm`
- `aes-256-gcm`
- `chacha20-ietf-poly1305`
- `2022-blake3-aes-128-gcm`
- `2022-blake3-aes-256-gcm`

For `2022-blake3-*` methods, `password` must be the base64 PSK used by mihomo.
The `server_key:user_key` EIH form is also accepted.

Missing, unsupported, or non-matching sniff config falls back to the original
SOCKS target display. Sniffing is for observability only; relay behavior does not
depend on discovering the inner target.
