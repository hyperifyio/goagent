package main

import (
    "bufio"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"
)

type applyPatchInput struct {
	UnifiedDiff string `json:"unifiedDiff"`
}

func main() {
    if err := run(); err != nil {
        // Error contract: single-line JSON-ish message to stderr
        msg := strings.TrimSpace(err.Error())
        fmt.Fprintln(os.Stderr, msg)
        os.Exit(1)
    }
}

type applyResult struct {
    FilesChanged int `json:"filesChanged"`
}

func run() error {
    // Read all stdin (expected small JSON)
    data, err := io.ReadAll(os.Stdin)
    if err != nil {
        return fmt.Errorf("read stdin: %w", err)
    }
    var in applyPatchInput
    if err := json.Unmarshal(data, &in); err != nil {
        return fmt.Errorf("invalid JSON: %w", err)
    }
    if strings.TrimSpace(in.UnifiedDiff) == "" {
        return fmt.Errorf("missing unifiedDiff")
    }

    filesChanged, err := applyUnifiedDiffStrict(in.UnifiedDiff)
    if err != nil {
        return err
    }
    out := applyResult{FilesChanged: filesChanged}
    enc, _ := json.Marshal(out)
    fmt.Println(string(enc))
    return nil
}

// applyUnifiedDiffStrict implements a minimal strict applier that currently
// supports creating new files from hunks with --- /dev/null and +++ b/<path>.
// It rejects other forms to remain strict for this initial slice.
func applyUnifiedDiffStrict(diff string) (int, error) {
    scanner := bufio.NewScanner(strings.NewReader(diff))
    scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

    var oldPath, newPath string
    var hunkLines []string
    state := "start"

    for scanner.Scan() {
        line := scanner.Text()
        if strings.HasPrefix(line, "--- ") {
            oldPath = strings.TrimSpace(strings.TrimPrefix(line, "--- "))
            state = "have-old"
            continue
        }
        if strings.HasPrefix(line, "+++ ") {
            newPath = strings.TrimSpace(strings.TrimPrefix(line, "+++ "))
            state = "have-new"
            continue
        }
        if strings.HasPrefix(line, "@@ ") {
            if state != "have-new" {
                return 0, fmt.Errorf("unexpected hunk header without file headers")
            }
            state = "in-hunk"
            continue
        }
        if state == "in-hunk" {
            hunkLines = append(hunkLines, line)
        }
    }
    if err := scanner.Err(); err != nil {
        return 0, fmt.Errorf("scan diff: %w", err)
    }

    // Validate paths
    if oldPath == "" || newPath == "" {
        return 0, fmt.Errorf("missing file headers")
    }
    if oldPath != "/dev/null" {
        return 0, fmt.Errorf("only new-file diffs supported in this slice")
    }
    // newPath is typically prefixed with a/ or b/
    cleanNew := strings.TrimPrefix(newPath, "a/")
    cleanNew = strings.TrimPrefix(cleanNew, "b/")
    if filepath.IsAbs(cleanNew) || strings.HasPrefix(filepath.Clean(cleanNew), "..") {
        return 0, fmt.Errorf("path escapes repository root")
    }

    // Build file content from added lines (+prefix) and empty/context lines
    var contentBuilder strings.Builder
    for _, l := range hunkLines {
        if len(l) == 0 {
            contentBuilder.WriteString("\n")
            continue
        }
        switch l[0] {
        case '+':
            contentBuilder.WriteString(l[1:])
            contentBuilder.WriteString("\n")
        case ' ':
            // For new file hunks, context lines should not exist; be strict
            return 0, fmt.Errorf("unexpected context line in new-file hunk")
        case '-':
            // Deletions should not appear for new file
            return 0, fmt.Errorf("unexpected deletion line in new-file hunk")
        default:
            // Unknown prefix, reject strictly
            return 0, fmt.Errorf("invalid hunk line prefix")
        }
    }

    // If file already exists, be idempotent: succeed with no changes if
    // content matches exactly; otherwise fail strictly (no overwrite).
    if _, err := os.Stat(cleanNew); err == nil {
        existing, rerr := os.ReadFile(cleanNew)
        if rerr != nil {
            return 0, fmt.Errorf("read existing: %w", rerr)
        }
        if string(existing) == contentBuilder.String() {
            return 0, nil
        }
        return 0, fmt.Errorf("target exists: %s", cleanNew)
    } else if !errors.Is(err, os.ErrNotExist) {
        return 0, fmt.Errorf("stat target: %w", err)
    }

    // Ensure parent directories exist
    if err := os.MkdirAll(filepath.Dir(cleanNew), 0o755); err != nil {
        return 0, fmt.Errorf("mkdir parents: %w", err)
    }
    // Write file
    if err := os.WriteFile(cleanNew, []byte(contentBuilder.String()), 0o644); err != nil {
        return 0, fmt.Errorf("write file: %w", err)
    }
    return 1, nil
}
