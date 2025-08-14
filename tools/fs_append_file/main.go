package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// inputSpecAppend models the stdin JSON contract for fs_append_file.
// {"path":"string","contentBase64":"string"}
// Path is repository-relative (no absolute paths, no .. escapes).
// On success, print single-line JSON {"bytesAppended":int} to stdout and exit 0.
// On error, print a single line to stderr and exit non-zero.

type inputSpecAppend struct {
	Path          string `json:"path"`
	ContentBase64 string `json:"contentBase64"`
}

type outputSpecAppend struct {
	BytesAppended int `json:"bytesAppended"`
}

func main() {
	if err := run(); err != nil {
		msg := strings.TrimSpace(err.Error())
		fmt.Fprintln(os.Stderr, msg)
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
	var in inputSpecAppend
	if err := json.Unmarshal(inBytes, &in); err != nil {
		return fmt.Errorf("bad json: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return fmt.Errorf("path is required")
	}
	if strings.TrimSpace(in.ContentBase64) == "" {
		return fmt.Errorf("contentBase64 is required")
	}
	if filepath.IsAbs(in.Path) {
		return fmt.Errorf("path must be relative to repository root")
	}
	clean := filepath.Clean(in.Path)
	if strings.HasPrefix(clean, "..") {
		return fmt.Errorf("path escapes repository root")
	}

	// Ensure parent directory exists
	dir := filepath.Dir(clean)
	if dir != "." && dir != "" {
		st, err := os.Stat(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("MISSING_PARENT: %s", dir)
			}
			return fmt.Errorf("stat parent: %w", err)
		}
		if !st.IsDir() {
			return fmt.Errorf("parent is not a directory: %s", dir)
		}
	}

	// Decode content
	data, err := base64.StdEncoding.DecodeString(in.ContentBase64)
	if err != nil {
		return fmt.Errorf("decode base64: %v", err)
	}

	// Open file for append (create if not exists)
	f, err := os.OpenFile(clean, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	// Advisory lock (exclusive) to serialize concurrent writers
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("flock: %w", err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	written := 0
	if len(data) > 0 {
		n, werr := f.Write(data)
		if werr != nil {
			return fmt.Errorf("write: %w", werr)
		}
		written = n
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("fsync: %w", err)
	}

	out := outputSpecAppend{BytesAppended: written}
	b, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	fmt.Println(string(b))
	return nil
}
