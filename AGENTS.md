# AGENTS.md

This repository is a Go application that runs a local SOCKS5 adapter for
decrypted efanapp `x365://` nodes. Treat it as private-networking software:
protect account material, keep protocol notes precise, and prefer small,
verified changes.

## Safety Rules

- Treat `config.yaml`, private node URLs, account UUIDs, REALITY keys, captures,
  provider domains, and live logs as private. Never commit, quote, or derive
  examples from them. Do not open private config unless the user explicitly asks
  for runtime debugging; when reporting failures, summarize and redact.
- Use only sanitized placeholders in docs and tests, such as `example.org`,
  `example.net`, `203.0.113.10`, and `<your-x365-uuid>`.
- Keep `config.reference.yaml` as the committed, sanitized reference template.
  Regenerate it with `make config-reference` only when the config shape changes;
  the generator strips X365 UUID userinfo only, so inspect template changes for
  leaked hosts, paths, keys, logs, or captures before committing.
- Treat zlib-compressed base64 template payloads as packaging, not redaction. Do
  not decode, decompress, expand, or quote `proxies_tmpl` unless the task is
  explicitly to audit or regenerate the template, and report only sanitized
  findings.
- Do not loosen UDP/443 or QUIC behavior without a fresh APK finding that proves
  X365 forwards QUIC through the UDP path. Document any such finding in
  `docs/x365-protocol.md`.

## Common Commands

```bash
make help
make deps
make style
make format
make build
make test
make race
make lint
make run
make curl
make config-reference
```

Go-backed targets use the in-repo `.gocache`; `go.mod` declares the required Go
version. Prefer the Make targets for Go work. Do not run bare Go commands when a
Make target exists, because the Makefile pins `GOCACHE` to `.gocache/build` and
`GOMODCACHE` to `.gocache/mod`.
Use `ARGS` for target arguments, such as:

```bash
make run ARGS="--config proxy.yaml"
make config-reference ARGS="--in config.yaml --out config.reference.yaml"
```

When a direct Go command is unavoidable, set both caches explicitly. Setting
only `GOCACHE` is not enough, because module downloads and locks otherwise go to
the user-level module cache.

```bash
GOCACHE="$(pwd)/.gocache/build" GOMODCACHE="$(pwd)/.gocache/mod" go get ...
GOCACHE="$(pwd)/.gocache/build" GOMODCACHE="$(pwd)/.gocache/mod" go mod tidy
GOCACHE="$(pwd)/.gocache/build" GOMODCACHE="$(pwd)/.gocache/mod" go test ./...
GOCACHE="$(pwd)/.gocache/build" GOMODCACHE="$(pwd)/.gocache/mod" go build ./...
GOCACHE="$(pwd)/.gocache/build" GOMODCACHE="$(pwd)/.gocache/mod" go run ./cmd/efanadp
GOCACHE="$(pwd)/.gocache/build" GOMODCACHE="$(pwd)/.gocache/mod" go list ./...
GOCACHE="$(pwd)/.gocache/build" GOMODCACHE="$(pwd)/.gocache/mod" go install ...
```

When running `golangci-lint` directly instead of `make lint`, also use the
in-repo lint cache:

```bash
GOCACHE="$(pwd)/.gocache/build" GOMODCACHE="$(pwd)/.gocache/mod" GOLANGCI_LINT_CACHE="$(pwd)/.gocache/lint" ./.gocache/bin/golangci-lint run
```

If a Go command still fails after using the in-repo caches, request escalation
only for network downloads, proxy access, or other external resources; do not
request escalation merely to write Go build, module, or lint caches.

- Run `make deps` only when formatting or linting tools are missing or the user
  asks, because it installs pinned tools into `.gocache/bin` and may need
  network access.
- Use `make style` for non-mutating formatting checks, `make format` to rewrite
  formatting, `make race` for race detection, and `make lint` for configured
  `golangci-lint` checks.
- `make run` starts the interactive TUI; do not use it for routine validation.
- `make curl` requires the local SOCKS5 proxy and external network access; use
  it only when live runtime verification is needed.

Manual UDP probing is guarded by the `manual` build tag, uses private config,
and may touch the network. Run it only when explicitly asked:

