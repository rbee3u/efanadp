package x365

import (
	"context"
	"io"
	"time"
)

type Client struct {
	node    Node
	userID  UserID
	timeout time.Duration
}

func NewClient(node Node, userID UserID, timeout time.Duration) *Client {
	return &Client{
		node:    node,
		userID:  userID,
		timeout: timeout,
	}
}

func (c *Client) OpenTCP(ctx context.Context, destHost string, destPort int) (io.ReadWriteCloser, error) {
	return openX365Multipath(ctx, c.node, c.userID, c.timeout, destHost, destPort)
}

func (c *Client) OpenUDP(ctx context.Context, destHost string, destPort int) (io.ReadWriteCloser, error) {
	return openX365UDP(ctx, c.node, c.userID, c.timeout, destHost, destPort)
}
