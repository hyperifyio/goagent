package oai

import (
    "crypto/rand"
    "encoding/hex"
    "fmt"
    "time"
)

// generateIdempotencyKey returns a random hex string suitable for Idempotency-Key.
func generateIdempotencyKey() string {
    var b [16]byte
    if _, err := rand.Read(b[:]); err != nil {
        // Fallback to timestamp-based key if crypto/rand fails; extremely unlikely
        return fmt.Sprintf("goagent-%d", time.Now().UnixNano())
    }
    return "goagent-" + hex.EncodeToString(b[:])
}
