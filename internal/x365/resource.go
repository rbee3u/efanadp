package x365

import (
	"io"
)

func closeQuietly(closer io.Closer) {
	if closer == nil {
		return
	}
	_ = closer.Close()
}

func writeFull(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
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
