package main

// https://github.com/hyperifyio/goagent/issues/1

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hyperifyio/goagent/tools/testutil"
)

// TestFsReadLines_MaxBytes_Truncates verifies output is truncated at maxBytes boundary
// without claiming EOF when the file likely continues.
func TestFsReadLines_MaxBytes_Truncates(t *testing.T) {
	bin := testutil.BuildTool(t, "fs_read_lines")

	// Arrange: create a repo-relative temp file with simple ASCII lines
	tmpDirAbs, err := os.MkdirTemp(".", "fsread-maxbytes-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(tmpDirAbs); err != nil {
			t.Logf("cleanup remove %s: %v", tmpDirAbs, err)
		}
	})
	base := filepath.Base(tmpDirAbs)
	fileRel := filepath.Join(base, "data.txt")
	content := "aa\nbb\ncc\n" // total 9 bytes
	if err := os.WriteFile(fileRel, []byte(content), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	// Act: request [1,3) but cap output to 4 bytes
	out, stderr, code := runFsReadLines(t, bin, map[string]any{
		"path":      fileRel,
		"startLine": 1,
		"endLine":   3,
		"maxBytes":  4,
	})

	// Assert: success, content truncated to exactly 4 bytes, EOF=false
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if len(out.Content) != 4 {
		t.Fatalf("content length mismatch: got %d want %d", len(out.Content), 4)
	}
	if out.Content != "aa\nb" { // would be "aa\nbb\n" without truncation
		t.Fatalf("content mismatch: got %q want %q", out.Content, "aa\nb")
	}
	if out.EOF {
		t.Fatalf("unexpected EOF=true when truncated by maxBytes")
	}
	if out.StartLine != 1 || out.EndLine != 3 {
		t.Fatalf("range echoed mismatch: got (%d,%d) want (1,3)", out.StartLine, out.EndLine)
	}
}
