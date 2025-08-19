package state

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// acquireStateLock attempts to take a coarse-grained advisory lock for the
// given directory by creating a file named "state.lock" with O_EXCL.
// If the lock is already held, it waits up to ~2s with jitter and retries.
// On success it returns an unlock function that removes the lock file.
// If the lock cannot be acquired within the wait budget, it returns a no-op
// unlock function and nil error so callers can proceed without crashing.
func acquireStateLock(dir string) (func(), error) {
	lockPath := filepath.Join(dir, "state.lock")

	// Ensure directory exists so we can create the lock file.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return func() {}, err
	}
	// Best-effort set exact perms (in case existing dir had broader perms).
	_ = os.Chmod(dir, 0o700)

	tryOnce := func() (bool, error) {
		// Include a small token in the file for debugging; best-effort.
		var token [8]byte
		_, _ = rand.Read(token[:])
		contents := []byte(fmt.Sprintf("ts=%s token=%s\n", time.Now().UTC().Format(time.RFC3339Nano), hex.EncodeToString(token[:])))
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			if os.IsExist(err) {
				return false, nil
			}
			return false, err
		}
		_, _ = f.Write(contents)
		_ = f.Sync()
		_ = f.Close()
		return true, nil
	}

	// Immediate attempt
	ok, err := tryOnce()
	if err != nil {
		return func() {}, err
	}
	if ok {
		return func() { _ = os.Remove(lockPath) }, nil
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		// Sleep 50-150ms jitter
		sleep := 50 + int(time.Now().UnixNano()%100)
		time.Sleep(time.Duration(sleep) * time.Millisecond)
		ok, err := tryOnce()
		if err != nil {
			return func() {}, err
		}
		if ok {
			return func() { _ = os.Remove(lockPath) }, nil
		}
	}
	// Failed to acquire; proceed without lock
	return func() {}, nil
}
