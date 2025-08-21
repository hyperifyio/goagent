package main

import (
    "bytes"
    "errors"
    "testing"
)

type alwaysErrorWriter struct{}

func (alwaysErrorWriter) Write(p []byte) (int, error) { return 0, errors.New("write error") }

func TestSafeFprintlnAndFprintf(t *testing.T) {
    // Success path writes to buffer
    var buf bytes.Buffer
    safeFprintln(&buf, "hello", 123)
    safeFprintf(&buf, " %s %d", "world", 456)
    if got := buf.String(); got == "" {
        t.Fatalf("expected non-empty output, got %q", got)
    }
    // Error path: should not panic
    var ew alwaysErrorWriter
    safeFprintln(ew, "ignored")
    safeFprintf(ew, "ignored %d", 1)
}
