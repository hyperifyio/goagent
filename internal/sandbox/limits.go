package sandbox

import (
	"bytes"
	"context"
	"errors"
	"time"
)

// ErrOutputLimit is returned when a bounded writer exceeds its configured cap.
var ErrOutputLimit = errors.New("OUTPUT_LIMIT")

// ErrTimeout is returned by helpers when execution exceeds the wall-time budget.
var ErrTimeout = errors.New("TIMEOUT")

// BoundedBuffer is an io.Writer implementation that caps total bytes written.
// When the cap is exceeded, it truncates additional input and returns ErrOutputLimit.
// Use Bytes() or String() to retrieve accumulated output and Truncated() to check status.
//
// Note: The writer never grows beyond the configured capacity in memory.
// A zero or negative maxKB defaults to 64 KiB.
type BoundedBuffer struct {
	buf       bytes.Buffer
	capBytes  int
	truncated bool
}

// NewBoundedBuffer creates a new BoundedBuffer with the provided maxKB capacity.
func NewBoundedBuffer(maxKB int) *BoundedBuffer {
	if maxKB <= 0 {
		maxKB = 64
	}
	return &BoundedBuffer{capBytes: maxKB * 1024}
}

// Write appends p to the buffer up to the capacity. If the write causes
// the capacity to be exceeded, the write is truncated and ErrOutputLimit is returned.
func (b *BoundedBuffer) Write(p []byte) (int, error) {
	if b.capBytes <= 0 {
		return 0, ErrOutputLimit
	}
	remaining := b.capBytes - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return 0, ErrOutputLimit
	}
	if len(p) > remaining {
		// Partial write up to remaining capacity
		_, _ = b.buf.Write(p[:remaining])
		b.truncated = true
		return remaining, ErrOutputLimit
	}
	return b.buf.Write(p)
}

// Bytes returns the current contents (may be truncated if cap exceeded).
func (b *BoundedBuffer) Bytes() []byte { return b.buf.Bytes() }

// String returns the current contents as string (may be truncated).
func (b *BoundedBuffer) String() string { return b.buf.String() }

// Truncated reports whether any write exceeded the cap.
func (b *BoundedBuffer) Truncated() bool { return b.truncated }

// WithWallTimeout returns a derived context that is canceled after wallMS milliseconds.
// If wallMS <= 0, a conservative default of 1000ms is used.
func WithWallTimeout(parent context.Context, wallMS int) (context.Context, context.CancelFunc) {
	if wallMS <= 0 {
		wallMS = 1000
	}
	return context.WithTimeout(parent, time.Duration(wallMS)*time.Millisecond)
}

// JSONError is a tiny helper to construct a standard error payload shape.
// Callers generally write this to stderr.
func JSONError(code, message string) []byte {
	// Minimal hand-rolled JSON to avoid allocations and error paths here.
	// code and message are expected to be short ASCII; if not, JSON remains valid but unescaped.
	return []byte(`{"code":"` + code + `","message":"` + message + `"}`)
}
