package main

// https://github.com/hyperifyio/goagent/issues/1

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/hyperifyio/goagent/tools/testutil"
)

type fsStatOutput struct {
	Exists    bool   `json:"exists"`
	Type      string `json:"type"`
	SizeBytes int64  `json:"sizeBytes"`
	ModeOctal string `json:"modeOctal"`
	ModTime   string `json:"modTime"`
	SHA256    string `json:"sha256,omitempty"`
}

// build via tools/testutil.BuildTool after migration to tools/cmd/fs_stat

// runFsStat runs the built fs_stat tool with the given JSON input and decodes stdout.
func runFsStat(t *testing.T, bin string, input any) (fsStatOutput, string, int) {
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
	var out fsStatOutput
	_ = json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &out)
	return out, stderr.String(), code
}

// makeRepoRelTempFile creates a temporary file under the repository root and
// returns its repo-relative path. The file is cleaned up automatically.
func makeRepoRelTempFile(t *testing.T, dirPrefix string, data []byte) (relPath string) {
	t.Helper()
	tmpAbs, err := os.MkdirTemp(".", dirPrefix)
	if err != nil {
		t.Fatalf("mkdir temp under repo: %v", err)
	}
	base := filepath.Base(tmpAbs)
	fileRel := filepath.Join(base, "file.bin")
	if err := os.WriteFile(fileRel, data, 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	return fileRel
}

// TestFsStat_File expresses the minimal contract: for an existing regular file,
// the tool exits 0 and reports exists=true, type="file", and sizeBytes.
func TestFsStat_File(t *testing.T) {
	bin := testutil.BuildTool(t, "fs_stat")

	content := []byte("hello-fsstat")
	path := makeRepoRelTempFile(t, "fsstat-file-", content)

	out, stderr, code := runFsStat(t, bin, map[string]any{
		"path": path,
	})
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if !out.Exists {
		t.Fatalf("expected exists=true, got false")
	}
	if out.Type != "file" {
		t.Fatalf("expected type=file, got %q", out.Type)
	}
	if out.SizeBytes != int64(len(content)) {
		t.Fatalf("sizeBytes mismatch: got %d want %d", out.SizeBytes, len(content))
	}
}

// TestFsStat_MissingPath verifies that a non-existent path is handled
// gracefully: exit code 0 and exists=false in the JSON output.
func TestFsStat_MissingPath(t *testing.T) {
	bin := testutil.BuildTool(t, "fs_stat")

	// Use a path name that is very unlikely to exist under repo root.
	missing := filepath.Join("fsstat-missing-", "no-such-file.bin")

	out, stderr, code := runFsStat(t, bin, map[string]any{
		"path": missing,
	})
	if code != 0 {
		t.Fatalf("expected success (exit 0) for missing path, got exit=%d stderr=%q", code, stderr)
	}
	if out.Exists {
		t.Fatalf("expected exists=false for missing path")
	}
}

// TestFsStat_Symlink_NoFollow verifies that when followSymlinks=false, a symlink
// is reported with type="symlink" (not the target type).
func TestFsStat_Symlink_NoFollow(t *testing.T) {
	bin := testutil.BuildTool(t, "fs_stat")

	content := []byte("hello-symlink")
	target := makeRepoRelTempFile(t, "fsstat-symlink-target-", content)

	// Create a symlink alongside the target within repo root.
	link := target + ".lnk"
	// Use a relative target name so resolution is relative to link's directory.
	if err := os.Symlink(filepath.Base(target), link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(link) })

	out, stderr, code := runFsStat(t, bin, map[string]any{
		"path":           link,
		"followSymlinks": false,
	})
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if !out.Exists {
		t.Fatalf("expected exists=true, got false")
	}
	if out.Type != "symlink" {
		t.Fatalf("expected type=symlink when not following, got %q", out.Type)
	}
}

// TestFsStat_Symlink_Follow verifies that when followSymlinks=true, a symlink to
// a regular file reports the target type and size.
func TestFsStat_Symlink_Follow(t *testing.T) {
	bin := testutil.BuildTool(t, "fs_stat")

	content := []byte("hello-symlink-follow")
	target := makeRepoRelTempFile(t, "fsstat-symlink-follow-", content)
	link := target + ".lnk"
	if err := os.Symlink(filepath.Base(target), link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(link) })

	out, stderr, code := runFsStat(t, bin, map[string]any{
		"path":           link,
		"followSymlinks": true,
	})
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if !out.Exists {
		t.Fatalf("expected exists=true, got false")
	}
	if out.Type != "file" {
		t.Fatalf("expected type=file when following, got %q", out.Type)
	}
	if out.SizeBytes != int64(len(content)) {
		t.Fatalf("sizeBytes mismatch: got %d want %d", out.SizeBytes, len(content))
	}
}

// TestFsStat_SHA256 verifies that when hash="sha256" and the path is a regular
// file, the tool includes the SHA256 hex digest in the output.
func TestFsStat_SHA256(t *testing.T) {
	bin := testutil.BuildTool(t, "fs_stat")

	content := []byte("sha256-content\n")
	path := makeRepoRelTempFile(t, "fsstat-sha256-", content)

	out, stderr, code := runFsStat(t, bin, map[string]any{
		"path": path,
		"hash": "sha256",
	})
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if out.SHA256 == "" {
		t.Fatalf("expected sha256 present")
	}
}
