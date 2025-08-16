package main

import (
    "bytes"
    "crypto/sha256"
    "encoding/base64"
    "encoding/hex"
    "os"
    "path/filepath"
    "testing"

    testutil "github.com/hyperifyio/goagent/tools/testutil"
)

// TestFsEditRange_BinaryContent verifies splicing works for arbitrary binary bytes.
func TestFsEditRange_BinaryContent(t *testing.T) {
    bin := testutil.BuildTool(t, "fs_edit_range")

    // Arrange: repo-relative temporary directory and binary file
    tmpDirAbs, err := os.MkdirTemp(".", "fsedit-bin-")
    if err != nil {
        t.Fatalf("mkdir temp: %v", err)
    }
    t.Cleanup(func() { _ = os.RemoveAll(tmpDirAbs) })
    base := filepath.Base(tmpDirAbs)
    fileRel := filepath.Join(base, "data.bin")
    orig := []byte{0x00, 0x01, 0x02, 0xFF, 0x10, 0x11}
    if err := os.WriteFile(fileRel, orig, 0o644); err != nil {
        t.Fatalf("seed file: %v", err)
    }

    // Act: replace bytes [2:5) (0x02,0xFF,0x10) with {0xAA,0xBB}
    repl := []byte{0xAA, 0xBB}
    out, stderr, code := runFsEditRange(t, bin, map[string]any{
        "path":              fileRel,
        "startByte":         2,
        "endByte":           5,
        "replacementBase64": base64.StdEncoding.EncodeToString(repl),
    })
    if code != 0 {
        t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
    }
    if out.BytesReplaced != len(repl) {
        t.Fatalf("bytesReplaced mismatch: got %d want %d", out.BytesReplaced, len(repl))
    }

    // Assert: file content and reported SHA
    got, err := os.ReadFile(fileRel)
    if err != nil {
        t.Fatalf("read back: %v", err)
    }
    want := []byte{0x00, 0x01, 0xAA, 0xBB, 0x11}
    if !bytes.Equal(got, want) {
        t.Fatalf("content mismatch: got %v want %v", got, want)
    }
    sum := sha256.Sum256(got)
    wantHex := hex.EncodeToString(sum[:])
    if out.NewSha256 != wantHex {
        t.Fatalf("newSha256 mismatch: got %q want %q", out.NewSha256, wantHex)
    }
}
