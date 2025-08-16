package main

import (
	"os"
	"path/filepath"
	"testing"

	testutil "github.com/hyperifyio/goagent/tools/testutil"
)

// TestFsListdir_Globs_NonRecursive filters by globs when non-recursive.
func TestFsListdir_Globs_NonRecursive(t *testing.T) {
	// Arrange
	tmpDirAbs, err := os.MkdirTemp(".", "fslistdir-glob-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDirAbs) })
	base := filepath.Base(tmpDirAbs)

	if err := os.Mkdir(filepath.Join(tmpDirAbs, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDirAbs, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDirAbs, "b.md"), []byte("y"), 0o644); err != nil {
		t.Fatalf("write b.md: %v", err)
	}

	bin := testutil.BuildTool(t, "fs_listdir")

	// Act: globs should only include *.txt at top level in non-recursive mode
	out, stderr, code := runFsListdir(t, bin, map[string]any{
		"path":          base,
		"recursive":     false,
		"includeHidden": false,
		"globs":         []string{"**/*.txt"},
		"maxResults":    100,
	})

	// Assert
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if out.Truncated {
		t.Fatalf("should not be truncated")
	}
	if len(out.Entries) != 1 {
		t.Fatalf("expected 1 entry (*.txt), got %d: %+v", len(out.Entries), out.Entries)
	}
	if filepath.Base(out.Entries[0].Path) != "a.txt" || out.Entries[0].Type != "file" {
		t.Fatalf("expected a.txt file, got: %+v", out.Entries[0])
	}
}
