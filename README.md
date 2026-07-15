# efanadp

`efanadp` runs a local SOCKS5 listener for decrypted efanapp `x365://` nodes.
It is intended to sit behind mihomo: mihomo keeps TUN capture, DNS, and routing
rules, while `efanadp` translates selected SOCKS5 TCP and non-443 UDP traffic
into the recovered X365 transport.

It does not decrypt nodes, fetch subscriptions, or manage provider accounts.
Private runtime material belongs in untracked local files.

```text
app traffic -> mihomo -> socks5://127.0.0.1:10865 -> efanadp -> X365 node
```

## Quick Start

Prerequisites: the Go version declared in `go.mod`, mihomo for TUN/rule
routing, and either a decrypted private X365 config or the committed template
plus your X365 UUID.

1. Prepare private runtime input:

   ```bash
   cp config.reference.yaml config.yaml
   ```

   Replace `proxies_uuid` with your UUID. If you already have a decrypted
   config, put it at `config.yaml` instead.

2. Start the TUI:

   ```bash
   make run
   ```

3. Select a node, then point mihomo at `127.0.0.1:10865`.

See [setup](docs/setup.md) for the full operating guide.

## Runtime Privacy

Keep `config.yaml`, `settings.json`, private node URLs, account UUIDs, REALITY
material, provider domains, captures, and live logs out of git and out of docs.
Use sanitized placeholders such as `example.org`, `example.net`,
`203.0.113.10`, and `<your-x365-uuid>`; do not derive examples by editing
private values.

`config.reference.yaml` removes only account UUID userinfo from URL templates,
but compression and base64 are packaging, not redaction. Review regenerated
templates before committing them, especially if `proxies_tmpl` changes. See
[privacy](docs/privacy.md) for the project rules.

## mihomo

Add a local SOCKS outbound:

```yaml
proxies:
  - name: EFAN-X365-LOCAL
    type: socks5
    server: 127.0.0.1
    port: 10865
    udp: true
```

Put guard rules before the adapter-bound proxy rule:

```yaml
rules:
  - PROCESS-NAME,efanadp,DIRECT
  - AND,((NETWORK,UDP),(DST-PORT,443),(DOMAIN-SUFFIX,example.org)),REJECT
  - DOMAIN-SUFFIX,example.net,DIRECT
  - DOMAIN-SUFFIX,example.org,EFAN-X365-LOCAL
```

Replace `example.net` with the node server domain suffix from your private
config. Replace `example.org` with the same condition that routes traffic to
`EFAN-X365-LOCAL`. Keep UDP/443 rejection scoped to adapter-bound traffic; a
global reject disables QUIC for unrelated traffic.

## Configuration

`config.yaml` can be either:

- a decrypted config with a top-level `proxies` map
- a copy of `config.reference.yaml` with `proxies_uuid` filled locally

Optional runtime fields include `listen` (default `127.0.0.1:10865`), `log`
for level/output/format, and `sniff.shadowsocks` for display-only inner target
sniffing behind a Shadowsocks landing server.

See [configuration](docs/configuration.md) for the full config reference.

## Useful Commands

`make help` is the authoritative target list. Common shortcuts:

```bash
make run ARGS="--config proxy.yaml"
make build
make test
make lint
make config-reference
```

`make config-reference` reads private input. `make deps` installs pinned
formatting and linting tools with the host Go toolchain, may need network
access, and writes to the host Go bin directory.

## Documentation

Start with [docs/README.md](docs/README.md). Frequent jumps:
[privacy](docs/privacy.md), [setup](docs/setup.md),
[logging](docs/logging.md), [troubleshooting](docs/troubleshooting.md),
[configuration](docs/configuration.md), and
[X365 protocol notes](docs/x365-protocol.md).

## Scope and Limits

Supported:

- Interactive TUI for node selection, node switching, and connection counters.
- Local SOCKS5 TCP and `UDP_ASSOCIATE` for non-443 UDP.
- X365 xhttp + REALITY/uTLS transport and recovered multipath TCP relay.
- Optional Shadowsocks target sniffing, runtime logs, and TUI connection
  counters.

Limits:

- UDP/443 remains disabled because the observed APK falls back instead of
  forwarding QUIC through X365 UDP.
- SOCKS5 UDP fragments are dropped; reassembly is not implemented.
- Multipath retransmit/window behavior is intentionally minimal.
- X365 or multipath framing changes require fresh reverse findings plus updates
  to this adapter and [docs/x365-protocol.md](docs/x365-protocol.md).
