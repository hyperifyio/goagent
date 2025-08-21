package oai

import (
	"strings"
	"testing"
)

func TestGenerateIdempotencyKey_PrefixAndUniqueness(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		k := generateIdempotencyKey()
		if !strings.HasPrefix(k, "goagent-") {
			t.Fatalf("missing prefix: %s", k)
		}
		if _, ok := seen[k]; ok {
			t.Fatalf("duplicate key generated: %s", k)
		}
		seen[k] = struct{}{}
	}
}
