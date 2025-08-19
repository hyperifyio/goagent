package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrStateInvalid is returned when persisted state cannot be loaded safely.
var ErrStateInvalid = errors.New("state invalid")

// isBaseName returns true if p is a simple base name with no path separators
// or parent traversal segments.
func isBaseName(p string) bool {
	if p == "" {
		return false
	}
	if filepath.Base(p) != p {
		return false
	}
	if strings.Contains(p, "..") {
		return false
	}
	return true
}

// LoadLatestStateBundle loads the most recent state bundle from dir by reading
// latest.json and then opening the referenced snapshot file. It validates the
// pointer structure, verifies the snapshot hash when present, and validates the
// bundle schema. On any issue (missing files, decode errors, version mismatch,
// permission errors), it returns (nil, ErrStateInvalid).
func LoadLatestStateBundle(dir string) (*StateBundle, error) {
    // Security: reject insecure directories (world-writable or non-owned) on Unix
    if err := ensureSecureStateDir(dir); err != nil {
        return nil, ErrStateInvalid
    }
	latestPath := filepath.Join(dir, "latest.json")
	latestBytes, err := os.ReadFile(latestPath)
	if err != nil {
		return nil, ErrStateInvalid
	}

	var ptr latestPointer
	if err := json.Unmarshal(latestBytes, &ptr); err != nil {
		return nil, ErrStateInvalid
	}
	if ptr.Version != "1" || !isBaseName(ptr.Path) {
		return nil, ErrStateInvalid
	}

	snapPath := filepath.Join(dir, ptr.Path)
	snapBytes, err := os.ReadFile(snapPath)
	if err != nil {
		return nil, ErrStateInvalid
	}

	if ptr.SHA256 != "" {
		sum := sha256.Sum256(snapBytes)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), ptr.SHA256) {
			return nil, ErrStateInvalid
		}
	}

	var b StateBundle
	if err := json.Unmarshal(snapBytes, &b); err != nil {
		return nil, ErrStateInvalid
	}
	if err := b.Validate(); err != nil {
		return nil, ErrStateInvalid
	}
	return &b, nil
}
