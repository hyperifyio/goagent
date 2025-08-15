package main

// https://github.com/hyperifyio/goagent/issues/1

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type fsListdirEntry struct {
	Path      string `json:"path"`
	Type      string `json:"type"`
	SizeBytes int64  `json:"sizeBytes"`
	ModeOctal string `json:"modeOctal"`
	ModTime   string `json:"modTime"`
}

type fsListdirOutput struct {
	Entries   []fsListdirEntry `json:"entries"`
	Truncated bool             `json:"truncated"`
}

// buildFsListdir builds ./tools/fs_listdir.go into a temporary binary.
func buildFsListdir(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "fs-listdir")
    cmd := exec.Command("go", "build", "-o", binPath, "./fs_listdir")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build fs_listdir tool: %v\n%s", err, string(out))
	}
	return binPath
}

// runFsListdir executes the fs_listdir tool with given JSON input.
func runFsListdir(t *testing.T, bin string, input any) (fsListdirOutput, string, int) {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	cmd := exec.Command(bin)
	cmd.Dir = "."
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	var out fsListdirOutput
	_ = json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &out)
	return out, stderr.String(), code
}

// TestFsListdir_EmptyDir_NonRecursive verifies empty directory returns no entries and not truncated.
func TestFsListdir_EmptyDir_NonRecursive(t *testing.T) {
	// Arrange: create a repo-relative empty temp dir
	tmpDirAbs, err := os.MkdirTemp(".", "fslistdir-empty-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDirAbs) })
	base := filepath.Base(tmpDirAbs)

	bin := buildFsListdir(t)

	// Act: list with recursive=false, includeHidden=false
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
	if out.Truncated {
		t.Fatalf("should not be truncated for empty dir")
	}
	if len(out.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d: %+v", len(out.Entries), out.Entries)
	}
}
