package x365

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"time"
)

const x365UDPMaxPayload = 0xffff

func openX365UDP(
	ctx context.Context,
	node Node,
	userID UserID,
	timeout time.Duration,
	destHost string,
	destPort int,
) (io.ReadWriteCloser, error) {
	stream, err := openXHTTPRealityStream(ctx, node, timeout)
	if err != nil {
		return nil, fmt.Errorf("open xhttp reality stream: %w", err)
	}
	conn, err := openUDP(stream, userID, destHost, destPort)
	if err != nil {
		return nil, fmt.Errorf("open x365 udp stream: %w", err)
	}
	return conn, nil
}

func openUDP(stream io.ReadWriteCloser, userID UserID, destHost string, destPort int) (io.ReadWriteCloser, error) {
	opened := false
	defer func() {
		if !opened {
			closeQuietly(stream)
		}
	}()

	req, err := buildX365RequestWithCommand(destHost, destPort, userID, x365CommandUDP)
	if err != nil {
		return nil, fmt.Errorf("build x365 udp request: %w", err)
	}
	if err := writeFull(stream, req); err != nil {
		return nil, fmt.Errorf("write x365 udp request: %w", err)
	}
	opened = true
	return &x365UDPConn{stream: stream}, nil
}

type x365UDPConn struct {
	stream  io.ReadWriteCloser
	writeMu sync.Mutex

	responseOnce sync.Once
	responseErr  error
	closeOnce    sync.Once
	closeErr     error
}

var _ io.ReadWriteCloser = (*x365UDPConn)(nil)

func (c *x365UDPConn) Read(p []byte) (int, error) {
	c.responseOnce.Do(func() {
		c.responseErr = readX365OKResponse(c.stream)
	})
	if c.responseErr != nil {
		return 0, c.responseErr
	}
	return readX365UDPDatagram(c.stream, p)
}

func (c *x365UDPConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	return writeX365UDPDatagram(c.stream, p)
}

func (c *x365UDPConn) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.stream.Close()
	})
	return c.closeErr
}

func readX365UDPDatagram(r io.Reader, p []byte) (int, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, err
	}
	payloadLen := int(header[0])<<8 | int(header[1])
	if payloadLen <= len(p) {
		if _, err := io.ReadFull(r, p[:payloadLen]); err != nil {
			return 0, err
		}
		return payloadLen, nil
	}

	if len(p) > 0 {
		if _, err := io.ReadFull(r, p); err != nil {
			return 0, err
		}
	}
	if err := discardFull(r, payloadLen-len(p)); err != nil {
		return len(p), err
	}
	return len(p), nil
}

func writeX365UDPDatagram(w io.Writer, p []byte) (int, error) {
	payloadLen := len(p)
	if payloadLen > x365UDPMaxPayload {
		return 0, fmt.Errorf("X365 UDP payload too large: %d", payloadLen)
	}
	header := [2]byte{}
	binary.BigEndian.PutUint16(header[:], uint16(payloadLen))
	if err := writeFull(w, header[:]); err != nil {
		return 0, err
	}
	if err := writeFull(w, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func discardFull(r io.Reader, n int) error {
	var buf [512]byte
	for n > 0 {
		chunk := min(n, len(buf))
		if _, err := io.ReadFull(r, buf[:chunk]); err != nil {
			return err
		}
		n -= chunk
	}
	return nil
}
