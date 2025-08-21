package oai

import (
    "strings"
    "testing"
)

func TestGenerateIdempotencyKey_FormatAndUniqueness(t *testing.T) {
    k1 := generateIdempotencyKey()
    k2 := generateIdempotencyKey()
    if !strings.HasPrefix(k1, "goagent-") || !strings.HasPrefix(k2, "goagent-") {
        t.Fatalf("missing prefix: %q %q", k1, k2)
    }
    if k1 == k2 {
        t.Fatalf("expected unique keys, got identical: %q", k1)
    }
    // Ensure hex-ish suffix length (16 bytes -> 32 hex chars)
    if len(k1) < len("goagent-")+32 {
        t.Fatalf("unexpected key length: %d for %q", len(k1), k1)
    }
}
