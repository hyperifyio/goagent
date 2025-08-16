package main

// https://github.com/hyperifyio/goagent/issues/1

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperifyio/goagent/tools/testutil"
)

type fsWriteOutput struct {
	BytesWritten int `json:"bytesWritten"`
}

// runFsWrite runs the built fs_write_file tool with the given JSON input.
func runFsWrite(t *testing.T, bin string, input any) (fsWriteOutput, string, int) {
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
	var out fsWriteOutput
	if code == 0 {
		if err := json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &out); err != nil {
			t.Fatalf("unmarshal stdout: %v; raw=%q", err, stdout.String())
		}
	}
	return out, stderr.String(), code
}

// makeRepoRelTempDir is now provided by tools/testutil.MakeRepoRelTempDir.

func TestFsWrite_CreateText(t *testing.T) {
	bin := testutil.BuildTool(t, "fs_write_file")
	dir := testutil.MakeRepoRelTempDir(t, "fswrite-text-")
	path := filepath.Join(dir, "hello.txt")
	content := []byte("hello world\n")
	out, stderr, code := runFsWrite(t, bin, map[string]any{
		"path":          path,
		"contentBase64": base64.StdEncoding.EncodeToString(content),
	})
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if out.BytesWritten != len(content) {
		t.Fatalf("bytesWritten mismatch: got %d want %d", out.BytesWritten, len(content))
	}
	readBack, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Equal(readBack, content) {
		t.Fatalf("content mismatch: got %q want %q", readBack, content)
	}
}

func TestFsWrite_Overwrite(t *testing.T) {
	bin := testutil.BuildTool(t, "fs_write_file")
	dir := testutil.MakeRepoRelTempDir(t, "fswrite-over-")
	path := filepath.Join(dir, "data.bin")
	// Seed with initial content
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	newContent := []byte("new-content")
	out, stderr, code := runFsWrite(t, bin, map[string]any{
		"path":          path,
		"contentBase64": base64.StdEncoding.EncodeToString(newContent),
	})
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if out.BytesWritten != len(newContent) {
		t.Fatalf("bytesWritten mismatch: got %d want %d", out.BytesWritten, len(newContent))
	}
	readBack, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Equal(readBack, newContent) {
		t.Fatalf("overwrite failed: got %q want %q", readBack, newContent)
	}
}

func TestFsWrite_Binary(t *testing.T) {
	bin := testutil.BuildTool(t, "fs_write_file")
	dir := testutil.MakeRepoRelTempDir(t, "fswrite-bin-")
	path := filepath.Join(dir, "bytes.bin")
	data := []byte{0x00, 0x10, 0xFF, 0x42, 0x00}
	out, stderr, code := runFsWrite(t, bin, map[string]any{
		"path":          path,
		"contentBase64": base64.StdEncoding.EncodeToString(data),
	})
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if out.BytesWritten != len(data) {
		t.Fatalf("bytesWritten mismatch: got %d want %d", out.BytesWritten, len(data))
	}
	readBack, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !bytes.Equal(readBack, data) {
		t.Fatalf("binary mismatch: got %v want %v", readBack, data)
	}
}

func TestFsWrite_MissingParent(t *testing.T) {
	bin := testutil.BuildTool(t, "fs_write_file")
	path := filepath.Join("no_such_parent_dir", "x", "file.txt")
	_, stderr, code := runFsWrite(t, bin, map[string]any{
		"path":          path,
		"contentBase64": base64.StdEncoding.EncodeToString([]byte("x")),
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit for missing parent")
	}
	if !strings.Contains(strings.ToUpper(stderr), "MISSING_PARENT") {
		t.Fatalf("stderr should contain MISSING_PARENT, got %q", stderr)
	}
}

// TestFsWrite_ErrorJSON_PathRequired verifies standardized error contract on missing required fields.
func TestFsWrite_ErrorJSON_PathRequired(t *testing.T) {
	bin := testutil.BuildTool(t, "fs_write_file")
	// Omit path to trigger validation error in readInput
	_, stderr, code := runFsWrite(t, bin, map[string]any{
		"contentBase64": base64.StdEncoding.EncodeToString([]byte("hello")),
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit code for missing path")
	}
	s := strings.TrimSpace(stderr)
	if s == "" || !strings.Contains(s, "\"error\"") {
		t.Fatalf("stderr should contain JSON with 'error' field, got: %q", stderr)
	}
}
