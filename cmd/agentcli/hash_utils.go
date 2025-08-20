package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
)

// sha256SumHex returns the lowercase hex SHA-256 of b.
func sha256SumHex(b []byte) string {
	h := sha256.New()
	_, _ = h.Write(b)
	return fmt.Sprintf("%x", h.Sum(nil))
}

// computeToolsetHash returns a stable hash of the tools manifest contents.
// When manifestPath is empty or unreadable, returns an empty string.
func computeToolsetHash(manifestPath string) string {
	path := strings.TrimSpace(manifestPath)
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return sha256SumHex(b)
}

// computeDefaultStateScope returns sha256(model + "|" + base + "|" + toolsetHash).
func computeDefaultStateScope(model string, base string, toolsetHash string) string {
	input := []byte(strings.TrimSpace(model) + "|" + strings.TrimSpace(base) + "|" + strings.TrimSpace(toolsetHash))
	return sha256SumHex(input)
}
