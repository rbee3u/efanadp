# Setup and Operation

Follow the [quick start](../README.md#quick-start), then configure mihomo below.
The default listener is `127.0.0.1:10865`. To select another config:

```bash
make run ARGS="--config proxy.yaml"
```

See [configuration](configuration.md#runtime-config-changes) for startup path
selection, remembered settings, and applying config edits.

## Building from Source

`make help` lists all targets and examples. Build for the host platform with
`make build`, or select a target explicitly:

```bash
make build GOOS=linux GOARCH=arm64
```

The output is `dist/efanadp-<GOOS>-<GOARCH>`, built without CGO. Run it from the
directory containing your runtime files, or pass `--config proxy.yaml`.

When running the built binary, update the mihomo `PROCESS-NAME` rule below to
match its filename (for example, `efanadp-linux-arm64`), or rename the binary
to `efanadp`.

For contributor commands and validation, see [AGENTS.md](../AGENTS.md).

## mihomo Setup

Add `efanadp` as a local SOCKS outbound:

```yaml
proxies:
  - name: EFAN-X365-LOCAL
    type: socks5
    server: 127.0.0.1
    port: 10865
    udp: true
```

Make sure the adapter's own outbound connection to the node server does not
loop back into mihomo:

```yaml
rules:
  - PROCESS-NAME,efanadp,DIRECT
  - AND,((NETWORK,UDP),(DST-PORT,443),(DOMAIN-SUFFIX,example.org)),REJECT
  - DOMAIN-SUFFIX,example.net,DIRECT
  - DOMAIN-SUFFIX,example.org,EFAN-X365-LOCAL
```

Replace `example.net` with the node-server domain suffix and `example.org` with
the adapter-bound condition. Keep the process bypass and scoped UDP/443 reject
before the proxy rule: the first prevents loops, the second avoids the
unsupported QUIC path. A global UDP/443 reject would disable unrelated QUIC.

## TUI Controls

| Key | Node list | Running screen |
| --- | --- | --- |
| `Left` / `Right` | Page through nodes | Page through connections |
| `Up` / `Down` | Select a node | — |
| `/` | Filter nodes | — |
| `Enter` | Start or switch to the selected node | — |
| `esc` | Exit search, otherwise return to the active service | — |
| `s` | — | Choose another node; the current service runs until confirmed |
| `q` | Quit outside search mode | Quit |

`Ctrl+C` always quits. During shutdown, a second `q` or `Ctrl+C` forces exit.

## Smoke Tests

After the TUI shows a running service, test direct SOCKS egress with:

```bash
curl --fail --silent --show-error --proxy "socks5h://127.0.0.1:10865" "https://ifconfig.me/ip"
```

This uses external network access. `socks5h://` leaves domain resolution to the
remote side, avoiding local mihomo fake IPs. See [troubleshooting](troubleshooting.md)
if it fails.
