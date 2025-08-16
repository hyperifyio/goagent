package main

import (
    "bufio"
    "encoding/json"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"
)

type moveInput struct {
    From      string `json:"from"`
    To        string `json:"to"`
    Overwrite bool   `json:"overwrite,omitempty"`
}

type moveOutput struct {
    Moved bool `json:"moved"`
}

func main() {
    in, err := readInput(os.Stdin)
    if err != nil {
        stderrJSON(err)
        os.Exit(1)
    }
    if err := validatePath(in.From); err != nil {
        stderrJSON(err)
        os.Exit(1)
    }
    if err := validatePath(in.To); err != nil {
        stderrJSON(err)
        os.Exit(1)
    }
    moved, err := move(in.From, in.To, in.Overwrite)
    if err != nil {
        stderrJSON(err)
        os.Exit(1)
    }
    _ = json.NewEncoder(os.Stdout).Encode(moveOutput{Moved: moved})
}

func readInput(r io.Reader) (moveInput, error) {
    var in moveInput
    b, err := io.ReadAll(bufio.NewReader(r))
    if err != nil {
        return in, fmt.Errorf("read stdin: %w", err)
    }
    if err := json.Unmarshal(b, &in); err != nil {
        return in, fmt.Errorf("parse json: %w", err)
    }
    if strings.TrimSpace(in.From) == "" || strings.TrimSpace(in.To) == "" {
        return in, fmt.Errorf("from and to are required")
    }
    return in, nil
}

func validatePath(p string) error {
    if filepath.IsAbs(p) {
        return fmt.Errorf("ABSOLUTE_PATH: %s", p)
    }
    clean := filepath.ToSlash(filepath.Clean(p))
    if strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
        return fmt.Errorf("PATH_ESCAPE: %s", p)
    }
    return nil
}

func move(from, to string, overwrite bool) (bool, error) {
    // If destination exists
    if st, err := os.Lstat(to); err == nil {
        if !overwrite {
            return false, fmt.Errorf("DEST_EXISTS: %s", to)
        }
        // Remove destination (file or dir)
        if st.IsDir() {
            if err := os.RemoveAll(to); err != nil {
                return false, err
            }
        } else {
            if err := os.Remove(to); err != nil {
                return false, err
            }
        }
    }
    // Try simple rename first
    if err := os.Rename(from, to); err == nil {
        return true, nil
    }
    // Copy+remove
    src, err := os.Open(from)
    if err != nil {
        return false, err
    }
    defer src.Close()
    if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
        return false, err
    }
    dst, err := os.Create(to)
    if err != nil {
        return false, err
    }
    defer func() { _ = dst.Close() }()
    if _, err := io.Copy(dst, src); err != nil {
        return false, err
    }
    if err := os.RemoveAll(from); err != nil {
        return false, err
    }
    return true, nil
}

func stderrJSON(err error) {
    msg := err.Error()
    msg = strings.ReplaceAll(msg, "\n", " ")
    fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", msg)
}
