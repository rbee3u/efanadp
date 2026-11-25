---
status: accepted
---

# Follow 1.0.71's direct X365 TCP shape

efanapp 1.0.71 removed the multipath layer and puts the real target in the X365
request. The adapter follows that shape and deletes its multipath framing instead
of keeping both generations. The server still accepts the old shape, but a client
must not depend on that compatibility path; keeping it would preserve obsolete
framing, ACK, and close machinery without a supported caller.

Consequence: nodes behind a server that never accepts direct TCP stop working.
The 1.0.71 client already uses direct TCP for every stream, so that combination
is unsupported by the vendor as well. See
[protocol](../x365-protocol.md#protocol-generations) for both shapes.
