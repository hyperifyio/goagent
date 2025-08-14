package main

import (
    "encoding/json"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"
)

// https://github.com/hyperifyio/goagent/issues/1

type fsRmInput struct {
    Path      string `json:"path"`
    Recursive bool   `json:"recursive"`
    Force     bool   `json:"force"`
}

type fsRmOutput struct {
    Removed bool `json:"removed"`
}

func main() {
    if err := run(); err != nil {
        // Print a concise, single-line error to stderr and exit non-zero
        fmt.Fprintln(os.Stderr, strings.TrimSpace(err.Error()))
        os.Exit(1)
    }
}

func run() error {
    inputBytes, err := io.ReadAll(os.Stdin)
    if err != nil {
        return fmt.Errorf("read stdin: %w", err)
    }
    if len(strings.TrimSpace(string(inputBytes))) == 0 {
        return fmt.Errorf("missing json input")
    }

    var in fsRmInput
    if err := json.Unmarshal(inputBytes, &in); err != nil {
        return fmt.Errorf("bad json: %w", err)
    }
    if strings.TrimSpace(in.Path) == "" {
        return fmt.Errorf("path is required")
    }
    if filepath.IsAbs(in.Path) {
        return fmt.Errorf("path must be relative to repository root")
    }
    clean := filepath.Clean(in.Path)
    if strings.HasPrefix(clean, "..") {
        return fmt.Errorf("path escapes repository root")
    }

    // Minimal implementation for S02: delete regular file only.
    info, err := os.Lstat(clean)
    if err != nil {
        if os.IsNotExist(err) {
            if in.Force {
                return writeJSON(fsRmOutput{Removed: false})
            }
            return fmt.Errorf("path does not exist")
        }
        return fmt.Errorf("stat: %w", err)
    }
    if info.IsDir() {
        // Directory handling (recursive) is out of scope for this slice.
        return fmt.Errorf("path is a directory; recursive delete not implemented")
    }

    if err := os.Remove(clean); err != nil {
        if os.IsNotExist(err) {
            if in.Force {
                return writeJSON(fsRmOutput{Removed: false})
            }
            return fmt.Errorf("path does not exist")
        }
        return fmt.Errorf("remove: %w", err)
    }

    return writeJSON(fsRmOutput{Removed: true})
}

func writeJSON(v any) error {
    b, err := json.Marshal(v)
    if err != nil {
        return fmt.Errorf("marshal: %w", err)
    }
    fmt.Println(string(b))
    return nil
}
