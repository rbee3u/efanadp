# Local X365 Adapter

This context covers local SOCKS5 access through decrypted X365 nodes. Its terms
distinguish the adapter from traffic orchestration and account management.

## Language

**Local X365 Adapter**:
A local service relaying SOCKS5 requests through a selected, decrypted X365 Node.
_Avoid_: VPN, node manager, account client

**Traffic Orchestrator**:
An optional companion, such as mihomo, responsible for traffic capture, DNS, and
routing before requests reach the adapter.

**X365 Node**:
An endpoint description containing the transport and account material needed to
open X365 streams.

**Node Label**:
A node's display and selection name, without a stable identity guarantee across
config edits.
_Avoid_: Node ID

**Runtime Configuration Snapshot**:
The configuration shared by all service sessions within one process lifetime.

**Service Session**:
One local SOCKS service run for a selected node and configuration snapshot.

**SOCKS Control Connection**:
The client TCP connection carrying one CONNECT relay or owning one UDP Association.

**UDP Association**:
A UDP relay whose client ownership and per-destination targets live under one
SOCKS Control Connection.
_Avoid_: UDP stream

**Upstream Stream**:
An X365 byte stream toward a remote target or control endpoint.
_Avoid_: SOCKS connection

**Target Chain**:
A display label combining the SOCKS target with a sniffed inner target; it has
no routing authority.

**Connection Projection**:
The current session's active connections, closed count, labels, and byte statistics.