```bash
GOCACHE="$(pwd)/.gocache/build" GOMODCACHE="$(pwd)/.gocache/mod" EFANADP_CONFIG=config.yaml EFANADP_NODE_INDEX=0 go test -tags manual ./internal/x365 -run TestManualX365UDPProbe
```

## Validation

- Before editing, check `git status --short` and preserve unrelated user
  changes. If files are already staged, inspect `git diff --cached` before
  layering edits.
- For code changes, run `make format`, `make build`, `make test`,
  `make race`, and `make lint` in that order. Review the diff after
  `make format`; keep relevant formatting edits with the change and call out
  unexpected file changes.
- For docs-only changes, review links, file names, and command names against the
  repository. Use `docs/` for project docs paths; this repo does not use
  `doc/`. Run `make help` when command docs or this file's command list
  change.
- For generated `config.reference.yaml` payload or config-shape changes, run
  `make config-reference`, then review the diff for sanitized output before any
  commit. For comment-only template wording, keep
  `internal/config/reference.go` and `config.reference.yaml` in sync.
- If validation fails or cannot run, report the exact command and reason. Some
  tests bind localhost TCP/UDP sockets; if a sandbox blocks that, retry with the
  appropriate approval before giving up.

## Code Map

- `cmd/efanadp/`: process entry point. No args launches the TUI;
  `config-reference` regenerates the committed template.
- `internal/app/`: command-line dispatch and manual dependency wiring.
- `internal/config/`: YAML config parsing, template UUID insertion, and
  sanitized `config.reference.yaml` generation.
- `internal/tui/`: Bubble Tea UI, startup config resolution/loading, settings,
  node switching, logs, and connection tables.
- `internal/proxy/`: SOCKS5 server, connection accounting, TCP relay,
  UDP_ASSOCIATE, and local SOCKS UDP envelope handling.
- `internal/x365/`: X365 nodes, request/frame bytes, multipath and direct UDP
  streams, xhttp + REALITY transport, and Shadowsocks target sniffing.
- `internal/human/`: small formatting/text helpers shared by UI and
  maintenance code.
- `docs/`: project docs. Keep runtime and protocol decisions here instead of
  growing the README.

## Documentation Map

- Update docs in the same change as user-facing behavior, config shape, protocol
  behavior, or Makefile targets.
- Update `README.md` only for the shortest onboarding path.
- Treat `docs/` as the canonical docs directory. If a prompt says `doc/`, check
  whether it means `docs/`; do not create a second docs tree unless explicitly
  asked to rename.
- Update `docs/README.md` when the documentation index or document set changes.
- Update `docs/privacy.md` when private-material handling, sanitization rules,
  template safety, or sharing guidance changes.
- Update `docs/setup.md` for operation, mihomo rules, TUI controls, logs, and
  smoke tests.
- Update `docs/troubleshooting.md` for curl, fake-IP, browser DNS/DoH, loop,
  QUIC, UDP/443, and config-load troubleshooting.
- Update `docs/configuration.md` when `config.yaml`, `config.reference.yaml`, or
  `sniff.shadowsocks` changes.
- Update `docs/architecture.md` when package responsibilities, TUI lifecycle, or
  relay behavior changes.
- Update `docs/x365-protocol.md` when reverse findings change protocol bytes,
  frame layouts, request headers, or invariants.
- Update `AGENTS.md` when safety rules, validation flow, or Makefile targets
  change.

## Implementation Notes

- Respect existing package boundaries. UI orchestration belongs in
  `internal/tui`; config parsing in `internal/config`; SOCKS adapter lifecycle
  in `internal/proxy`; network protocol behavior in `internal/x365`.
- Prefer existing parsers and helpers for config, URI, and protocol data instead
  of ad hoc string handling.
- Keep tests close to changed behavior. Existing tests cover config parsing, TUI
  state transitions, SOCKS5 parsing, TCP/UDP relay framing, Shadowsocks
  sniffing, and connection accounting. Use sanitized fixtures; never derive test
  data from private nodes or logs.
- Avoid live network dependencies in tests. `proxy.Config` has injectable
  upstreams and callbacks; use those unless a manual probe was explicitly
  requested.
- Treat reverse-engineered X365 behavior as the source of truth; do not infer
  relay or protocol changes from generic proxy behavior alone.
- Keep log wording and TUI labels consistent with the docs: `Config file`,
  `Node`, `Local SOCKS5`, `source`, and `target`.
