# Documentation

This directory holds details that should not crowd the README. Keep every
example sanitized: no private node data, account UUIDs, REALITY keys, provider
domains, captures, `settings.json`, or live logs.

## Start Here

The canonical directory is `docs/`; this repository does not use `doc/`.

| Need | Read |
| --- | --- |
| Domain language and ubiquitous terms | [CONTEXT.md](../CONTEXT.md) |
| Architecture boundary and service ownership | [ADR 0001](adr/0001-local-x365-adapter-boundary.md) |
| Private material, safe examples, settings, log redaction, template safety | [privacy.md](privacy.md) |
| First run, mihomo rules, TUI controls, smoke tests | [setup.md](setup.md) |
| Log outputs, event fields, TCP/UDP examples | [logging.md](logging.md) |
| `config.yaml`, reference generation, startup config behavior | [configuration.md](configuration.md) |
| Fake IPs, curl, browser DNS/DoH, loops, UDP/443, QUIC | [troubleshooting.md](troubleshooting.md) |
| Package responsibilities and relay flow | [architecture.md](architecture.md) |
| X365, multipath, UDP, xhttp, and REALITY bytes | [x365-protocol.md](x365-protocol.md) |

## Maintenance Rules

- Keep [README.md](../README.md) focused on first-run setup and links here.
- Keep `docs/` as the single docs root unless a rename is deliberate.
- Update docs with user-facing behavior, config shape, Makefile targets, relay
  behavior, or protocol findings.
- Keep private-material rules in [privacy.md](privacy.md), operational symptoms
  in [troubleshooting.md](troubleshooting.md), log event details in
  [logging.md](logging.md), and byte-level protocol findings in
  [x365-protocol.md](x365-protocol.md).
- Treat `proxies_tmpl` as packaged template data, not safe text. Do not decode,
  decompress, or quote it during routine docs work.
- Keep private runtime material out of all examples. Use `example.org`,
  `example.net`, `203.0.113.10`, and `<your-x365-uuid>`.
