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

	testutil "github.com/hyperifyio/goagent/tools/testutil"
)

type fsMoveOutput struct {
	Moved bool `json:"moved"`
}

// buildFsMoveTool builds the fs_move tool using shared helper.
func buildFsMoveTool(t *testing.T) string {
	return testutil.BuildTool(t, "fs_move")
}

// runFsMove runs the built fs_move tool with the given JSON input and decodes stdout.
func runFsMove(t *testing.T, bin string, input any) (fsMoveOutput, string, int) {
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
	var out fsMoveOutput
	_ = json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &out)
	return out, stderr.String(), code
}


// TestFsMove_RenameSimple_NoOverwrite expresses the basic contract: renaming a file
// within the same filesystem should succeed when destination does not exist. The tool
// exits 0, outputs {"moved":true}, and the source disappears while destination appears
// with identical contents.
func TestFsMove_RenameSimple_NoOverwrite(t *testing.T) {
	// Build (will fail until fs_move is implemented)
	bin := buildFsMoveTool(t)

    dir := testutil.MakeRepoRelTempDir(t, "fsmove-basic-")
	src := filepath.Join(dir, "a.txt")
	dst := filepath.Join(dir, "b.txt")
	content := []byte("hello")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	out, stderr, code := runFsMove(t, bin, map[string]any{
		"from": src,
		"to":   dst,
	})
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if !out.Moved {
		t.Fatalf("expected moved=true, got false")
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("expected source removed, stat err=%v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content mismatch: got %q want %q", string(got), string(content))
	}
}

// TestFsMove_DestinationExists_OverwriteFalse ensures the tool refuses to
// clobber an existing destination when overwrite is false or omitted.
func TestFsMove_DestinationExists_OverwriteFalse(t *testing.T) {
	bin := buildFsMoveTool(t)
    dir := testutil.MakeRepoRelTempDir(t, "fsmove-overlap-")
	src := filepath.Join(dir, "a.txt")
	dst := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(src, []byte("one"), 0o644); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	if err := os.WriteFile(dst, []byte("two"), 0o644); err != nil {
		t.Fatalf("seed dst: %v", err)
	}

	// No overwrite flag provided
	_, stderr, code := runFsMove(t, bin, map[string]any{
		"from": src,
		"to":   dst,
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit due to destination exists, got 0; stderr=%q", stderr)
	}
}

// TestFsMove_DestinationExists_OverwriteTrue ensures the tool replaces an
// existing destination when overwrite is true.
func TestFsMove_DestinationExists_OverwriteTrue(t *testing.T) {
	bin := buildFsMoveTool(t)
    dir := testutil.MakeRepoRelTempDir(t, "fsmove-overwrite-")
	src := filepath.Join(dir, "a.txt")
	dst := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatalf("seed src: %v", err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed dst: %v", err)
	}

	out, stderr, code := runFsMove(t, bin, map[string]any{
		"from":      src,
		"to":        dst,
		"overwrite": true,
	})
	if code != 0 {
		t.Fatalf("expected success with overwrite, got exit=%d stderr=%q", code, stderr)
	}
	if !out.Moved {
		t.Fatalf("expected moved=true, got false")
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("expected source removed, stat err=%v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != "new" {
		t.Fatalf("expected destination content 'new', got %q", string(got))
	}
}

// TestFsMove_ErrorJSONContract verifies standardized error contract: on failure,
// the tool writes a single-line JSON object with an "error" key to stderr and
// exits non-zero. This aligns with L91 standardization.
func TestFsMove_ErrorJSONContract(t *testing.T) {
	bin := buildFsMoveTool(t)
	// Missing required fields triggers an error
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin)
	cmd.Dir = "."
	cmd.Stdin = bytes.NewReader([]byte(`{}`))
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected non-zero exit for invalid input; stderr=%q", stderr.String())
	}
	// Stderr must be a single-line JSON containing an "error" field
	line := strings.TrimSpace(stderr.String())
	var obj map[string]any
	if jerr := json.Unmarshal([]byte(line), &obj); jerr != nil {
		t.Fatalf("stderr is not JSON: %q err=%v", line, jerr)
	}
	if _, ok := obj["error"]; !ok {
		t.Fatalf("stderr JSON missing 'error' key: %v", obj)
	}
}
