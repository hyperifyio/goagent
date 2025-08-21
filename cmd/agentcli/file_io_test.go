package main

import (
    "bytes"
    "os"
    "path/filepath"
    "testing"
)

func TestWriteFileAtomic_CreatesParentAndWrites(t *testing.T) {
    d := t.TempDir()
    p := filepath.Join(d, "nested", "file.txt")
    data := []byte("hello world")
    if err := writeFileAtomic(p, data, 0o644); err != nil {
        t.Fatalf("writeFileAtomic error: %v", err)
    }
    b, err := os.ReadFile(p)
    if err != nil {
        t.Fatalf("reading wrote file: %v", err)
    }
    if !bytes.Equal(b, data) {
        t.Fatalf("content mismatch: got %q want %q", string(b), string(data))
    }
    if _, err := os.Stat(p + ".tmp"); err == nil || !os.IsNotExist(err) {
        t.Fatalf("temp file should not exist after rename")
    }
}

func TestResolveMaybeFile(t *testing.T) {
    // Inline when file path empty
    if got, err := resolveMaybeFile("inline text", ""); err != nil || got != "inline text" {
        t.Fatalf("inline case: got %q err=%v", got, err)
    }
    // From file
    d := t.TempDir()
    fp := filepath.Join(d, "in.txt")
    if err := os.WriteFile(fp, []byte("from-file"), 0o644); err != nil {
        t.Fatalf("prep file: %v", err)
    }
    if got, err := resolveMaybeFile("ignored", fp); err != nil || got != "from-file" {
        t.Fatalf("file case: got %q err=%v", got, err)
    }
    // From STDIN when filePath == "-"
    oldStdin := os.Stdin
    defer func() { os.Stdin = oldStdin }()
    stdinFile := filepath.Join(d, "stdin.txt")
    if err := os.WriteFile(stdinFile, []byte("from-stdin"), 0o644); err != nil {
        t.Fatalf("prep stdin file: %v", err)
    }
    f, err := os.Open(stdinFile)
    if err != nil { t.Fatalf("open stdin file: %v", err) }
    os.Stdin = f
    got, err := resolveMaybeFile("ignored", "-")
    if err != nil || got != "from-stdin" {
        t.Fatalf("stdin case: got %q err=%v", got, err)
    }
}
