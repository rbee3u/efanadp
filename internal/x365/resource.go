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

func closePipeReaderWithError(reader *io.PipeReader, err error) {
	if reader == nil {
		return
	}
	_ = reader.CloseWithError(err)
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
