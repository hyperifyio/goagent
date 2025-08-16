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

	"github.com/hyperifyio/goagent/tools/testutil"
)

// Contract output for fs_read_lines
type fsReadLinesOutput struct {
	Content   string `json:"content"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	EOF       bool   `json:"eof"`
}

// buildFsReadLinesTool builds the fs_read_lines tool binary using canonical paths.
func buildFsReadLinesTool(t *testing.T) string {
	t.Helper()
	return testutil.BuildTool(t, "fs_read_lines")
}

// runFsReadLines executes the fs_read_lines tool with given JSON input.
func runFsReadLines(t *testing.T, bin string, input any) (fsReadLinesOutput, string, int) {
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
	var out fsReadLinesOutput
	_ = json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &out)
	return out, stderr.String(), code
}

// TestFsReadLines_LF_Simple verifies extracting a subset of LF-delimited lines.
func TestFsReadLines_LF_Simple(t *testing.T) {
	bin := buildFsReadLinesTool(t)

	// Arrange: create a repo-relative temp file with 5 LF lines
	tmpDirAbs, err := os.MkdirTemp(".", "fsread-lines-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDirAbs) })
	base := filepath.Base(tmpDirAbs)
	fileRel := filepath.Join(base, "data.txt")
	content := "l1\nl2\nl3\nl4\nl5\n"
	if err := os.WriteFile(fileRel, []byte(content), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	// Act: request [startLine=2, endLine=4] (1-based inclusive start, exclusive end)
	out, stderr, code := runFsReadLines(t, bin, map[string]any{
		"path":      fileRel,
		"startLine": 2,
		"endLine":   4,
	})

	// Assert expected success and correct output contract
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if out.StartLine != 2 || out.EndLine != 4 {
		t.Fatalf("range echoed mismatch: got (%d,%d) want (2,4)", out.StartLine, out.EndLine)
	}
	if out.Content != "l2\nl3\n" {
		t.Fatalf("content mismatch: got %q want %q", out.Content, "l2\nl3\n")
	}
	if out.EOF {
		t.Fatalf("unexpected EOF=true for partial read")
	}
}

// TestFsReadLines_CRLF_Normalize verifies CRLF input is normalized to LF in output.
func TestFsReadLines_CRLF_Normalize(t *testing.T) {
	bin := buildFsReadLinesTool(t)

	// Arrange: create a repo-relative temp file with CRLF line endings
	tmpDirAbs, err := os.MkdirTemp(".", "fsread-crlf-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDirAbs) })
	base := filepath.Base(tmpDirAbs)
	fileRel := filepath.Join(base, "data.txt")
	// 5 lines with CRLF endings
	content := "l1\r\nl2\r\nl3\r\nl4\r\nl5\r\n"
	if err := os.WriteFile(fileRel, []byte(content), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	// Act: request [startLine=2, endLine=5)
	out, stderr, code := runFsReadLines(t, bin, map[string]any{
		"path":      fileRel,
		"startLine": 2,
		"endLine":   5,
	})

	// Assert: success, LF-normalized content, EOF=false for partial range
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if out.StartLine != 2 || out.EndLine != 5 {
		t.Fatalf("range echoed mismatch: got (%d,%d) want (2,5)", out.StartLine, out.EndLine)
	}
	// Output must use LF even though input used CRLF
	if out.Content != "l2\nl3\nl4\n" {
		t.Fatalf("content mismatch: got %q want %q", out.Content, "l2\nl3\nl4\n")
	}
	if out.EOF {
		t.Fatalf("unexpected EOF=true for partial read")
	}
}

// TestFsReadLines_ErrorJSON verifies the standardized stderr JSON error contract
// for fs_read_lines: on invalid input, the tool must write a single-line JSON
// object with an "error" key to stderr and exit non-zero.
func TestFsReadLines_ErrorJSON(t *testing.T) {
	bin := buildFsReadLinesTool(t)

	// Use an absolute path to trigger validation failure.
	abs := string(os.PathSeparator) + filepath.Join("tmp", "fsread-abs.txt")

	_, stderr, code := runFsReadLines(t, bin, map[string]any{
		"path":      abs,
		"startLine": 1,
		"endLine":   2,
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit on invalid absolute path")
	}
	line := strings.TrimSpace(stderr)
	var obj map[string]any
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		t.Fatalf("stderr is not JSON: %q err=%v", line, err)
	}
	if _, ok := obj["error"]; !ok {
		t.Fatalf("stderr JSON missing 'error' key: %v", obj)
	}
}
