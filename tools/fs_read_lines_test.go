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

// Contract output for fs_read_lines
type fsReadLinesOutput struct {
    Content   string `json:"content"`
    StartLine int    `json:"startLine"`
    EndLine   int    `json:"endLine"`
    EOF       bool   `json:"eof"`
}

// buildFsReadLinesTool builds ./tools/fs_read_lines into a temporary binary.
func buildFsReadLinesTool(t *testing.T) string {
    t.Helper()
    tmpDir := t.TempDir()
    binPath := filepath.Join(tmpDir, "fs-read-lines")
    cmd := exec.Command("go", "build", "-o", binPath, "./fs_read_lines")
    cmd.Dir = "."
    out, err := cmd.CombinedOutput()
    if err != nil {
        t.Fatalf("failed to build fs_read_lines tool: %v\n%s", err, string(out))
    }
    return binPath
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
