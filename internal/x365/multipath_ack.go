package x365

import (
	"io"
	"sync"
)

// ackQueue coalesces cumulative ACKs without waiting for the frame write lock.
// Waiting behind a flow-controlled upload would stop Read from draining
// downloads and can deadlock both directions.
type ackQueue struct {
	// signal is a coalesced wakeup; seq holds the acknowledgement.
	signal chan struct{}
	quit   chan struct{}
	done   chan struct{}

	quitOnce sync.Once

	mu      sync.Mutex
	seq     uint64
	pending bool
}

func newACKQueue() *ackQueue {
	return &ackQueue{
		signal: make(chan struct{}, 1),
		quit:   make(chan struct{}),
		done:   make(chan struct{}),
	}
}

func (q *ackQueue) offer(seq uint64) {
	q.mu.Lock()
	if !q.pending || seq > q.seq {
		q.seq = seq
		q.pending = true
	}
	q.mu.Unlock()
}

func (q *ackQueue) take() (uint64, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	seq, pending := q.seq, q.pending
	q.pending = false
	return seq, pending
}

func (q *ackQueue) wake() {
	select {
	case q.signal <- struct{}{}:
	default:
	}
}

// Close the stream before stop so a blocked writer releases the sender
// goroutine.
func (q *ackQueue) stop() {
	q.quitOnce.Do(func() {
		close(q.quit)
	})
	<-q.done
}

// flush acquires the frame write lock; a failed write ends the sender.
func (q *ackQueue) run(flush func() error) {
	defer close(q.done)
	for {
		select {
		case <-q.signal:
		case <-q.quit:
			return
		}
		if err := flush(); err != nil {
			return
		}
	}
}

func (c *multipathConn) ackDataFrame(seq uint64) {
	c.acks.offer(seq)
	if !c.writeMu.TryLock() {
		c.acks.wake()
		return
	}
	// The next relay read/write reports failure; failed ACKs cannot be retried.
	_ = c.flushACKLocked()
	c.writeMu.Unlock()
}

func (c *multipathConn) flushACK() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.flushACKLocked()
}

// Taking and writing under one lock prevents flushers from reordering ACKs.
func (c *multipathConn) flushACKLocked() error {
	if c.closed.Load() {
		return io.ErrClosedPipe
	}
	if c.writeErr != nil {
		return c.writeErr
	}
	seq, ok := c.acks.take()
	if !ok {
		return nil
	}
	ack := ackFrameHeader(seq)
	if err := writeFull(c.stream, ack[:]); err != nil {
		c.writeErr = err
		return err
	}
	return nil
}
