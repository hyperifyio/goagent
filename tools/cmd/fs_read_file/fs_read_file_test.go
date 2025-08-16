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

type fsReadOutput struct {
	ContentBase64 string `json:"contentBase64"`
	SizeBytes     int64  `json:"sizeBytes"`
	EOF           bool   `json:"eof"`
}

// build via tools/testutil.BuildTool after migration to tools/cmd/fs_read_file

// runFsRead runs the built fs_read_file tool with the given JSON input and decodes stdout.
func runFsRead(t *testing.T, bin string, input any) (fsReadOutput, string, int) {
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
	var out fsReadOutput
	_ = json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &out)
	return out, stderr.String(), code
}

func makeRepoRelTempFile(t *testing.T, dirPrefix string, data []byte) (relPath string) {
	t.Helper()
	// Create a temp directory under repo root (current directory).
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

func TestFsRead_TextFile(t *testing.T) {
    bin := testutil.BuildTool(t, "fs_read_file")
	content := []byte("hello world\n")
	path := makeRepoRelTempFile(t, "fsread-text-", content)
	out, stderr, code := runFsRead(t, bin, map[string]any{
		"path": path,
	})
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if out.SizeBytes != int64(len(content)) {
		t.Fatalf("sizeBytes mismatch: got %d want %d", out.SizeBytes, len(content))
	}
	if !out.EOF {
		t.Fatalf("expected EOF=true")
	}
	decoded, err := base64.StdEncoding.DecodeString(out.ContentBase64)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	if !bytes.Equal(decoded, content) {
		t.Fatalf("content mismatch: got %q want %q", decoded, content)
	}
}

func TestFsRead_BinaryRoundTrip(t *testing.T) {
    bin := testutil.BuildTool(t, "fs_read_file")
	data := []byte{0x00, 0x10, 0xFF, 0x42, 0x00}
	path := makeRepoRelTempFile(t, "fsread-bin-", data)
	out, stderr, code := runFsRead(t, bin, map[string]any{"path": path})
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	decoded, err := base64.StdEncoding.DecodeString(out.ContentBase64)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bytes.Equal(decoded, data) {
		t.Fatalf("binary mismatch: got %v want %v", decoded, data)
	}
}

func TestFsRead_Ranges(t *testing.T) {
    bin := testutil.BuildTool(t, "fs_read_file")
	data := []byte("abcdefg")
	path := makeRepoRelTempFile(t, "fsread-range-", data)
	// offset=2, max=3 -> cde, eof=false
	out1, stderr1, code1 := runFsRead(t, bin, map[string]any{"path": path, "offsetBytes": 2, "maxBytes": 3})
	if code1 != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code1, stderr1)
	}
	b1, _ := base64.StdEncoding.DecodeString(out1.ContentBase64)
	if string(b1) != "cde" || out1.EOF {
		t.Fatalf("unexpected range1: content=%q eof=%v", string(b1), out1.EOF)
	}
	// offset=5, max=10 -> fg, eof=true
	out2, stderr2, code2 := runFsRead(t, bin, map[string]any{"path": path, "offsetBytes": 5, "maxBytes": 10})
	if code2 != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code2, stderr2)
	}
	b2, _ := base64.StdEncoding.DecodeString(out2.ContentBase64)
	if string(b2) != "fg" || !out2.EOF {
		t.Fatalf("unexpected range2: content=%q eof=%v", string(b2), out2.EOF)
	}
}

func TestFsRead_NotFound(t *testing.T) {
    bin := testutil.BuildTool(t, "fs_read_file")
	_, stderr, code := runFsRead(t, bin, map[string]any{"path": "this/does/not/exist.txt"})
	if code == 0 {
		t.Fatalf("expected non-zero exit for missing file")
	}
    if !strings.Contains(strings.ToUpper(stderr), "NOT_FOUND") {
        t.Fatalf("stderr should contain NOT_FOUND, got %q", stderr)
    }
}

// TestFsRead_ErrorJSON verifies standardized error contract: on failure,
// the tool writes a single-line JSON object to stderr with an "error" key
// and exits non-zero.
func TestFsRead_ErrorJSON(t *testing.T) {
    bin := testutil.BuildTool(t, "fs_read_file")

    // Use an absolute path to trigger validation failure (repo-relative enforced).
    abs := string(os.PathSeparator) + filepath.Join("tmp", "fsread-abs.txt")

    _, stderr, code := runFsRead(t, bin, map[string]any{
        "path": abs,
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
