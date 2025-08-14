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

type fsSearchMatch struct {
    Path    string `json:"path"`
    Line    int    `json:"line"`
    Col     int    `json:"col"`
    Preview string `json:"preview"`
}

type fsSearchOutput struct {
    Matches   []fsSearchMatch `json:"matches"`
    Truncated bool            `json:"truncated"`
}

// buildFsSearch builds ./tools/fs_search into a temporary binary.
func buildFsSearch(t *testing.T) string {
    t.Helper()
    tmpDir := t.TempDir()
    binPath := filepath.Join(tmpDir, "fs-search")
    cmd := exec.Command("go", "build", "-o", binPath, "./fs_search")
    cmd.Dir = "."
    out, err := cmd.CombinedOutput()
    if err != nil {
        t.Fatalf("failed to build fs_search tool: %v\n%s", err, string(out))
    }
    return binPath
}

// runFsSearch executes the fs_search tool with given JSON input.
func runFsSearch(t *testing.T, bin string, input any) (fsSearchOutput, string, int) {
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
    var out fsSearchOutput
    _ = json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &out)
    return out, stderr.String(), code
}

// TestFsSearch_Literal_SingleFile creates a small file and searches for a literal string.
func TestFsSearch_Literal_SingleFile(t *testing.T) {
    // Arrange: create a repo-relative temp file with known content
    tmpDirAbs, err := os.MkdirTemp(".", "fssearch-lit-")
    if err != nil {
        t.Fatalf("mkdir temp: %v", err)
    }
    t.Cleanup(func() { _ = os.RemoveAll(tmpDirAbs) })
    base := filepath.Base(tmpDirAbs)
    fileRel := filepath.Join(base, "a.txt")
    content := "alpha\nbravo charlie\nalpha bravo\n"
    if err := os.WriteFile(fileRel, []byte(content), 0o644); err != nil {
        t.Fatalf("write file: %v", err)
    }

    bin := buildFsSearch(t)

    // Act: literal search for "bravo"
    out, stderr, code := runFsSearch(t, bin, map[string]any{
        "query":      "bravo",
        "regex":      false,
        "globs":      []string{"**/*.txt"},
        "maxResults": 10,
    })

    // Assert: should succeed (exit 0), have at least one match in our file, not truncated
    if code != 0 {
        t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
    }
    if out.Truncated {
        t.Fatalf("should not be truncated for small input")
    }
    // Find a match in our file
    found := false
    for _, m := range out.Matches {
        if m.Path == fileRel {
            if m.Line <= 0 || m.Col <= 0 {
                t.Fatalf("invalid line/col: line=%d col=%d", m.Line, m.Col)
            }
            if !strings.Contains(m.Preview, "bravo") {
                t.Fatalf("preview should contain query, got %q", m.Preview)
            }
            found = true
            break
        }
    }
    if !found {
        t.Fatalf("expected at least one match in %s, got %+v", fileRel, out.Matches)
    }
}
