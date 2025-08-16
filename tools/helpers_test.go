package main

import (
	"os"
	"path/filepath"
	"testing"
)

// makeRepoRelTempDir creates a temporary directory under the repository root
// (current working directory in tests) and returns the relative path.
func makeRepoRelTempDir(t *testing.T, prefix string) string {
	t.Helper()
	tmpAbs, err := os.MkdirTemp(".", prefix)
	if err != nil {
		t.Fatalf("mkdir temp under repo: %v", err)
	}
	base := filepath.Base(tmpAbs)
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	return base
}
