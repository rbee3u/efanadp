// Package x365 implements the recovered X365 node transport: x365:// node
// parsing, request and frame bytes, the multipath TCP stream, the direct UDP
// stream, the xhttp + REALITY carrier, and display-only Shadowsocks target
// sniffing.
//
// Byte layouts and framing rules here come from reverse-engineering the APK and
// are the source of truth for this adapter; do not adjust them to match generic
// proxy behavior. Record any new finding in docs/x365-protocol.md.
package x365

import (
	"context"
	"io"
	"time"
)

// Client opens TCP and UDP streams through one X365 node.
type Client struct {
	node    Node
	userID  UserID
	timeout time.Duration
}

// NewClient binds a validated node and user ID to a transport-opening timeout.
// A non-positive timeout disables the added deadline.
func NewClient(node Node, userID UserID, timeout time.Duration) *Client {
	return &Client{
		node:    node,
		userID:  userID,
		timeout: timeout,
	}
}

// OpenTCP opens a multipath stream to the destination. The caller owns the
// returned stream and must Close it; ctx bounds the open only, not later I/O.
func (c *Client) OpenTCP(ctx context.Context, destHost string, destPort int) (io.ReadWriteCloser, error) {
	return openX365Multipath(ctx, c.node, c.userID, c.timeout, destHost, destPort)
}

// OpenUDP opens a direct UDP stream to the destination, carrying one datagram
// per Read or Write rather than a byte stream. The caller owns the returned
// stream and must Close it; ctx bounds the open only, not later I/O.
func (c *Client) OpenUDP(ctx context.Context, destHost string, destPort int) (io.ReadWriteCloser, error) {
	return openX365UDP(ctx, c.node, c.userID, c.timeout, destHost, destPort)
}
