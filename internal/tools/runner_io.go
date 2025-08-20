package tools

import (
	"io"
)

// safeReadAll reads all bytes from r; on error it returns any bytes read so far (or nil).
func safeReadAll(r io.Reader) []byte {
	b, err := io.ReadAll(r)
	if err != nil {
		return b
	}
	return b
}
