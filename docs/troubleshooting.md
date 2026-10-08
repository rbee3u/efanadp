# Troubleshooting

For normal operation see [setup](setup.md). Before sharing diagnostics, follow
[privacy](privacy.md).

## curl Hangs or Uses a Fake IP

Use the [SOCKS smoke test](setup.md#smoke-tests) with `socks5h://`.

`socks5://` resolves the domain locally first. With mihomo fake-ip enabled, that
can produce a `198.18.0.0/15` address that the remote X365 side cannot map back
to the original domain. `efanadp` rejects those fake-IP targets and logs a hint
to use `socks5h://`.

## Browser Differs From curl

If curl uses the proxy but a browser does not, temporarily disable browser
Secure DNS/DoH and compare mihomo's connection/rule matches. Browser DNS can
bypass mihomo's DNS handling and affect domain matching in TUN + fake-IP setups.

## UDP/443, QUIC, and Secure DNS Stall

UDP/443 is dropped because the observed APK avoids the X365 UDP path for QUIC.
Use the [scoped reject rule](setup.md#mihomo-setup) before the matching adapter
rule. Rejecting all UDP/443 would affect unrelated traffic.

## Adapter Traffic Loops Through mihomo

Apply the [process and node-server bypass rules](setup.md#mihomo-setup) before
adapter-bound rules. Match `PROCESS-NAME` to the actual binary filename;
otherwise the adapter's node connection can recurse until it times out.

## Config Does Not Load

Common checks:

- The resolved file exists. First use usually means copying
  `config.reference.yaml` to `config.yaml`, or passing an explicit path with
  `--config`.
- `proxies_uuid` has been replaced with your local UUID when using the template.
- Plain decrypted configs still have a top-level `proxies` map.
- Node URLs and optional sniffing entries have the required
  [fields](configuration.md).

Fix the file and restart. See [path selection](configuration.md#runtime-config-changes)
if the app loads an unexpected config.

## UDP Expectations

For queue, target-limit, or response drops, see [logging](logging.md#event-lines).
See [UDP policy](x365-protocol.md#udp-path) for supported traffic and
[relay lifecycle](architecture.md#udp-relay) for target caching and expiry.
