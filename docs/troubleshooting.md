# Troubleshooting

Start with [`setup.md`](setup.md) for the normal workflow and
[`configuration.md`](configuration.md) for config shape. This page focuses on
symptoms and the smallest safe checks to isolate them.

When sharing a failure, describe the command or visible TUI status and redact
private config, node URLs, provider domains, and logs. See
[`privacy.md`](privacy.md) for report-safe examples.

## Quick Triage

| Symptom | First check |
| --- | --- |
| Direct curl hangs or shows fake-IP behavior | Use `socks5h://127.0.0.1:10865`. |
| Browser differs from curl | Disable browser Secure DNS/DoH for the test. |
| Startup or handshake recurses | Add `PROCESS-NAME,efanadp,DIRECT` before proxy rules. |
| QUIC or Secure DNS stalls | Scope UDP/443 rejection to adapter-bound traffic. |
| Config load fails at startup | Fix the file or pass the intended path with `--config`. |

## curl Hangs or Uses a Fake IP

Use `socks5h://`, not `socks5://`, when testing the adapter directly:

```bash
curl -x socks5h://127.0.0.1:10865 https://ifconfig.me/ip
curl -x socks5h://127.0.0.1:10865 https://api.ipify.org
```

`socks5://` resolves the domain locally first. With mihomo fake-ip enabled, that
can produce a `198.18.0.0/15` address that the remote X365 side cannot map back
to the original domain. `efanadp` rejects those fake-IP targets and logs a hint
to use `socks5h://`.

## Browser Differs From curl

If curl shows the proxied IP but a browser shows the real IP, check the
browser's Secure DNS or DNS-over-HTTPS setting. For Chrome:

```text
chrome://settings/security
Use secure DNS -> off
```

Browser DoH can bypass mihomo's DNS handling. With TUN + fake-ip setups, that
can make domain rules behave differently between browser traffic and command-line
tools.

## UDP/443, QUIC, and Secure DNS Stall

`efanadp` supports SOCKS5 `UDP_ASSOCIATE` for non-443 UDP, but the observed X365
client does not forward UDP/443 through the X365 UDP path. Scope a UDP/443 rule
to the same traffic that would otherwise use the adapter:

```yaml
rules:
  - PROCESS-NAME,efanadp,DIRECT
  # Keep this scoped to adapter-bound traffic. A bare UDP/443 REJECT disables
  # QUIC globally.
  - AND,((NETWORK,UDP),(DST-PORT,443),(DOMAIN-SUFFIX,example.org)),REJECT
  - DOMAIN-SUFFIX,example.org,EFAN-X365-LOCAL
```

Use the same domain, process, or rule-set condition that routes traffic to
`EFAN-X365-LOCAL`. Keep this rule before the matching proxy rule.

## Adapter Traffic Loops Through mihomo

The adapter's own connection to the node server must not be routed back to the
adapter. Put a process rule before the proxy rule:

```yaml
rules:
  - PROCESS-NAME,efanadp,DIRECT
  - DOMAIN-SUFFIX,example.net,DIRECT
  - DOMAIN-SUFFIX,example.org,EFAN-X365-LOCAL
```

Replace `example.net` with the node server domain suffix from your private
config. If the node server itself is captured by the adapter rule, startup or
handshake attempts can recurse until they time out.

## Config Does Not Load

Common checks:

- The resolved file exists. First use usually means copying
  `config.reference.yaml` to `config.yaml`, or passing an explicit path with
  `--config`.
- `proxies_uuid` has been replaced with your local UUID when using the template.
- Plain decrypted configs still have a top-level `proxies` map.
- Node URLs include `path`, `host`, `sni`, `pbk`, and `sid`.
- `sniff.shadowsocks` entries include `server`, `port`, `method`, and
  `password` when that optional block is present.

`efanadp` loads config once during startup. Fix the file, then start the process
again.

## UDP Expectations

Current policy is intentionally narrow:

- UDP/443 is dropped until a fresh APK finding proves X365 forwards QUIC through
  UDP.
- SOCKS5 UDP fragments are dropped; reassembly is not implemented.
- Non-443 UDP targets are cached per destination inside one `UDP_ASSOCIATE`
  association.

Do not loosen these rules based on generic proxy behavior alone. Update
[`x365-protocol.md`](x365-protocol.md) with reverse findings before changing
protocol policy.
