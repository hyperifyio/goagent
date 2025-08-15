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

// buildFsListdir builds ./tools/fs_listdir into a temporary binary.
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

// TestFsListdir_FilesDirsOrder_HiddenFiltering verifies non-recursive listing orders
// directories before files lexicographically and excludes hidden entries when includeHidden=false.
func TestFsListdir_FilesDirsOrder_HiddenFiltering(t *testing.T) {
    // Arrange: create a repo-relative temp dir with a dir, a file, and hidden entries
    tmpDirAbs, err := os.MkdirTemp(".", "fslistdir-mix-")
    if err != nil {
        t.Fatalf("mkdir temp: %v", err)
    }
    t.Cleanup(func() { _ = os.RemoveAll(tmpDirAbs) })
    base := filepath.Base(tmpDirAbs)

    // Visible directory and file
    if err := os.Mkdir(filepath.Join(tmpDirAbs, "z_dir"), 0o755); err != nil {
        t.Fatalf("mkdir child dir: %v", err)
    }
    if err := os.WriteFile(filepath.Join(tmpDirAbs, "a.txt"), []byte("hi"), 0o644); err != nil {
        t.Fatalf("write file: %v", err)
    }
    // Hidden directory and file
    if err := os.Mkdir(filepath.Join(tmpDirAbs, ".hdir"), 0o755); err != nil {
        t.Fatalf("mkdir hidden dir: %v", err)
    }
    if err := os.WriteFile(filepath.Join(tmpDirAbs, ".secret"), []byte("x"), 0o644); err != nil {
        t.Fatalf("write hidden file: %v", err)
    }

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
        t.Fatalf("should not be truncated for small dir")
    }
    if len(out.Entries) != 2 {
        t.Fatalf("expected 2 entries (dir+file), got %d: %+v", len(out.Entries), out.Entries)
    }
    // Expect directory first regardless of name
    if out.Entries[0].Type != "dir" || !strings.HasSuffix(out.Entries[0].Path, "/z_dir") {
        t.Fatalf("expected first entry to be dir z_dir, got: %+v", out.Entries[0])
    }
    if out.Entries[1].Type != "file" || !strings.HasSuffix(out.Entries[1].Path, "/a.txt") {
        t.Fatalf("expected second entry to be file a.txt, got: %+v", out.Entries[1])
    }
}

func TestFsListdir_ErrorJSON_PathRequired(t *testing.T) {
    bin := buildFsListdir(t)
    // Provide empty JSON to trigger validation error: path is required
    cmd := exec.Command(bin)
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr
    cmd.Stdin = bytes.NewBufferString("{}")
    err := cmd.Run()
    if err == nil {
        t.Fatalf("expected non-zero exit for missing path; stderr=%q", stderr.String())
    }
    // Stderr must be single-line JSON: {"error":"..."}
    var payload map[string]any
    if jerr := json.Unmarshal(bytes.TrimSpace(stderr.Bytes()), &payload); jerr != nil {
        t.Fatalf("stderr is not valid JSON: %v; got %q", jerr, stderr.String())
    }
    if _, ok := payload["error"]; !ok {
        t.Fatalf("stderr JSON missing 'error' field: %v", payload)
    }
}
