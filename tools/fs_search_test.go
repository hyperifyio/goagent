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

// TestFsSearch_Regex_SingleFile adds a failing test to define regex behavior.
// It expects the tool to support regex queries when {"regex":true}.
// https://github.com/hyperifyio/goagent/issues/1
func TestFsSearch_Regex_SingleFile(t *testing.T) {
    // Arrange: create a repo-relative temp file with known content
    tmpDirAbs, err := os.MkdirTemp(".", "fssearch-regex-")
    if err != nil {
        t.Fatalf("mkdir temp: %v", err)
    }
    t.Cleanup(func() { _ = os.RemoveAll(tmpDirAbs) })
    base := filepath.Base(tmpDirAbs)
    fileRel := filepath.Join(base, "r.txt")
    content := "alpha\nbravo charlie\nalpha bravo\n"
    if err := os.WriteFile(fileRel, []byte(content), 0o644); err != nil {
        t.Fatalf("write file: %v", err)
    }

    bin := buildFsSearch(t)

    // Act: regex search for lines starting with "alpha"
    out, stderr, code := runFsSearch(t, bin, map[string]any{
        "query":      "^alpha",
        "regex":      true,
        "globs":      []string{"**/*.txt"},
        "maxResults": 10,
    })

    // Assert: should succeed and find at least one match in our file
    if code != 0 {
        t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
    }
    if out.Truncated {
        t.Fatalf("should not be truncated for small input")
    }
    found := false
    for _, m := range out.Matches {
        if m.Path == fileRel {
            if m.Line <= 0 || m.Col <= 0 {
                t.Fatalf("invalid line/col: line=%d col=%d", m.Line, m.Col)
            }
            if !strings.HasPrefix(m.Preview, "alpha") {
                t.Fatalf("preview should start with 'alpha', got %q", m.Preview)
            }
            found = true
            break
        }
    }
    if !found {
        t.Fatalf("expected at least one match in %s, got %+v", fileRel, out.Matches)
    }
}

// TestFsSearch_Globs_Filter verifies glob filtering limits files considered.
// It expects that only files matching the provided globs are searched.
// https://github.com/hyperifyio/goagent/issues/1
func TestFsSearch_Globs_Filter(t *testing.T) {
    tmpDirAbs, err := os.MkdirTemp(".", "fssearch-glob-")
    if err != nil {
        t.Fatalf("mkdir temp: %v", err)
    }
    t.Cleanup(func() { _ = os.RemoveAll(tmpDirAbs) })
    base := filepath.Base(tmpDirAbs)

    txtRel := filepath.Join(base, "note.txt")
    mdRel := filepath.Join(base, "note.md")
    if err := os.WriteFile(txtRel, []byte("needle in txt\n"), 0o644); err != nil {
        t.Fatalf("write txt: %v", err)
    }
    if err := os.WriteFile(mdRel, []byte("needle in md\n"), 0o644); err != nil {
        t.Fatalf("write md: %v", err)
    }

    bin := buildFsSearch(t)

    // Act: literal search with globs restricting to only .md files
    out, stderr, code := runFsSearch(t, bin, map[string]any{
        "query":      "needle",
        "regex":      false,
        "globs":      []string{"**/*.md"},
        "maxResults": 10,
    })
    if code != 0 {
        t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
    }
    if out.Truncated {
        t.Fatalf("should not be truncated for small input")
    }

    // Assert: should contain match for mdRel and not for txtRel
    var sawMD, sawTXT bool
    for _, m := range out.Matches {
        if m.Path == mdRel {
            sawMD = true
        }
        if m.Path == txtRel {
            sawTXT = true
        }
    }
    if !sawMD {
        t.Fatalf("expected a match in %s, got %+v", mdRel, out.Matches)
    }
    if sawTXT {
        t.Fatalf("did not expect a match in %s due to globs filter", txtRel)
    }
}

// TestFsSearch_Truncation verifies that when maxResults is reached, the tool
// stops early, sets Truncated=true, and returns exactly maxResults matches.
// https://github.com/hyperifyio/goagent/issues/1
func TestFsSearch_Truncation(t *testing.T) {
    // Arrange: create a repo-relative temp dir with a file containing many matches
    tmpDirAbs, err := os.MkdirTemp(".", "fssearch-trunc-")
    if err != nil {
        t.Fatalf("mkdir temp: %v", err)
    }
    t.Cleanup(func() { _ = os.RemoveAll(tmpDirAbs) })
    base := filepath.Base(tmpDirAbs)

    fileRel := filepath.Join(base, "many.txt")
    // Create a line with multiple occurrences and multiple lines to ensure >2 matches
    content := "x x x x x\nxx xx\n"
    if err := os.WriteFile(fileRel, []byte(content), 0o644); err != nil {
        t.Fatalf("write file: %v", err)
    }

    bin := buildFsSearch(t)

    // Act: literal search for "x" with maxResults=2
    out, stderr, code := runFsSearch(t, bin, map[string]any{
        "query":      "x",
        "regex":      false,
        "globs":      []string{"**/*.txt"},
        "maxResults": 2,
    })

    // Assert
    if code != 0 {
        t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
    }
    if !out.Truncated {
        t.Fatalf("expected truncated=true when reaching maxResults, got false")
    }
    if len(out.Matches) != 2 {
        t.Fatalf("expected exactly 2 matches, got %d: %+v", len(out.Matches), out.Matches)
    }
    for _, m := range out.Matches {
        if m.Path != fileRel {
            t.Fatalf("unexpected path %q (want %q)", m.Path, fileRel)
        }
        if m.Line <= 0 || m.Col <= 0 {
            t.Fatalf("invalid line/col: line=%d col=%d", m.Line, m.Col)
        }
        if !strings.Contains(m.Preview, "x") {
            t.Fatalf("preview should contain 'x', got %q", m.Preview)
        }
    }
}
