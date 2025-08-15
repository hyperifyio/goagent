package main

import (
    "os"
    "path/filepath"
    "testing"

    testutil "github.com/hyperifyio/goagent/tools/testutil"
)

// TestFsListdir_Symlink_EntryPresent verifies non-recursive listing includes symlink entries
// with type="symlink" and respects includeHidden=false.
func TestFsListdir_Symlink_EntryPresent(t *testing.T) {
	// Arrange: create a repo-relative temp dir with a file and a symlink
	tmpDirAbs, err := os.MkdirTemp(".", "fslistdir-symlink-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDirAbs) })
	base := filepath.Base(tmpDirAbs)

	// Create target file and symlink next to it
	target := filepath.Join(tmpDirAbs, "target.txt")
	if err := os.WriteFile(target, []byte("data"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	symlinkPath := filepath.Join(tmpDirAbs, "link.txt")
	if err := os.Symlink("target.txt", symlinkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

    bin := testutil.BuildTool(t, "fs_listdir")

	// Act: list with recursive=false
	out, stderr, code := runFsListdir(t, bin, map[string]any{
		"path":          base,
		"recursive":     false,
		"includeHidden": false,
		"maxResults":    100,
	})

	// Assert
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	foundFile := false
	foundLink := false
	for _, e := range out.Entries {
		if filepath.Base(e.Path) == "target.txt" && e.Type == "file" {
			foundFile = true
		}
		if filepath.Base(e.Path) == "link.txt" && e.Type == "symlink" {
			foundLink = true
		}
	}
	if !foundFile || !foundLink {
		t.Fatalf("expected both file and symlink entries, got: %+v", out.Entries)
	}
}
