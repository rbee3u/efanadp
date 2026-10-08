# Privacy and Sanitization

`config.yaml`, `settings.json`, private node URLs, account UUIDs, REALITY keys
and short IDs, provider domains, captures, and live logs are private. Never
commit, quote, or derive examples from them. Open private config only for
explicitly requested runtime debugging.

## Examples and Reports

Start examples from placeholders: `example.org`, `example.net`, `203.0.113.10`,
`<your-x365-uuid>`, `<public-key>`, `<short-id>`, and `<shadowsocks-password>`.
Partial redaction of a private value is insufficient.

Before sharing, inspect the diff for private material. Report failures by
command or UI action, status, and redacted error class. Settings contain local
paths and node names; logs can expose client addresses, targets, and sniffed
inner hosts. Keep both untracked.

Config and template YAML errors omit source excerpts while preserving parser
reason and position. Preserve this boundary to avoid exposing node material.

## Config Template

`config.reference.yaml` is the committed reference. Regenerate only for
config-shape changes or explicit template maintenance, using the
[reference command](configuration.md#committed-template-format).

`proxies_tmpl` is zlib-compressed base64, which is packaging, not redaction.
The generator removes X365 UUID userinfo only; hosts, paths, keys, short IDs,
and other provider material can remain. Decode, decompress, expand, or quote
it only for an explicit template audit or regeneration task. Inspect generated
output before committing and report only sanitized findings.

The command atomically replaces its output with mode `0600` after a complete
write and refuses an output that resolves to its private input file.
