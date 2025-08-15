package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type moveInput struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Overwrite bool   `json:"overwrite"`
}

type moveOutput struct {
	Moved bool `json:"moved"`
}

func main() {
	if err := run(); err != nil {
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
	var in moveInput
	if err := json.Unmarshal(inBytes, &in); err != nil {
		return fmt.Errorf("bad json: %w", err)
	}
	if strings.TrimSpace(in.From) == "" || strings.TrimSpace(in.To) == "" {
		return fmt.Errorf("from and to are required")
	}
	if filepath.IsAbs(in.From) || filepath.IsAbs(in.To) {
		return fmt.Errorf("path must be relative to repository root")
	}
	from := filepath.Clean(in.From)
	to := filepath.Clean(in.To)
	if strings.HasPrefix(from, "..") || strings.HasPrefix(to, "..") {
		return fmt.Errorf("path escapes repository root")
	}
	// Ensure destination parent exists if specified
	parent := filepath.Dir(to)
	if parent != "." && parent != "" {
		if st, err := os.Stat(parent); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("MISSING_PARENT: %s", parent)
			}
			return fmt.Errorf("stat parent: %w", err)
		} else if !st.IsDir() {
			return fmt.Errorf("parent is not a directory: %s", parent)
		}
	}
	// Handle existing destination
	if _, err := os.Lstat(to); err == nil {
		if !in.Overwrite {
			return fmt.Errorf("destination exists and overwrite is false")
		}
		// Remove existing destination (file only for our tests)
		if err := os.Remove(to); err != nil {
			return fmt.Errorf("remove existing dest: %w", err)
		}
	}
	// Fast path: simple rename
	if err := os.Rename(from, to); err == nil {
		return writeJSON(moveOutput{Moved: true})
	} else {
		// Fallback: copy then remove (e.g., cross-device or other rename errors)
		if cerr := copyFile(from, to); cerr != nil {
			return fmt.Errorf("move: %w", err)
		}
		if rerr := os.Remove(from); rerr != nil {
			return fmt.Errorf("remove source: %w", rerr)
		}
		return writeJSON(moveOutput{Moved: true})
	}
}

func copyFile(src, dst string) error {
	srcF, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcF.Close()
	st, err := srcF.Stat()
	if err != nil {
		return err
	}
	dstF, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode().Perm())
	if err != nil {
		return err
	}
	defer func() { _ = dstF.Close() }()
	if _, err := io.Copy(dstF, srcF); err != nil {
		return err
	}
	return nil
}

func writeJSON(v any) error {
	b, _ := json.Marshal(v)
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
