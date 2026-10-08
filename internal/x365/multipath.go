package x365

import (
	"crypto/rand"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

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

	req, err := buildX365Request(multipathControlHost, multipathControlPort, userID, x365CommandTCP)
	if err != nil {
		return nil, fmt.Errorf("build x365 multipath request: %w", err)
	}
	if err := writeFull(stream, req); err != nil {
		return nil, fmt.Errorf("write x365 multipath request: %w", err)
	}
	if err := readX365OKResponse(stream); err != nil {
		return nil, fmt.Errorf("read x365 multipath response: %w", err)
	}
	var streamID [16]byte
	if _, err := rand.Read(streamID[:]); err != nil {
		return nil, fmt.Errorf("create multipath stream ID: %w", err)
	}
	openFrame, err := buildMultipathOpenFrame(destHost, destPort, streamID)
	if err != nil {
		return nil, fmt.Errorf("build multipath hello: %w", err)
	}
	if err := writeFull(stream, openFrame); err != nil {
		return nil, fmt.Errorf("write multipath hello: %w", err)
	}
	opened = true
	return newMultipathConn(stream), nil
}

type multipathConn struct {
	stream    io.ReadWriteCloser
	readBuf   []byte
	writeMu   sync.Mutex
	writeSeq  uint64
	writeErr  error
	closeOnce sync.Once
	closeErr  error
	closed    atomic.Bool

	acks *ackQueue
}

var _ io.ReadWriteCloser = (*multipathConn)(nil)

const multipathCloseFrameTimeout = 100 * time.Millisecond

func newMultipathConn(stream io.ReadWriteCloser) *multipathConn {
	conn := &multipathConn{
		stream: stream,
		acks:   newACKQueue(),
	}
	go conn.acks.run(conn.flushACK)
	return conn
}

func (c *multipathConn) Read(p []byte) (int, error) {
	for len(c.readBuf) == 0 {
		header, err := readFrameHeader(c.stream)
		if err != nil {
			return 0, err
		}
		switch header.Type {
		case frameData:
			n, err := c.readDataFramePayload(header.Seq, header.PayloadLen, p)
			if err != nil || n > 0 {
				return n, err
			}
		case frameClose:
			if err := discardFull(c.stream, header.PayloadLen); err != nil {
				return 0, err
			}
			return 0, io.EOF
		default:
			if err := discardFull(c.stream, header.PayloadLen); err != nil {
				return 0, err
			}
		}
	}
	return c.readBuffered(p), nil
}

func (c *multipathConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	if c.writeErr != nil {
		return 0, c.writeErr
	}

	header, err := dataFrameHeader(c.writeSeq, len(p))
	if err != nil {
		return 0, err
	}
	if err := c.writeDataFrameLocked(header, p); err != nil {
		// Partial frame writes are terminal; retries would corrupt framing.
		c.writeErr = err
		return 0, err
	}
	c.writeSeq += framePayloadLenUint64(len(p))
	return len(p), nil
}

func (c *multipathConn) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		closeFrameDone := c.startCloseFrame()
		if closeFrameDone != nil {
			timer := time.NewTimer(multipathCloseFrameTimeout)
			select {
			case <-closeFrameDone:
				timer.Stop()
			case <-timer.C:
			}
		}
		// Close first to release flow-controlled writes before waiting for the
		// ACK sender.
		c.closeErr = c.stream.Close()
		if closeFrameDone != nil {
			<-closeFrameDone
		}
		c.acks.stop()
	})
	return c.closeErr
}

// Send close asynchronously: even with the write lock free, HTTP/2 flow control
// can stall the write.
func (c *multipathConn) startCloseFrame() <-chan struct{} {
	if !c.writeMu.TryLock() {
		return nil
	}
	if c.writeErr != nil {
		c.writeMu.Unlock()
		return nil
	}
	done := make(chan struct{})
	frame := closeFrameHeader(c.writeSeq)
	go func() {
		defer close(done)
		defer c.writeMu.Unlock()
		_ = writeFull(c.stream, frame[:])
	}()
	return done
}

func (c *multipathConn) writeDataFrameLocked(header [frameHeaderSize]byte, payload []byte) error {
	if err := writeFull(c.stream, header[:]); err != nil {
		return err
	}
	return writeFull(c.stream, payload)
}

func (c *multipathConn) readDataFramePayload(seq uint64, payloadLen int, dst []byte) (int, error) {
	directLen := min(payloadLen, len(dst))
	if _, err := io.ReadFull(c.stream, dst[:directLen]); err != nil {
		return 0, err
	}
	var remainder []byte
	if payloadLen > directLen {
		remainder = make([]byte, payloadLen-directLen)
		if _, err := io.ReadFull(c.stream, remainder); err != nil {
			return 0, err
		}
	}
	c.ackDataFrame(seq + framePayloadLenUint64(payloadLen))
	c.readBuf = remainder
	return directLen, nil
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
