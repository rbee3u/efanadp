package x365

import "sync"

// ackQueue holds the newest multipath ACK a reader has produced but not yet
// written, so acknowledging an inbound data frame never has to wait for the
// frame write lock.
//
// Waiting there wedges the connection. A data frame write parks in the upstream
// flow-control window for as long as the destination is slow to drain, and it
// holds the write lock for that whole time. An ACK issued from Read would queue
// behind it, so Read would stop draining the response body; the server would
// then stop being able to send, which is what it needs to do before it can drain
// the upload the write is waiting on. Neither direction can move again.
//
// Multipath ACKs are cumulative, so coalescing loses nothing: only the newest
// sequence carries information, and one slot is enough to hold it.
type ackQueue struct {
	// signal is a coalescing wakeup with capacity one. It tells the sender that
	// a sequence is waiting, never which one.
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

// offer records seq as the sequence to acknowledge next. It never blocks.
func (q *ackQueue) offer(seq uint64) {
	q.mu.Lock()
	// Sequences only ever advance, so keeping the larger value means a slow
	// offer can never rewind an acknowledgement that already went out.
	if !q.pending || seq > q.seq {
		q.seq = seq
		q.pending = true
	}
	q.mu.Unlock()
}

// take removes the queued sequence, reporting false when nothing is waiting.
func (q *ackQueue) take() (uint64, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	seq, pending := q.seq, q.pending
	q.pending = false
	return seq, pending
}

// wake asks the sender goroutine to write whatever is queued.
func (q *ackQueue) wake() {
	select {
	case q.signal <- struct{}{}:
	default:
	}
}

// stop ends the sender goroutine and waits for it to return. Callers close the
// stream first, so a send already parked on the write lock fails and unwinds
// instead of holding stop open.
func (q *ackQueue) stop() {
	q.quitOnce.Do(func() {
		close(q.quit)
	})
	<-q.done
}

// run writes queued ACKs until the queue is stopped or the stream fails. flush
// is called with the frame write lock held by run.
func (q *ackQueue) run(flush func() error) {
	defer close(q.done)
	for {
		select {
		case <-q.signal:
		case <-q.quit:
			return
		}
		if err := flush(); err != nil {
			// The stream is gone, so no later ACK can be written either.
			return
		}
	}
}
