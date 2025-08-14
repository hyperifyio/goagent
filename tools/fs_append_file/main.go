package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type appendInput struct {
	Path          string `json:"path"`
	ContentBase64 string `json:"contentBase64"`
}

type appendOutput struct {
	BytesAppended int `json:"bytesAppended"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, strings.TrimSpace(err.Error()))
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
	var in appendInput
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
	// Ensure parent exists
	parent := filepath.Dir(clean)
	if parent != "." && parent != "" {
		if st, err := os.Stat(parent); err != nil {
			return fmt.Errorf("MISSING_PARENT: %s", parent)
		} else if !st.IsDir() {
			return fmt.Errorf("parent is not a directory: %s", parent)
		}
	}
	f, err := os.OpenFile(clean, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()
	// Advisory lock to serialize writers
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	// Single write to avoid interleaving
	n, err := f.Write(data)
	if err != nil {
		return fmt.Errorf("write: %w", err)
	}
	out := appendOutput{BytesAppended: n}
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
	return nil
}
