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
)

type fsAppendOutput struct {
	BytesAppended int `json:"bytesAppended"`
}

// buildFsAppendTool builds ./tools/fs_append_file.go into a temporary binary.
func buildFsAppendTool(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "fs-append-file")
	cmd := exec.Command("go", "build", "-o", binPath, "./fs_append_file.go")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build fs_append_file tool: %v\n%s", err, string(out))
	}
	return binPath
}

// runFsAppend runs the built fs_append_file tool with the given JSON input.
func runFsAppend(t *testing.T, bin string, input any) (fsAppendOutput, string, int) {
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
	var out fsAppendOutput
	_ = json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &out)
	return out, stderr.String(), code
}

func TestFsAppend_DoubleAppend(t *testing.T) {
	// Build (will initially fail until tool is implemented)
	bin := buildFsAppendTool(t)

	dir := makeRepoRelTempDir(t, "fsappend-double-")
	path := filepath.Join(dir, "hello.txt")

	part1 := []byte("hello")
	out1, stderr1, code1 := runFsAppend(t, bin, map[string]any{
		"path":          path,
		"contentBase64": base64.StdEncoding.EncodeToString(part1),
	})
	if code1 != 0 {
		t.Fatalf("first append expected success, got exit=%d stderr=%q", code1, stderr1)
	}
	if out1.BytesAppended != len(part1) {
		t.Fatalf("bytesAppended mismatch on first append: got %d want %d", out1.BytesAppended, len(part1))
	}

	part2 := []byte(" world")
	out2, stderr2, code2 := runFsAppend(t, bin, map[string]any{
		"path":          path,
		"contentBase64": base64.StdEncoding.EncodeToString(part2),
	})
	if code2 != 0 {
		t.Fatalf("second append expected success, got exit=%d stderr=%q", code2, stderr2)
	}
	if out2.BytesAppended != len(part2) {
		t.Fatalf("bytesAppended mismatch on second append: got %d want %d", out2.BytesAppended, len(part2))
	}

	// Verify final file content
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	want := append(append([]byte{}, part1...), part2...)
	if !bytes.Equal(got, want) {
		t.Fatalf("content mismatch: got %q want %q", got, want)
	}
}
