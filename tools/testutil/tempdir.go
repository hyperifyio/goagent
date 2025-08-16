package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// MakeRepoRelTempDir creates a temporary directory under the current
// package working directory and returns its relative path (basename).
// The directory is removed at test cleanup.
func MakeRepoRelTempDir(t *testing.T, prefix string) string {
	t.Helper()
	tmpAbs, err := os.MkdirTemp(".", prefix)
	if err != nil {
		t.Fatalf("mkdir temp under repo: %v", err)
	}
	base := filepath.Base(tmpAbs)
	t.Cleanup(func() {
		if err := os.RemoveAll(base); err != nil {
			t.Logf("cleanup remove %s: %v", base, err)
		}
	})
	return base
}
