package main

// https://github.com/hyperifyio/goagent/issues/1

import (
    "bytes"
    "crypto/sha256"
    "encoding/base64"
    "encoding/hex"
    "encoding/json"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"

    testutil "github.com/hyperifyio/goagent/tools/testutil"
)

type fsEditRangeOutput struct {
	BytesReplaced int    `json:"bytesReplaced"`
	NewSha256     string `json:"newSha256"`
}

// buildFsEditRangeTool builds ./tools/fs_edit_range into a temporary binary.
func buildFsEditRangeTool(t *testing.T) string { return testutil.BuildTool(t, "fs_edit_range") }

// runFsEditRange executes the fs_edit_range tool with given JSON input.
func runFsEditRange(t *testing.T, bin string, input any) (fsEditRangeOutput, string, int) {
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
	var out fsEditRangeOutput
	_ = json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &out)
	return out, stderr.String(), code
}

// TestFsEditRange_MidFile_Splicing encodes the core contract:
// replace bytes in [startByte:endByte) with replacementBase64, atomically.
func TestFsEditRange_MidFile_Splicing(t *testing.T) {
	bin := buildFsEditRangeTool(t)

	// Arrange: create a repo-relative temp file with known content
	tmpDirAbs, err := os.MkdirTemp(".", "fsedit-mid-")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDirAbs) })
	base := filepath.Base(tmpDirAbs)
	fileRel := filepath.Join(base, "data.bin")
	orig := []byte("abcdef") // indices: 0 1 2 3 4 5
	if err := os.WriteFile(fileRel, orig, 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	// Act: replace bytes [2:4) ("cd") with "XY"
	repl := []byte("XY")
	out, stderr, code := runFsEditRange(t, bin, map[string]any{
		"path":               fileRel,
		"startByte":          2,
		"endByte":            4,
		"replacementBase64":  base64.StdEncoding.EncodeToString(repl),
	})

	// Assert expected success and correct output contract
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if out.BytesReplaced != len(repl) {
		t.Fatalf("bytesReplaced mismatch: got %d want %d", out.BytesReplaced, len(repl))
	}
	// Verify file content
	got, err := os.ReadFile(fileRel)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	want := []byte("abXYef")
	if !bytes.Equal(got, want) {
		t.Fatalf("content mismatch: got %q want %q", got, want)
	}
	// Verify reported SHA matches actual SHA-256 of new content (hex-encoded)
	sum := sha256.Sum256(got)
	wantHex := hex.EncodeToString(sum[:])
	if !strings.EqualFold(out.NewSha256, wantHex) {
		t.Fatalf("newSha256 mismatch: got %q want %q", out.NewSha256, wantHex)
	}
}

// TestFsEditRange_Beginning_Splicing ensures replacement at the beginning [0:n).
func TestFsEditRange_Beginning_Splicing(t *testing.T) {
    bin := buildFsEditRangeTool(t)

    // Arrange
    tmpDirAbs, err := os.MkdirTemp(".", "fsedit-beg-")
    if err != nil {
        t.Fatalf("mkdir temp: %v", err)
    }
    t.Cleanup(func() { _ = os.RemoveAll(tmpDirAbs) })
    base := filepath.Base(tmpDirAbs)
    fileRel := filepath.Join(base, "data.bin")
    orig := []byte("abcdef")
    if err := os.WriteFile(fileRel, orig, 0o644); err != nil {
        t.Fatalf("seed file: %v", err)
    }

    // Act: replace bytes [0:2) ("ab") with "ZZ"
    repl := []byte("ZZ")
    out, stderr, code := runFsEditRange(t, bin, map[string]any{
        "path":              fileRel,
        "startByte":         0,
        "endByte":           2,
        "replacementBase64": base64.StdEncoding.EncodeToString(repl),
    })

    // Assert
    if code != 0 {
        t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
    }
    if out.BytesReplaced != len(repl) {
        t.Fatalf("bytesReplaced mismatch: got %d want %d", out.BytesReplaced, len(repl))
    }
    got, err := os.ReadFile(fileRel)
    if err != nil {
        t.Fatalf("read back: %v", err)
    }
    want := []byte("ZZcdef")
    if !bytes.Equal(got, want) {
        t.Fatalf("content mismatch: got %q want %q", got, want)
    }
    sum := sha256.Sum256(got)
    wantHex := hex.EncodeToString(sum[:])
    if !strings.EqualFold(out.NewSha256, wantHex) {
        t.Fatalf("newSha256 mismatch: got %q want %q", out.NewSha256, wantHex)
    }
}

// TestFsEditRange_End_Splicing ensures replacement at the end [size-2:size).
func TestFsEditRange_End_Splicing(t *testing.T) {
    bin := buildFsEditRangeTool(t)

    // Arrange
    tmpDirAbs, err := os.MkdirTemp(".", "fsedit-end-")
    if err != nil {
        t.Fatalf("mkdir temp: %v", err)
    }
    t.Cleanup(func() { _ = os.RemoveAll(tmpDirAbs) })
    base := filepath.Base(tmpDirAbs)
    fileRel := filepath.Join(base, "data.bin")
    orig := []byte("abcdef")
    if err := os.WriteFile(fileRel, orig, 0o644); err != nil {
        t.Fatalf("seed file: %v", err)
    }

    // Act: replace bytes [4:6) ("ef") with "ZZZ"
    repl := []byte("ZZZ")
    out, stderr, code := runFsEditRange(t, bin, map[string]any{
        "path":              fileRel,
        "startByte":         4,
        "endByte":           6,
        "replacementBase64": base64.StdEncoding.EncodeToString(repl),
    })

    // Assert
    if code != 0 {
        t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
    }
    if out.BytesReplaced != len(repl) {
        t.Fatalf("bytesReplaced mismatch: got %d want %d", out.BytesReplaced, len(repl))
    }
    got, err := os.ReadFile(fileRel)
    if err != nil {
        t.Fatalf("read back: %v", err)
    }
    want := []byte("abcdZZZ")
    if !bytes.Equal(got, want) {
        t.Fatalf("content mismatch: got %q want %q", got, want)
    }
    sum := sha256.Sum256(got)
    wantHex := hex.EncodeToString(sum[:])
    if !strings.EqualFold(out.NewSha256, wantHex) {
        t.Fatalf("newSha256 mismatch: got %q want %q", out.NewSha256, wantHex)
    }
}
