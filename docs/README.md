# Documentation

This directory holds details that should not crowd the README. Keep every
example sanitized: no private node data, account UUIDs, REALITY keys, provider
domains, captures, or live logs.

## Start Here

The current split is intentionally small: private-material handling, normal
operation, config reference, symptom triage, implementation architecture, and
protocol findings each have a separate home. The canonical directory is
`docs/`; this repository does not use `doc/`.

| Need | Read |
| --- | --- |
| Private material, safe examples, logs, template safety | [privacy.md](privacy.md) |
| First run, mihomo rules, TUI controls, logs, smoke tests | [setup.md](setup.md) |
| `config.yaml`, reference generation, startup config behavior | [configuration.md](configuration.md) |
| Fake IPs, curl, browser DNS/DoH, loops, UDP/443, QUIC | [troubleshooting.md](troubleshooting.md) |
| Package responsibilities and relay flow | [architecture.md](architecture.md) |
| X365, multipath, UDP, xhttp, and REALITY bytes | [x365-protocol.md](x365-protocol.md) |

## Maintenance Rules

- Keep [README.md](../README.md) focused on first-run setup and links into
  this directory.
- Keep `docs/` as the single docs root unless a rename is deliberate; avoid
  parallel `doc/` and `docs/` trees.
- Update docs in the same change as user-facing behavior, config shape,
  Makefile targets, relay behavior, or protocol findings.
- Keep private-material rules centralized in [privacy.md](privacy.md), then link
  to it from user-facing docs instead of repeating long warnings everywhere.
- Put operational symptoms and resolver behavior in
  [troubleshooting.md](troubleshooting.md), not in protocol notes.
- Put reverse-engineered byte layouts and invariants in
  [x365-protocol.md](x365-protocol.md), not in setup docs.
- Treat `proxies_tmpl` as packaged template data, not safe text. Do not decode,
  decompress, or quote it during routine docs work.
- Keep private runtime material out of all examples. Use `example.org`,
  `example.net`, `203.0.113.10`, and `<your-x365-uuid>`.
