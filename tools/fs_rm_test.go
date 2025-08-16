package main

// https://github.com/hyperifyio/goagent/issues/1

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type fsRmOutput struct {
	Removed bool `json:"removed"`
}

// buildFsRmTool builds ./tools/cmd/fs_rm into a temporary binary.
func buildFsRmTool(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "fs-rm")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/fs_rm")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build fs_rm tool: %v\n%s", err, string(out))
	}
	return binPath
}

// runFsRm runs the built fs_rm tool with the given JSON input and decodes stdout.
func runFsRm(t *testing.T, bin string, input any) (fsRmOutput, string, int) {
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
	var out fsRmOutput
	_ = json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &out)
	return out, stderr.String(), code
}

// TestFsRm_DeleteFile expresses the contract: deleting a regular file succeeds,
// tool exits 0, outputs {"removed":true}, and the file no longer exists.
func TestFsRm_DeleteFile(t *testing.T) {
	// Build (will fail until fs_rm is implemented)
	bin := buildFsRmTool(t)

	dir := makeRepoRelTempDir(t, "fsrm-")
	path := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	out, stderr, code := runFsRm(t, bin, map[string]any{
		"path": path,
	})
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if !out.Removed {
		t.Fatalf("expected removed=true, got false")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file to be removed, stat err=%v", err)
	}
}

// TestFsRm_DeleteDirRecursive expresses the next contract: deleting a directory
// tree with recursive=true succeeds, tool exits 0, outputs {"removed":true},
// and the directory no longer exists.
func TestFsRm_DeleteDirRecursive(t *testing.T) {
	bin := buildFsRmTool(t)

	dir := makeRepoRelTempDir(t, "fsrm-dir-")
	deep := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("mkdir tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(deep, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	out, stderr, code := runFsRm(t, bin, map[string]any{
		"path":      dir,
		"recursive": true,
	})
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if !out.Removed {
		t.Fatalf("expected removed=true, got false")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected directory to be removed, stat err=%v", err)
	}
}

// TestFsRm_ErrorJSON_PathRequired verifies that errors are reported as single-line
// JSON to stderr with an "error" field when required input is missing.
func TestFsRm_ErrorJSON_PathRequired(t *testing.T) {
	bin := buildFsRmTool(t)

	cmd := exec.Command(bin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = bytes.NewBufferString("{}")
	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected non-zero exit for missing path; stderr=%q", stderr.String())
	}
	var payload map[string]any
	if jerr := json.Unmarshal(bytes.TrimSpace(stderr.Bytes()), &payload); jerr != nil {
		t.Fatalf("stderr is not valid JSON: %v; got %q", jerr, stderr.String())
	}
	if _, ok := payload["error"]; !ok {
		t.Fatalf("stderr JSON missing 'error' field: %v", payload)
	}
}

// TestFsRm_ForceOnMissing verifies force=true on a missing path exits 0,
// returns {"removed":false}, and the path remains absent.
func TestFsRm_ForceOnMissing(t *testing.T) {
	bin := buildFsRmTool(t)

	dir := makeRepoRelTempDir(t, "fsrm-missing-")
	path := filepath.Join(dir, "absent.txt")

	out, stderr, code := runFsRm(t, bin, map[string]any{
		"path":  path,
		"force": true,
	})
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if out.Removed {
		t.Fatalf("expected removed=false for missing path with force=true")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected path to be absent, stat err=%v", err)
	}
}
