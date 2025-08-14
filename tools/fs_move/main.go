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
	var in moveInput
	if err := json.Unmarshal(inBytes, &in); err != nil {
		return fmt.Errorf("bad json: %w", err)
	}
	if strings.TrimSpace(in.From) == "" || strings.TrimSpace(in.To) == "" {
		return fmt.Errorf("from and to are required")
	}
	if filepath.IsAbs(in.From) || filepath.IsAbs(in.To) {
		return fmt.Errorf("paths must be relative to repository root")
	}
	from := filepath.Clean(in.From)
	to := filepath.Clean(in.To)
	if strings.HasPrefix(from, "..") || strings.HasPrefix(to, "..") {
		return fmt.Errorf("path escapes repository root")
	}
	// Ensure destination parent exists
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return fmt.Errorf("mkdir dest parent: %w", err)
	}
	// If destination exists and overwrite is false, block.
	if !in.Overwrite {
		if _, err := os.Lstat(to); err == nil {
			return fmt.Errorf("destination exists and overwrite=false")
		}
	}
	// Try simple rename first
	if err := os.Rename(from, to); err == nil {
		return writeJSON(moveOutput{Moved: true})
	} else if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("source not found: %s", from)
	} else {
		// Cross-device: fallback to copy+remove
		if err := copyFileOrDir(from, to, in.Overwrite); err != nil {
			return err
		}
		if err := os.RemoveAll(from); err != nil {
			return fmt.Errorf("remove source: %w", err)
		}
		return writeJSON(moveOutput{Moved: true})
	}
}

func copyFileOrDir(src, dst string, overwrite bool) error {
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}
	if info.IsDir() {
		return copyDir(src, dst, overwrite)
	}
	return copyFile(src, dst, overwrite)
}

func copyDir(src, dst string, overwrite bool) error {
	// Create destination dir
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("mkdir dst: %w", err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("readdir: %w", err)
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(s, d, overwrite); err != nil {
				return err
			}
		} else {
			if err := copyFile(s, d, overwrite); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string, overwrite bool) error {
	if !overwrite {
		if _, err := os.Lstat(dst); err == nil {
			return fmt.Errorf("destination exists and overwrite=false")
		}
	}
	srcF, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer srcF.Close()
	info, err := srcF.Stat()
	if err != nil {
		return fmt.Errorf("stat src: %w", err)
	}
	tmp := dst + ".tmp-copy"
	dstF, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("open dst: %w", err)
	}
	defer func() { _ = dstF.Close(); _ = os.Remove(tmp) }()
	if _, err := io.Copy(dstF, srcF); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	if err := dstF.Close(); err != nil {
		return fmt.Errorf("close dst: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("rename into place: %w", err)
	}
	return nil
}

func writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	fmt.Println(string(b))
	return nil
}
