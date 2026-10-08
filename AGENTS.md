# AGENTS.md

Go SOCKS5 adapter for decrypted efanapp `x365://` nodes. Keep changes small and
verified; add no document, abstraction, or dependency without a concrete need.

## Safety

- Treat `config.yaml`, `settings.json`, private node URLs, account UUIDs, REALITY
  keys, provider domains, captures, and live logs as private. Never commit,
  quote, or derive examples from them. Open private config only for explicitly
  requested runtime debugging; summarize and redact failures.
- Use placeholders such as `example.org`, `example.net`, `203.0.113.10`, and
  `<your-x365-uuid>` in docs and tests.
- Keep `config.reference.yaml` as the committed reference. Regenerate only for
  config-shape changes or explicit template maintenance. The generator removes
  X365 UUID userinfo only: inspect output for private hosts, paths, keys, logs,
  or captures before committing.
- `proxies_tmpl` compression and base64 are packaging, not redaction. Decode,
  decompress, expand, or quote it only for an explicit template audit or
  regeneration task; report only sanitized findings.
- Do not loosen UDP/443 or QUIC behavior without a fresh APK finding proving
  X365 forwards QUIC through UDP. Record it in `docs/x365-protocol.md`.

## Commands and Validation

Use Make targets and the host tools from `PATH`; `go.mod` declares the Go version.
`make help` lists targets. `make run` without `ARGS` starts the TUI; it is not a
validation command.

- Check `git status --short` before editing; inspect `git diff --cached` if
  anything is staged. Preserve unrelated changes.
- For code changes, run `make format`, review the diff, then run `make build`,
  `make test`, `make race`, and `make lint` in order. Keep relevant formatting
  edits and report unexpected changes. `make style` checks without rewriting.
- For docs-only changes, verify links, paths, and commands against the repo.
  Run `make help` when command documentation changes.
- For config-shape or generated-payload changes, run
  `make run ARGS="config-reference"` and apply the safety checks above. Keep
  template comments in `internal/config/reference.go` and
  `config.reference.yaml` synchronized.
- Report failed validation by exact command and reason. If the sandbox blocks
  localhost TCP/UDP tests, retry with appropriate approval.
- Run `make deps` only when formatting/linting tools are missing or requested;
  it installs pinned tools and may need network access. Put the host Go bin
  directory on `PATH`.

Manual UDP probes use private config and network access. Run only on explicit
request:

```bash
EFANADP_CONFIG=config.yaml EFANADP_NODE_INDEX=0 go test -tags manual ./internal/x365 -run TestManualX365UDPProbe
```

## Implementation

Follow package ownership in [architecture](docs/architecture.md#package-responsibilities).

- Keep TCP byte streams distinct from `datagram.Conn`.
- Do not infer network state from display labels or config error categories
  from diagnostic strings. Reuse existing parsers and helpers.
- Test changed behavior near its implementation with sanitized fixtures and
  injected upstreams/callbacks from `proxy.Config`; avoid live network tests.
- Base protocol changes on APK findings recorded in `docs/x365-protocol.md`.
- Keep UI/log terms consistent: `Config file`, `Node`, `Local SOCKS5`, `source`,
  and `target`.

## Documentation and Comments

Update the relevant document in the [index](docs/README.md) when behavior,
config, protocol, or Make targets change. Keep the README to onboarding and links.

Keep [GLOSSARY.md](GLOSSARY.md) to domain terms and `docs/adr/` to consequential
trade-offs and their reasons. When replacing an accepted decision, link its
successor and state what it supersedes.

Preserve adequate text; delete repetition or replace it with a link. Keep comments
for intent, constraints, ownership, and surprising behavior; omit code narration,
common knowledge, and obvious test descriptions. Write comments in English,
wrapping prose near 80 columns.
