// Package datagram defines the message-oriented I/O contract shared by the
// SOCKS relay and its X365 upstream. It contains no transport implementation.
package datagram

// Conn carries one datagram per call, including zero-length datagrams.
// ReadDatagram consumes a whole datagram: if p is too small, the remainder is
// discarded. Callers must provide space for the upstream maximum payload.
// WriteDatagram never splits or combines datagrams. A write error may leave the
// transport unusable; callers close it rather than retry a partial datagram.
// One reader may run alongside writers. Writes are serialized by the connection.
// Close is safe concurrently with I/O, is idempotent, and unblocks pending I/O.
type Conn interface {
	ReadDatagram(p []byte) (int, error)
	WriteDatagram(p []byte) (int, error)
	Close() error
}
