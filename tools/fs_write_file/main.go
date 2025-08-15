package main

import (
    "encoding/base64"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strconv"
    "strings"
)

type writeInput struct {
	Path            string `json:"path"`
	ContentBase64   string `json:"contentBase64"`
	CreateModeOctal string `json:"createModeOctal"`
}

type writeOutput struct {
	BytesWritten int `json:"bytesWritten"`
}

func main() {
	if err := run(); err != nil {
        // Standardized error contract: single-line JSON to stderr.
        writeStdErrJSON(err)
		os.Exit(1)
	}
}

func run() error {
	inBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	if len(strings.TrimSpace(string(inBytes))) == 0 {
		return fmt.Errorf("missing json input")
	}
	var in writeInput
	if err := json.Unmarshal(inBytes, &in); err != nil {
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
	if strings.TrimSpace(in.ContentBase64) == "" {
		return fmt.Errorf("contentBase64 is required")
	}
	data, err := base64.StdEncoding.DecodeString(in.ContentBase64)
	if err != nil {
		return fmt.Errorf("decode base64: %w", err)
	}
	parent := filepath.Dir(clean)
	if parent == "." {
		parent = ""
	}
	if parent != "" {
		if st, err := os.Stat(parent); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("MISSING_PARENT: %s", parent)
			}
			return fmt.Errorf("stat parent: %w", err)
		} else if !st.IsDir() {
			return fmt.Errorf("parent is not a directory: %s", parent)
		}
	}

	mode := os.FileMode(0o644)
	if strings.TrimSpace(in.CreateModeOctal) != "" {
		if m, perr := strconv.ParseUint(in.CreateModeOctal, 8, 32); perr == nil {
			mode = os.FileMode(m)
		}
	}

	// Atomic write: temp file in same dir + rename
	tempDir := parent
	if tempDir == "" {
		tempDir = "."
	}
	tmp, err := os.CreateTemp(tempDir, ".fswrite-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	// Ensure cleanup on failure
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(tmp.Name(), clean); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	out := writeOutput{BytesWritten: len(data)}
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
	return nil
}

// writeStdErrJSON writes {"error":"..."} as a single line to stderr.
func writeStdErrJSON(err error) {
    msg := strings.TrimSpace(err.Error())
    payload := map[string]string{"error": msg}
    b, _ := json.Marshal(payload)
    fmt.Fprintln(os.Stderr, string(b))
}
