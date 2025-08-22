package image

import (
	"testing"
	"time"
)

func TestNewClient_AppliesTimeoutAndRetry(t *testing.T) {
	c := NewClient("https://example", "key", 3*time.Second, 5, 750*time.Millisecond)
	if got := c.HTTPTimeout(); got != 3*time.Second {
		t.Fatalf("HTTPTimeout=%s; want 3s", got)
	}
	r := c.Retry()
	if r.MaxRetries != 5 {
		t.Fatalf("MaxRetries=%d; want 5", r.MaxRetries)
	}
	if r.Backoff != 750*time.Millisecond {
		t.Fatalf("Backoff=%s; want 750ms", r.Backoff)
	}
}

func TestNewClient_NormalizesInputs(t *testing.T) {
	c := NewClient("https://example", "key", 0, -1, 0)
	if got := c.HTTPTimeout(); got <= 0 {
		t.Fatalf("HTTPTimeout=%s; want > 0 default", got)
	}
	r := c.Retry()
	if r.MaxRetries != 0 {
		t.Fatalf("MaxRetries=%d; want 0", r.MaxRetries)
	}
}
