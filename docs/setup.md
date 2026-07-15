# Setup and Operation

This guide covers daily use. Privacy rules live in
[`privacy.md`](privacy.md); config field details live in
[`configuration.md`](configuration.md); log details live in
[`logging.md`](logging.md); failure-mode notes live in
[`troubleshooting.md`](troubleshooting.md); protocol details live in
[`x365-protocol.md`](x365-protocol.md).

## First-Run Checklist

1. Prepare private runtime input: either put a decrypted node config at
   `config.yaml`, or copy `config.reference.yaml` and replace `proxies_uuid`.
2. Start the TUI with `make run`.
3. Select a node and point mihomo at the local SOCKS5 listener.

Keep `config.yaml`, `settings.json`, and runtime logs untracked. Use
[`privacy.md`](privacy.md) before sharing failures or examples.

The default local listener is:

```text
127.0.0.1:10865
```

The config path is resolved in this order:

1. `--config` / `-c`.
2. `configPath` in `settings.json` in the current working directory.
3. `config.yaml` in the current directory.

For a one-off config path, run:

```bash
make run ARGS="--config proxy.yaml"
```

The TUI persists the last successful config path and node in:

```text
settings.json
```

On the next launch without `--config`, it restores them and starts the last node
when that node still exists in the config.

## mihomo Setup

The mihomo rules need to do three separate jobs: send selected traffic to the
local adapter, keep the adapter's own node connection direct, and block only the
UDP/443 traffic that would otherwise use X365.

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
  # Scope UDP/443 rejection to the same traffic that would otherwise use
  # EFAN-X365-LOCAL. Do not use a bare global UDP/443 REJECT unless you want to
  # disable QUIC for all traffic.
  - AND,((NETWORK,UDP),(DST-PORT,443),(DOMAIN-SUFFIX,example.org)),REJECT
  - DOMAIN-SUFFIX,example.net,DIRECT
  - DOMAIN-SUFFIX,example.org,EFAN-X365-LOCAL
```

Replace `example.net` with the node server domain suffix from your private
config. Provider website domains are not required for adapter runtime unless you
have another reason to visit those sites directly.

Replace `example.org` with the same domain, process, or rule-set condition that
sends traffic to `EFAN-X365-LOCAL`. Keep the scoped UDP/443 rejection before
the matching proxy rule because the observed X365 client does not forward QUIC
through UDP/443. Keep `PROCESS-NAME,efanadp,DIRECT` near the top so the
adapter's own node connection cannot recurse through itself.

## TUI Controls

The UI labels are English. The recurring terms are `Config file`, `Node`,
`Local SOCKS5`, and `current service keeps running`.

Node list screen:

- `Left` / `Right`: page through nodes.
- `Up` / `Down`: select a node.
- `/`: filter nodes. While typing a filter, `esc` exits search mode.
- `Enter`: start the selected node, or confirm a node switch while the current
  service keeps running until the switch is confirmed.
- `esc`: cancel back to the running service while one is active.
- `q`: quit.

Running screen:

- `Left` / `Right`: page through active connections.
- `s`: choose another node while keeping the current one running until
  confirmed.
- `q`: quit.

To apply config file changes, stop `efanadp` and start it again.

## Logs

Runtime logs are written to the configured `log.output`. The default is:

```text
logs/app.log
```

Logs can contain local addresses, targets, and sniffed inner hosts. Keep them
out of git and redact them before sharing; see
[`privacy.md`](privacy.md) for report-safe summaries. Output options and event
examples are in [`logging.md`](logging.md).

## Smoke Tests

After the TUI shows a running service, test direct SOCKS egress with:

```bash
curl --fail --silent --show-error --proxy "socks5h://127.0.0.1:10865" "https://ifconfig.me/ip"
```

The command uses the default listener and external network access. Use
`socks5h://127.0.0.1:10865`; `socks5://` resolves the domain locally first and
can turn the target into a mihomo fake IP.

See [`troubleshooting.md`](troubleshooting.md) when curl hangs, a browser
behaves differently from curl, or mihomo shows fake-IP, QUIC, or UDP/443 traffic
that does not match the expected rules.
