# Privacy and Sanitization

This project handles private-networking runtime material. Use this checklist
before writing docs, tests, issues, commits, or review notes.

## Before Sharing or Committing

- Check the diff for private configs, node URLs, UUIDs, keys, provider domains,
  captures, and live logs.
- Write examples from placeholders; do not partially redact private values.
- Summarize live failures by command, TUI status, and redacted error class
  instead of pasting raw config or log lines.
- Decode, decompress, or expand `proxies_tmpl` only for explicit template
  maintenance or audit work, and never quote decoded private material.

## Private Material

Treat these as private:

- `config.yaml`
- `settings.json`
- private node URLs
- account UUIDs
- REALITY keys and short IDs
- captures, provider domains, and live logs

Do not commit, quote, or derive examples from them. Do not open private config
unless the user explicitly asks for runtime debugging. When reporting runtime
failures, summarize the symptom and redact the material.

## Safe Examples

Use only sanitized placeholders in committed text:

- `example.org` for adapter-bound traffic examples
- `example.net` for node-server domain examples
- `203.0.113.10` for documentation-only IPv4 examples
- `<your-x365-uuid>`, `<public-key>`, `<short-id>`, and
  `<shadowsocks-password>` for secrets or node material

Do not copy hostnames, paths, UUIDs, public keys, or log lines from a private
node and then "partially redact" them. Start from placeholders instead.

## Config Template Safety

`config.reference.yaml` is the committed template. Regenerate it with
`make config-reference` only when the config shape changes or when explicitly
maintaining the template.

The `proxies_tmpl` field is zlib-compressed, base64-encoded packaging, not
redaction. The generator strips X365 UUID userinfo only; unpacked templates can
still represent node hosts, paths, public keys, short IDs, or other
provider-specific values after decoding and decompression. Do not decode,
decompress, expand, or quote it during routine documentation or issue triage.
When template maintenance is required, inspect the generated diff for sanitized
output before committing.

## Logs and Reports

`settings.json` can contain local config paths and remembered node names. Logs
can contain local client addresses, targets, and sniffed inner hosts. Keep
`settings.json` and `logs/` out of git and do not paste live logs into docs or
comments.

Useful reports describe:

- the command or UI action
- the visible TUI status
- the redacted error class or symptom
- whether mihomo, browser DNS/DoH, UDP/443, or fake-IP behavior is involved

## Protocol Findings

Protocol changes need fresh reverse evidence. In particular, do not loosen
UDP/443 or QUIC behavior unless a new APK finding proves X365 forwards QUIC
through the UDP path. Document such findings in
[`x365-protocol.md`](x365-protocol.md).
