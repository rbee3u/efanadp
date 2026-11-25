package x365

import (
	"fmt"
	"io"
)

// openTCP opens a direct X365 stream to the real destination. Since 1.0.71 the
// request header names that destination and payload is unframed, so the only
// handshake is the standard five-byte acknowledgement.
func openTCP(
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

	req, err := buildX365Request(destHost, destPort, userID, x365CommandTCP)
	if err != nil {
		return nil, fmt.Errorf("build x365 request: %w", err)
	}
	if err := writeFull(stream, req); err != nil {
		return nil, fmt.Errorf("write x365 request: %w", err)
	}
	if err := readX365OKResponse(stream); err != nil {
		return nil, fmt.Errorf("read x365 response: %w", err)
	}
	opened = true
	return stream, nil
}
