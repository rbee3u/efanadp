---
status: accepted
---

# Local X365 Adapter product and trust boundary

efanadp relays local SOCKS5 traffic through decrypted X365 nodes. TUN capture,
DNS, and routing belong to an optional companion such as mihomo; decryption,
subscriptions, and account management remain outside the adapter. This limits
private node handling without duplicating traffic orchestration.

SOCKS5 uses no-auth for trusted loopback clients. Binding beyond loopback provides
no multi-user isolation; supporting it requires authentication and access control.
