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
    "sync"
    "testing"

    testutil "github.com/hyperifyio/goagent/tools/testutil"
)

type fsAppendOutput struct {
	BytesAppended int `json:"bytesAppended"`
}

// Build via shared helper in tools/testutil.

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
    bin := testutil.BuildTool(t, "fs_append_file")

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

func TestFsAppend_Validation_MissingPath(t *testing.T) {
    bin := testutil.BuildTool(t, "fs_append_file")
	_, stderr, code := runFsAppend(t, bin, map[string]any{
		"path":          "",
		"contentBase64": base64.StdEncoding.EncodeToString([]byte("data")),
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit for missing path")
	}
	if !strings.Contains(strings.ToLower(stderr), "path is required") {
		t.Fatalf("stderr should mention path is required, got %q", stderr)
	}
}

func TestFsAppend_Validation_MissingContent(t *testing.T) {
    bin := testutil.BuildTool(t, "fs_append_file")
	dir := makeRepoRelTempDir(t, "fsappend-validate-")
	path := filepath.Join(dir, "x.txt")
	_, stderr, code := runFsAppend(t, bin, map[string]any{
		"path":          path,
		"contentBase64": "",
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit for missing contentBase64")
	}
	if !strings.Contains(strings.ToLower(stderr), "contentbase64 is required") {
		t.Fatalf("stderr should mention contentBase64 is required, got %q", stderr)
	}
}

func TestFsAppend_Validation_AbsolutePath(t *testing.T) {
    bin := testutil.BuildTool(t, "fs_append_file")
	abs := filepath.Join("/", "tmp", "x.txt")
	_, stderr, code := runFsAppend(t, bin, map[string]any{
		"path":          abs,
		"contentBase64": base64.StdEncoding.EncodeToString([]byte("x")),
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit for absolute path")
	}
	if !strings.Contains(strings.ToLower(stderr), "path must be relative to repository root") {
		t.Fatalf("stderr should mention relative path requirement, got %q", stderr)
	}
}

func TestFsAppend_Validation_PathEscape(t *testing.T) {
    bin := testutil.BuildTool(t, "fs_append_file")
	_, stderr, code := runFsAppend(t, bin, map[string]any{
		"path":          filepath.Join("..", "escape.txt"),
		"contentBase64": base64.StdEncoding.EncodeToString([]byte("x")),
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit for path escape")
	}
	if !strings.Contains(strings.ToLower(stderr), "path escapes repository root") {
		t.Fatalf("stderr should mention path escapes repository root, got %q", stderr)
	}
}

func TestFsAppend_Validation_BadBase64(t *testing.T) {
    bin := testutil.BuildTool(t, "fs_append_file")
	dir := makeRepoRelTempDir(t, "fsappend-validate-")
	path := filepath.Join(dir, "bad.txt")
	_, stderr, code := runFsAppend(t, bin, map[string]any{
		"path":          path,
		"contentBase64": "!!!not-base64!!!",
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit for bad base64")
	}
	if !strings.Contains(strings.ToLower(stderr), "decode base64") {
		t.Fatalf("stderr should mention base64 decode failure, got %q", stderr)
	}
}

func TestFsAppend_ConcurrentWriters(t *testing.T) {
    bin := testutil.BuildTool(t, "fs_append_file")

	dir := makeRepoRelTempDir(t, "fsappend-concurrent-")
	path := filepath.Join(dir, "concurrent.txt")

	// Distinct payloads to allow order-agnostic verification via counts
	partA := bytes.Repeat([]byte("A"), 10000)
	partB := bytes.Repeat([]byte("B"), 12000)

	var wg sync.WaitGroup
	wg.Add(2)

	var out1 fsAppendOutput
	var err1 string
	var code1 int
	go func() {
		defer wg.Done()
		out1, err1, code1 = runFsAppend(t, bin, map[string]any{
			"path":          path,
			"contentBase64": base64.StdEncoding.EncodeToString(partA),
		})
	}()

	var out2 fsAppendOutput
	var err2 string
	var code2 int
	go func() {
		defer wg.Done()
		out2, err2, code2 = runFsAppend(t, bin, map[string]any{
			"path":          path,
			"contentBase64": base64.StdEncoding.EncodeToString(partB),
		})
	}()

	wg.Wait()

	if code1 != 0 {
		t.Fatalf("first concurrent append expected success, got exit=%d stderr=%q", code1, err1)
	}
	if code2 != 0 {
		t.Fatalf("second concurrent append expected success, got exit=%d stderr=%q", code2, err2)
	}
	if out1.BytesAppended != len(partA) {
		t.Fatalf("bytesAppended mismatch for first writer: got %d want %d", out1.BytesAppended, len(partA))
	}
	if out2.BytesAppended != len(partB) {
		t.Fatalf("bytesAppended mismatch for second writer: got %d want %d", out2.BytesAppended, len(partB))
	}

	// Verify final content length and composition (order-agnostic)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	wantLen := len(partA) + len(partB)
	if len(got) != wantLen {
		t.Fatalf("final size mismatch: got %d want %d", len(got), wantLen)
	}
	var countA, countB int
	for _, b := range got {
		if b == 'A' {
			countA++
		} else if b == 'B' {
			countB++
		}
	}
	if countA != len(partA) || countB != len(partB) {
		t.Fatalf("content composition mismatch: countA=%d want %d, countB=%d want %d", countA, len(partA), countB, len(partB))
	}
}
