// Package x365 implements multipath TCP and direct UDP over xhttp + REALITY.
// Protocol changes require APK evidence recorded in docs/x365-protocol.md.
package x365

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/rbee3u/efanadp/internal/datagram"
)

// Client opens TCP and UDP streams through one X365 node.
type Client struct {
	node    Node
	userID  UserID
	timeout time.Duration
}

// NewClient prepares node authentication and binds it to an opening timeout.
// A non-positive timeout disables the added deadline.
func NewClient(node Node, timeout time.Duration) (*Client, error) {
	userID, err := node.UserID()
	if err != nil {
		return nil, err
	}
	return &Client{
		node:    node,
		userID:  userID,
		timeout: timeout,
	}, nil
}

// OpenTCP opens a multipath stream to the destination. The caller owns the
// returned stream and must Close it. Canceling ctx also ends its transport.
func (c *Client) OpenTCP(ctx context.Context, destHost string, destPort int) (io.ReadWriteCloser, error) {
	stream, err := openXHTTPRealityStream(ctx, c.node, c.timeout)
	if err != nil {
		return nil, fmt.Errorf("open xhttp reality stream: %w", err)
	}
	conn, err := handshakeWithDeadline(ctx, c.timeout, stream, func() (io.ReadWriteCloser, error) {
		return openMultipath(stream, c.userID, destHost, destPort)
	})
	if err != nil {
		return nil, fmt.Errorf("open multipath stream: %w", err)
	}
	return conn, nil
}

// OpenUDP opens a direct UDP stream to the destination, carrying one datagram
// per ReadDatagram or WriteDatagram. The caller owns the connection and must
// Close it. Canceling ctx also ends its transport.
func (c *Client) OpenUDP(ctx context.Context, destHost string, destPort int) (datagram.Conn, error) {
	stream, err := openXHTTPRealityStream(ctx, c.node, c.timeout)
	if err != nil {
		return nil, fmt.Errorf("open xhttp reality stream: %w", err)
	}
	// openUDP defers the response read to ReadDatagram. This deadline covers
	// the request write; the association's idle sweep bounds a stalled response.
	conn, err := handshakeWithDeadline(ctx, c.timeout, stream, func() (datagram.Conn, error) {
		return openUDP(stream, c.userID, destHost, destPort)
	})
	if err != nil {
		return nil, fmt.Errorf("open x365 udp stream: %w", err)
	}
	return conn, nil
}
