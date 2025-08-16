package main

import (
    "bufio"
    "encoding/base64"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"
    "sync"
)

type appendInput struct {
    Path          string `json:"path"`
    ContentBase64 string `json:"contentBase64"`
}

type appendOutput struct {
    BytesAppended int `json:"bytesAppended"`
}

var fileLocks sync.Map // map[string]*sync.Mutex

func main() {
    in, err := readInput(os.Stdin)
    if err != nil {
        stderrJSON(err)
        os.Exit(1)
    }
    if err := validatePath(in.Path); err != nil {
        stderrJSON(err)
        os.Exit(1)
    }
    data, err := base64.StdEncoding.DecodeString(in.ContentBase64)
    if err != nil {
        stderrJSON(fmt.Errorf("decode base64: %w", err))
        os.Exit(1)
    }
    // advisory lock per-path
    muIface, _ := fileLocks.LoadOrStore(in.Path, &sync.Mutex{})
    mu := muIface.(*sync.Mutex)
    mu.Lock()
    defer mu.Unlock()

    f, err := os.OpenFile(in.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
    if err != nil {
        stderrJSON(err)
        os.Exit(1)
    }
    defer f.Close()
    if _, err := f.Write(data); err != nil {
        stderrJSON(err)
        os.Exit(1)
    }
    _ = json.NewEncoder(os.Stdout).Encode(appendOutput{BytesAppended: len(data)})
}

func readInput(r io.Reader) (appendInput, error) {
    var in appendInput
    b, err := io.ReadAll(bufio.NewReader(r))
    if err != nil {
        return in, fmt.Errorf("read stdin: %w", err)
    }
    if err := json.Unmarshal(b, &in); err != nil {
        return in, fmt.Errorf("parse json: %w", err)
    }
    if strings.TrimSpace(in.Path) == "" {
        return in, errors.New("path is required")
    }
    if strings.TrimSpace(in.ContentBase64) == "" {
        return in, errors.New("contentBase64 is required")
    }
    return in, nil
}

func validatePath(p string) error {
    if filepath.IsAbs(p) {
        return fmt.Errorf("path must be relative to repository root: %s", p)
    }
    clean := filepath.ToSlash(filepath.Clean(p))
    if strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
        return fmt.Errorf("path escapes repository root: %s", p)
    }
    return nil
}

func stderrJSON(err error) {
    msg := err.Error()
    msg = strings.ReplaceAll(msg, "\n", " ")
    fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", msg)
}
