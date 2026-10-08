package x365

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	"github.com/rbee3u/efanadp/internal/datagram"
)

const x365UDPMaxPayload = 0xffff

func openUDP(stream io.ReadWriteCloser, userID UserID, destHost string, destPort int) (datagram.Conn, error) {
	opened := false
	defer func() {
		if !opened {
			closeQuietly(stream)
		}
	}()

	req, err := buildX365Request(destHost, destPort, userID, x365CommandUDP)
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
	stream   io.ReadWriteCloser
	writeMu  sync.Mutex
	writeErr error

	responseOnce sync.Once
	responseErr  error
	closeOnce    sync.Once
	closeErr     error
}

var _ datagram.Conn = (*x365UDPConn)(nil)

func (c *x365UDPConn) ReadDatagram(p []byte) (int, error) {
	c.responseOnce.Do(func() {
		c.responseErr = readX365OKResponse(c.stream)
	})
	if c.responseErr != nil {
		return 0, c.responseErr
	}
	return readX365UDPDatagram(c.stream, p)
}

func (c *x365UDPConn) WriteDatagram(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	if len(p) > x365UDPMaxPayload {
		return 0, fmt.Errorf("X365 UDP payload too large: %d", len(p))
	}

	n, err := writeX365UDPDatagram(c.stream, p)
	if err != nil {
		// Once any part of a length-prefixed datagram is written, the stream
		// cannot be resynchronized. Preserve the first error and reject retries.
		c.writeErr = err
	}
	return n, err
}

func (c *x365UDPConn) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.stream.Close()
	})
	return c.closeErr
}

// readX365UDPDatagram discards any payload beyond len(p) to preserve framing.
// Callers need x365UDPMaxPayload bytes to avoid silent truncation.
func readX365UDPDatagram(r io.Reader, p []byte) (int, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, err
	}
	payloadLen := int(binary.BigEndian.Uint16(header[:]))
	n := min(payloadLen, len(p))
	if _, err := io.ReadFull(r, p[:n]); err != nil {
		return 0, err
	}
	if err := discardFull(r, payloadLen-n); err != nil {
		return n, err
	}
	return n, nil
}

func writeX365UDPDatagram(w io.Writer, p []byte) (int, error) {
	payloadLen := len(p)
	if payloadLen > x365UDPMaxPayload {
		return 0, fmt.Errorf("X365 UDP payload too large: %d", payloadLen)
	}
	var header [2]byte
	binary.BigEndian.PutUint16(header[:], uint16(payloadLen))
	if err := writeFull(w, header[:]); err != nil {
		return 0, err
	}
	if err := writeFull(w, p); err != nil {
		return 0, err
	}
	return len(p), nil
}
