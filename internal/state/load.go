package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	// Best-effort lock to reduce concurrent writers/readers races
	if unlock, lockErr := acquireStateLock(dir); lockErr == nil && unlock != nil {
		defer unlock()
	}
	latestPath := filepath.Join(dir, "latest.json")
	latestBytes, err := os.ReadFile(latestPath)
	if err != nil {
		// Missing latest.json is not a quarantinable corruption; caller can regenerate.
		return nil, ErrStateInvalid
	}

	var ptr latestPointer
	if err := json.Unmarshal(latestBytes, &ptr); err != nil {
		// Partially written or corrupt latest.json → quarantine pointer for regeneration.
		quarantineFile(latestPath)
		return nil, ErrStateInvalid
	}
	if ptr.Version != "1" || !isBaseName(ptr.Path) {
		// Unknown version or unsafe path → quarantine pointer.
		quarantineFile(latestPath)
		return nil, ErrStateInvalid
	}

	snapPath := filepath.Join(dir, ptr.Path)
	snapBytes, err := os.ReadFile(snapPath)
	if err != nil {
		// Pointer to missing or unreadable snapshot → quarantine pointer.
		quarantineFile(latestPath)
		return nil, ErrStateInvalid
	}

	if ptr.SHA256 != "" {
		sum := sha256.Sum256(snapBytes)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), ptr.SHA256) {
			// Snapshot contents do not match recorded hash → quarantine snapshot and pointer.
			quarantineFile(snapPath)
			quarantineFile(latestPath)
			return nil, ErrStateInvalid
		}
	}

	var b StateBundle
	if err := json.Unmarshal(snapBytes, &b); err != nil {
		// Corrupt snapshot JSON → quarantine snapshot and pointer.
		quarantineFile(snapPath)
		quarantineFile(latestPath)
		return nil, ErrStateInvalid
	}
	if err := b.Validate(); err != nil {
		// Invalid bundle structure → quarantine snapshot and pointer.
		quarantineFile(snapPath)
		quarantineFile(latestPath)
		return nil, ErrStateInvalid
	}
	return &b, nil
}

// quarantineFile renames the given path to a sibling with a ".quarantined" suffix.
// If the target exists, it appends a numeric counter (e.g., .quarantined.1) up to 99 attempts.
// Errors are returned only for unexpected conditions; callers typically ignore failures.
func quarantineFile(path string) {
	// Ensure we only operate within an existing directory and on a regular file when possible.
	base := filepath.Base(path)
	if base == "." || base == ".." || base == "" {
		return
	}
	dir := filepath.Dir(path)
	// Compute first candidate
	cand := filepath.Join(dir, base+".quarantined")
	if _, err := os.Stat(cand); err == nil {
		// Find an available suffix
		for i := 1; i < 100; i++ {
			next := filepath.Join(dir, fmt.Sprintf("%s.quarantined.%d", base, i))
			if _, err := os.Stat(next); os.IsNotExist(err) {
				cand = next
				break
			}
		}
	}
	// Attempt atomic rename; best-effort.
	if err := os.Rename(path, cand); err != nil {
		return
	}
}
