package x365

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

func openX365Multipath(
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
	conn, err := openMultipath(stream, userID, destHost, destPort)
	if err != nil {
		return nil, fmt.Errorf("open multipath stream: %w", err)
	}
	return conn, nil
}

func openMultipath(
	stream io.ReadWriteCloser,
	userID UserID,
	destHost string,
	destPort int,
) (io.ReadWriteCloser, error) {
	opened := false
	defer func() {
		if !opened {
			closeQuietly(stream)
		}
	}()

	req, err := buildX365Request(multipathControlHost, multipathControlPort, userID)
	if err != nil {
		return nil, fmt.Errorf("build x365 multipath request: %w", err)
	}
	if err := writeFull(stream, req); err != nil {
		return nil, fmt.Errorf("write x365 multipath request: %w", err)
	}
	if err := readX365OKResponse(stream); err != nil {
		return nil, fmt.Errorf("read x365 multipath response: %w", err)
	}
	framer, err := newMultipathFramer()
	if err != nil {
		return nil, fmt.Errorf("create multipath framer: %w", err)
	}
	openFrame, err := framer.openFrame(destHost, destPort)
	if err != nil {
		return nil, fmt.Errorf("build multipath hello: %w", err)
	}
	if err := writeFull(stream, openFrame); err != nil {
		return nil, fmt.Errorf("write multipath hello: %w", err)
	}
	opened = true
	return &multipathConn{stream: stream, framer: framer}, nil
}

type multipathConn struct {
	stream    io.ReadWriteCloser
	framer    *multipathFramer
	readBuf   []byte
	writeMu   sync.Mutex
	closeOnce sync.Once
	closeErr  error
}

var _ io.ReadWriteCloser = (*multipathConn)(nil)

func (c *multipathConn) Read(p []byte) (int, error) {
	for len(c.readBuf) == 0 {
		frame, payloadLen, err := readFrameHeader(c.stream)
		if err != nil {
			return 0, err
		}
		switch frame.Type {
		case frameData:
			n, err := c.readDataFramePayload(frame.Seq, payloadLen, p)
			if err != nil || n > 0 {
				return n, err
			}
		case frameClose:
			if err := discardFull(c.stream, payloadLen); err != nil {
				return 0, err
			}
			return 0, io.EOF
		case frameACK, frameHello:
			if err := discardFull(c.stream, payloadLen); err != nil {
				return 0, err
			}
			continue
		default:
			if err := discardFull(c.stream, payloadLen); err != nil {
				return 0, err
			}
			continue
		}
	}
	return c.readBuffered(p), nil
}

func (c *multipathConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	header, err := c.framer.dataFrameHeader(len(p))
	if err != nil {
		return 0, err
	}
	if err := c.writeDataFrameLocked(header, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *multipathConn) Close() error {
	c.closeOnce.Do(func() {
		if c.writeMu.TryLock() {
			frame := c.framer.closeFrameHeader()
			_ = c.writeFrameLocked(frame[:])
			c.writeMu.Unlock()
			c.closeErr = c.stream.Close()
			return
		}
		c.closeErr = c.stream.Close()
	})
	return c.closeErr
}

func (c *multipathConn) writeFrame(frame []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeFrameLocked(frame)
}

func (c *multipathConn) writeFrameLocked(frame []byte) error {
	return writeFull(c.stream, frame)
}

func (c *multipathConn) writeDataFrameLocked(header [frameHeaderSize]byte, payload []byte) error {
	if err := writeFull(c.stream, header[:]); err != nil {
		return err
	}
	return writeFull(c.stream, payload)
}

func (c *multipathConn) readDataFramePayload(seq uint64, payloadLen int, dst []byte) (int, error) {
	if payloadLen == 0 {
		c.ackDataFrame(seq)
		return 0, nil
	}
	if payloadLen <= len(dst) {
		if _, err := io.ReadFull(c.stream, dst[:payloadLen]); err != nil {
			return 0, err
		}
		c.ackDataFrame(seq + framePayloadLenUint64(payloadLen))
		return payloadLen, nil
	}

	directLen := len(dst)
	if directLen > 0 {
		if _, err := io.ReadFull(c.stream, dst); err != nil {
			return 0, err
		}
	}
	remainder := make([]byte, payloadLen-directLen)
	if _, err := io.ReadFull(c.stream, remainder); err != nil {
		return 0, err
	}
	c.ackDataFrame(seq + framePayloadLenUint64(payloadLen))
	c.readBuf = remainder
	return directLen, nil
}

func (c *multipathConn) ackDataFrame(seq uint64) {
	ack := ackFrameHeader(seq)
	_ = c.writeFrame(ack[:])
}

func (c *multipathConn) readBuffered(p []byte) int {
	n := copy(p, c.readBuf)
	if n == len(c.readBuf) {
		c.readBuf = nil
	} else {
		c.readBuf = c.readBuf[n:]
	}
	return n
}

func readFrameHeader(r io.Reader) (frame, int, error) {
	var header [frameHeaderSize]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return frame{}, 0, err
	}
	result, payloadLen := parseFrameHeader(header[:])
	return result, payloadLen, nil
}
