package main

// https://github.com/hyperifyio/goagent/issues/1

import (
    "bytes"
    "encoding/base64"
    "encoding/json"
    "os"
    "os/exec"
    "path/filepath"
    "runtime"
    "strings"
    "sync"
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
    cmd := exec.Command("go", "build", "-o", binPath, "./fs_append_file")
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

func makeRepoRelTempDirAppend(t *testing.T, prefix string) string {
    t.Helper()
    tmpAbs, err := os.MkdirTemp(".", prefix)
    if err != nil {
        t.Fatalf("mkdir temp under repo: %v", err)
    }
    base := filepath.Base(tmpAbs)
    t.Cleanup(func() { _ = os.RemoveAll(base) })
    return base
}

func TestFsAppend_DoubleAppend(t *testing.T) {
    bin := buildFsAppendTool(t)
    dir := makeRepoRelTempDirAppend(t, "fsappend-double-")
    path := filepath.Join(dir, "data.txt")
    // First append creates the file
    chunk1 := []byte("A")
    out1, stderr1, code1 := runFsAppend(t, bin, map[string]any{
        "path":          path,
        "contentBase64": base64.StdEncoding.EncodeToString(chunk1),
    })
    if code1 != 0 {
        t.Fatalf("expected success, got exit=%d stderr=%q", code1, stderr1)
    }
    if out1.BytesAppended != len(chunk1) {
        t.Fatalf("bytesAppended mismatch: got %d want %d", out1.BytesAppended, len(chunk1))
    }
    // Second append
    chunk2 := []byte("B")
    out2, stderr2, code2 := runFsAppend(t, bin, map[string]any{
        "path":          path,
        "contentBase64": base64.StdEncoding.EncodeToString(chunk2),
    })
    if code2 != 0 {
        t.Fatalf("expected success, got exit=%d stderr=%q", code2, stderr2)
    }
    if out2.BytesAppended != len(chunk2) {
        t.Fatalf("bytesAppended mismatch: got %d want %d", out2.BytesAppended, len(chunk2))
    }
    // Verify content
    data, err := os.ReadFile(path)
    if err != nil {
        t.Fatalf("read back: %v", err)
    }
    if string(data) != "AB" {
        t.Fatalf("content mismatch: got %q want %q", string(data), "AB")
    }
}

func TestFsAppend_ConcurrentWriters(t *testing.T) {
    bin := buildFsAppendTool(t)
    dir := makeRepoRelTempDirAppend(t, "fsappend-conc-")
    path := filepath.Join(dir, "concurrent.txt")

    // Prepare N unique chunks
    n := runtime.GOMAXPROCS(0)
    if n < 4 { n = 4 }
    chunks := make([][]byte, n)
    for i := 0; i < n; i++ {
        chunks[i] = []byte(strings.Repeat(string('A'+byte(i%26)), 256))
    }

    var wg sync.WaitGroup
    wg.Add(n)
    errs := make([]string, n)
    for i := 0; i < n; i++ {
        i := i
        go func() {
            defer wg.Done()
            _, stderr, code := runFsAppend(t, bin, map[string]any{
                "path":          path,
                "contentBase64": base64.StdEncoding.EncodeToString(chunks[i]),
            })
            if code != 0 {
                errs[i] = stderr
            }
        }()
    }
    wg.Wait()
    for i, e := range errs {
        if e != "" {
            t.Fatalf("concurrent append %d failed: %q", i, e)
        }
    }

    // Validate final content: length and each chunk present exactly once
    data, err := os.ReadFile(path)
    if err != nil {
        t.Fatalf("read final: %v", err)
    }
    total := 0
    for _, c := range chunks { total += len(c) }
    if len(data) != total {
        t.Fatalf("final length mismatch: got %d want %d", len(data), total)
    }
    for i, c := range chunks {
        got := strings.Count(string(data), string(c))
        if got != 1 {
            t.Fatalf("chunk %d occurrence mismatch: got %d want 1", i, got)
        }
    }
}
