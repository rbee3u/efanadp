# efanadp

A local SOCKS5 adapter for decrypted efanapp `x365://` nodes. It carries TCP and
non-443 UDP through X365; mihomo can handle TUN capture, DNS, and routing.
Node decryption, subscriptions, and account management are outside its scope.

```text
app traffic -> mihomo -> socks5://127.0.0.1:10865 -> efanadp -> X365 node
```

## Quick Start

Requires the Go version in `go.mod` and either a decrypted node config or your
X365 UUID for the committed template.

1. Put your decrypted config at `config.yaml`, or copy the template and replace
   `proxies_uuid` with your UUID:

   ```bash
   cp config.reference.yaml config.yaml
   ```

2. Start the TUI and select a node:

   ```bash
   make run
   ```

3. Point your SOCKS client at `127.0.0.1:10865`. For mihomo, apply the
   [outbound and guard rules](docs/setup.md#mihomo-setup).

Keep the listener on loopback: SOCKS5 supports no-auth only. Private configs,
settings, and logs must stay untracked; see [privacy](docs/privacy.md).

See [setup](docs/setup.md) for controls, builds, and smoke tests,
[configuration](docs/configuration.md) for fields and startup settings, and
[troubleshooting](docs/troubleshooting.md) for connection failures.
`make help` lists development commands. The [documentation index](docs/README.md)
links architecture, ADRs, and protocol details.
