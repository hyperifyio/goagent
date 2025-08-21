package oai

import (
	"bytes"
	"io"
	"testing"
)

// Validates that newLineReader returns lines including trailing \n and signals EOF.
func TestNewLineReader_ReadsLinesAndEOF(t *testing.T) {
	src := bytes.NewBufferString("first\nsecond\n")
	next := newLineReader(src)

	line1, err := next()
	if err != nil {
		t.Fatalf("unexpected error on first read: %v", err)
	}
	if line1 != "first\n" {
		t.Fatalf("unexpected first line: %q", line1)
	}

	line2, err := next()
	if err != nil {
		t.Fatalf("unexpected error on second read: %v", err)
	}
	if line2 != "second\n" {
		t.Fatalf("unexpected second line: %q", line2)
	}

	// Third read should hit EOF
	if _, err := next(); err == nil || err != io.EOF {
		t.Fatalf("expected io.EOF on third read, got: %v", err)
	}
}
