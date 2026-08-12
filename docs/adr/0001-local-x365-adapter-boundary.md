---
status: accepted
---

# Local X365 Adapter boundary and service ownership

This ADR retrospectively records the implemented baseline for a local adapter
around decrypted efanapp X365 nodes. The decision keeps the product boundary
narrow so future changes have an explicit architectural reference. The
canonical terms used here are defined in [`CONTEXT.md`](../../CONTEXT.md).

## Decision

- efanadp is a trusted-host Local X365 Adapter. It accepts SOCKS5 `CONNECT` and
  `UDP_ASSOCIATE` traffic and carries it through a selected X365 Node.
- mihomo, or another local Traffic Orchestrator, is optional. When present, it
  owns TUN capture, DNS, and routing; efanadp does not duplicate those duties.
- A process uses one Runtime Configuration Snapshot. Configuration changes are
  applied by restarting the process rather than by hot reload.
- The TUI owns at most one Service Session at a time. Confirming a node switch
  stops the current session before starting the next one. If the next session
  fails, efanadp returns to node selection and does not automatically roll back
  to the previous node.
- The current boundary excludes node decryption, subscription retrieval,
  provider or account management, TUN, DNS, routing, and remote multi-user
  authentication. Adding one of these responsibilities requires a new ADR that
  extends or supersedes this decision.

This separation keeps private node material and reverse-engineered transport
behavior inside one focused adapter, while leaving general traffic orchestration
to tools designed for it. Single-snapshot, single-session ownership makes the
listener and upstream resource lifecycle unambiguous; the trade-off is restart-
based configuration and a service gap during node switches.

Protocol byte layouts and reverse-engineering constraints remain documented in
[`docs/x365-protocol.md`](../x365-protocol.md); this ADR does not replace that
reference.

## Consequences

- Node switching has a deliberate service gap and resets the Connection
  Projection to the new session.
- The supported trust model is local and no-auth. A non-loopback listen address
  can be configured by the current implementation, but remote exposure is not
  a supported multi-user deployment contract.
- The adapter currently restores a remembered node by Node Label, persists an
  explicitly supplied config path, and may save the preference before the
  listener has successfully bound. These are current behaviors, not stronger
  guarantees of node identity or service availability.

## Follow-ups

- Decide whether remembered-node persistence should use a stable Node ID rather
  than a display label.
- Decide whether preference writes should wait for a successful listener bind
  and whether explicit `--config` should persist.
- If remote or multi-user SOCKS exposure becomes a requirement, define
  authentication and access control in a superseding ADR.
