---
status: accepted
---

# One configuration snapshot and one active service session

A process loads one configuration snapshot and runs at most one service session.
Node switching prepares the replacement offline, stops and awaits the old
session, then starts the replacement. This trades hot reload and uninterrupted
handover for consistent configuration and exclusive listener ownership.

Preparation validates authentication, not reachability; failure leaves the
current session running. Switching interrupts connections; a bind failure
returns to node selection without rollback. Config edits require a restart.

See [architecture](../architecture.md#runtime-flow-and-ownership) for ownership
and [settings](../configuration.md#remembered-settings) for restoration behavior.
