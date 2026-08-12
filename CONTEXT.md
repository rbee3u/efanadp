# Local X365 Adapter

The Local X365 Adapter context describes a trusted-host service that accepts
local SOCKS5 traffic and carries selected requests through already-decrypted
X365 Nodes. It does not own traffic capture, name resolution, routing, node
decryption, subscriptions, or account management.

## Language

**Local X365 Adapter**:
The product boundary that translates local SOCKS5 requests into X365 upstream
traffic for a selected X365 Node.
_Avoid_: gateway, VPN, node manager, account client

**Traffic Orchestrator**:
An optional companion such as mihomo that owns TUN capture, DNS, and routing
before traffic reaches the adapter.
_Avoid_: adapter, upstream, node

**X365 Node**:
A parsed endpoint description that supplies the remote transport and account
material needed to open X365 upstream streams.
_Avoid_: proxy group, node label, account

**Node Label**:
The human-readable name used to display and select an X365 Node.
It is not a guaranteed stable identity for restoring a node across config edits.
_Avoid_: Node ID, endpoint identity

**Runtime Configuration Snapshot**:
The single configuration loaded for one process lifetime, including the listen
address, logging policy, node source, and optional target-sniffing rules.
_Avoid_: live config, hot-reloaded config

**Service Session**:
One running local SOCKS service associated with one selected X365 Node and one
Runtime Configuration Snapshot.
_Avoid_: application, connection, node

**SOCKS Control Connection**:
The client-side TCP connection that negotiates SOCKS5 and either carries one
CONNECT relay or owns one UDP Association.
_Avoid_: upstream connection, socket

**UDP Association**:
The lifetime-scoped relay created by SOCKS5 UDP_ASSOCIATE, including its local
UDP endpoint, client ownership, and per-destination UDP targets.
_Avoid_: UDP connection, UDP stream

**Upstream Stream**:
An X365-carried byte stream opened by the adapter toward a remote target or
control path.
_Avoid_: node connection, SOCKS connection

**Target Chain**:
The display label combining an outer SOCKS target with an optionally sniffed
inner target, written as `outer->inner`.
It is observability metadata and never the routing authority.
_Avoid_: resolved route, effective target

**Connection Projection**:
The current Service Session's observable view of active connections, closed
count, labels, and byte statistics.
It is session-scoped rather than process-wide history.
_Avoid_: connection history, accounting ledger
