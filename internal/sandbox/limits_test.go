package sandbox

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBoundedBuffer_TruncatesAndSignals(t *testing.T) {
	buf := NewBoundedBuffer(1) // 1 KiB
	payload := strings.Repeat("A", 1536)
	n, err := buf.Write([]byte(payload))
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if err != ErrOutputLimit {
		t.Fatalf("expected ErrOutputLimit, got %v", err)
	}
	if n != 1024 {
		t.Fatalf("expected partial write of 1024, got %d", n)
	}
	if !buf.Truncated() {
		t.Fatalf("expected truncated=true")
	}
	if len(buf.Bytes()) != 1024 {
		t.Fatalf("expected buffer length 1024, got %d", len(buf.Bytes()))
	}
}

func TestBoundedBuffer_FitsWithinCap(t *testing.T) {
	buf := NewBoundedBuffer(2) // 2 KiB
	payload := strings.Repeat("B", 1500)
	n, err := buf.Write([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1500 {
		t.Fatalf("expected full write of 1500, got %d", n)
	}
	if buf.Truncated() {
		t.Fatalf("did not expect truncation")
	}
	if len(buf.Bytes()) != 1500 {
		t.Fatalf("expected buffer length 1500, got %d", len(buf.Bytes()))
	}
}

func TestWithWallTimeout_TimesOutRoughlyOnBudget(t *testing.T) {
	ctx, cancel := WithWallTimeout(context.Background(), 50)
	defer cancel()

	start := time.Now()
	<-ctx.Done()
	elapsed := time.Since(start)
	if elapsed < 40*time.Millisecond || elapsed > 250*time.Millisecond {
		t.Fatalf("expected ~50ms timeout, got %v", elapsed)
	}
}
