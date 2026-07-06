package x365

import (
	"fmt"
	"io"
)

func readX365OKResponse(r io.Reader) error {
	var resp [5]byte
	if _, err := io.ReadFull(r, resp[:]); err != nil {
		return err
	}
	if string(resp[:4]) != "X365" || resp[4] != 0 {
		return fmt.Errorf("bad X365 response: %x", resp)
	}
	return nil
}
