package main

// https://github.com/hyperifyio/goagent/issues/1

import (
    "bytes"
    "encoding/base64"
    "os"
    "path/filepath"
    "sync"
    "testing"

    testutil "github.com/hyperifyio/goagent/tools/testutil"
)

// TestFsEditRange_Concurrent_Serializes asserts two concurrent edits are serialized
// resulting in a state equivalent to some sequential execution.
// We choose equal-length replacements so byte indices remain stable.
func TestFsEditRange_Concurrent_Serializes(t *testing.T) {
    bin := testutil.BuildTool(t, "fs_edit_range")

    // Arrange: seed repo-relative temp file
    tmpDirAbs, err := os.MkdirTemp(".", "fsedit-conc-")
    if err != nil {
        t.Fatalf("mkdir temp: %v", err)
    }
    t.Cleanup(func() { _ = os.RemoveAll(tmpDirAbs) })
    base := filepath.Base(tmpDirAbs)
    fileRel := filepath.Join(base, "data.bin")
    orig := []byte("abcdef") // 0..5
    if err := os.WriteFile(fileRel, orig, 0o644); err != nil {
        t.Fatalf("seed file: %v", err)
    }

    // Edits: E1 [2:4) -> "XY"; E2 [4:6) -> "ZZ" (equal lengths)
    // Sequentially applied, final should be "abXYZZ" regardless of order.
    var wg sync.WaitGroup
    wg.Add(2)
    var code1, code2 int
    go func() {
        defer wg.Done()
        _, _, code1 = runFsEditRange(t, bin, map[string]any{
            "path":              fileRel,
            "startByte":         2,
            "endByte":           4,
            "replacementBase64": base64.StdEncoding.EncodeToString([]byte("XY")),
        })
    }()
    go func() {
        defer wg.Done()
        _, _, code2 = runFsEditRange(t, bin, map[string]any{
            "path":              fileRel,
            "startByte":         4,
            "endByte":           6,
            "replacementBase64": base64.StdEncoding.EncodeToString([]byte("ZZ")),
        })
    }()
    wg.Wait()

    if code1 != 0 || code2 != 0 {
        t.Fatalf("expected both edits to succeed, got codes (%d,%d)", code1, code2)
    }

    got, err := os.ReadFile(fileRel)
    if err != nil {
        t.Fatalf("read back: %v", err)
    }
    want := []byte("abXYZZ")
    if !bytes.Equal(got, want) {
        t.Fatalf("final content not serializable: got %q want %q", got, want)
    }
}
