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

// latestPointer is the JSON structure written to latest.json to point to the
// concrete snapshot file. The path is the base name inside the state directory.
type latestPointer struct {
	Version string `json:"version"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
}

// syncDirFunc is a hook to fsync a directory after atomic renames.
// It is a var so tests can override and assert that directory fsync was used.
var syncDirFunc = func(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := d.Close(); cerr != nil {
			// best-effort: ignore close error for directory sync
			_ = cerr
		}
	}()
	return d.Sync()
}

// writeFileAtomic writes data to a temporary file next to dstPath with mode 0600,
// fsyncs the file, renames it atomically to dstPath, and fsyncs the directory.
func writeFileAtomic(dir string, dstPath string, data []byte) error {
	// Ensure directory exists with 0700 perms
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// Best-effort set exact perms (in case existing dir had broader perms)
	if err := os.Chmod(dir, 0o700); err != nil {
		// best-effort: directory may already have stricter perms
		_ = err
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Ensure 0600 permissions for the temp file
	if err := tmp.Chmod(0o600); err != nil {
		if cerr := tmp.Close(); cerr != nil {
			_ = cerr
		}
		if rerr := os.Remove(tmpName); rerr != nil {
			_ = rerr
		}
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		if cerr := tmp.Close(); cerr != nil {
			_ = cerr
		}
		if rerr := os.Remove(tmpName); rerr != nil {
			_ = rerr
		}
		return err
	}
	if err := tmp.Sync(); err != nil {
		if cerr := tmp.Close(); cerr != nil {
			_ = cerr
		}
		if rerr := os.Remove(tmpName); rerr != nil {
			_ = rerr
		}
		return err
	}
	if err := tmp.Close(); err != nil {
		if rerr := os.Remove(tmpName); rerr != nil {
			_ = rerr
		}
		return err
	}
	if err := os.Rename(tmpName, dstPath); err != nil {
		if rerr := os.Remove(tmpName); rerr != nil {
			_ = rerr
		}
		return err
	}
	if err := syncDirFunc(dir); err != nil {
		return err
	}
	return nil
}

// sanitizeRFC3339ForFilename makes the RFC3339 timestamp safe for cross-platform filenames
// by removing ':' characters. Example: 2006-01-02T15:04:05Z07:00 -> 2006-01-02T150405Z0700
func sanitizeRFC3339ForFilename(ts string) string {
	// Remove colons which are problematic on Windows filesystems
	return strings.ReplaceAll(ts, ":", "")
}

// SaveStateBundle persists the provided bundle into dir using an atomic write strategy.
// It writes a content-addressed snapshot file named
//
//	state-<RFC3339UTC>-<8charSHA>.json
//
// and then updates latest.json atomically to point to that snapshot. All files are
// written with 0600 permissions and the directory is fsync'ed after renames.
// The function does not mutate the given bundle; callers must ensure it is valid.
func SaveStateBundle(dir string, bundle *StateBundle) error {
	if bundle == nil {
		return errors.New("nil bundle")
	}
	if err := bundle.Validate(); err != nil {
		return fmt.Errorf("invalid bundle: %w", err)
	}

	// Security: reject insecure directories (world-writable or non-owned) on Unix
	if err := ensureSecureStateDir(dir); err != nil {
		return err
	}

	// Attempt coarse-grained advisory lock to avoid concurrent writes
	if unlock, lockErr := acquireStateLock(dir); lockErr == nil && unlock != nil {
		defer unlock()
	}

	// Redact/sanitize secrets before persisting
	sanitized, err := sanitizeBundleForSave(bundle)
	if err != nil {
		return err
	}

	// Marshal the snapshot deterministically.
	// Note: json.Marshal is sufficient; map key ordering is not relied upon for correctness here.
	snapshotBytes, err := json.MarshalIndent(sanitized, "", "  ")
	if err != nil {
		return err
	}

	// Compute content hash for integrity and short suffix
	sum := sha256.Sum256(snapshotBytes)
	shaHex := hex.EncodeToString(sum[:])
	short8 := shaHex[:8]

	baseName := fmt.Sprintf("state-%s-%s.json", sanitizeRFC3339ForFilename(bundle.CreatedAt), short8)
	finalPath := filepath.Join(dir, baseName)

	if err := writeFileAtomic(dir, finalPath, snapshotBytes); err != nil {
		return err
	}

	// Write pointer file
	ptr := latestPointer{Version: "1", Path: baseName, SHA256: shaHex}
	ptrBytes, err := json.MarshalIndent(ptr, "", "  ")
	if err != nil {
		return err
	}
	latestPath := filepath.Join(dir, "latest.json")
	if err := writeFileAtomic(dir, latestPath, ptrBytes); err != nil {
		return err
	}
	return nil
}
