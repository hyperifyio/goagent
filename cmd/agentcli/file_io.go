package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// writeFileAtomic writes data to path atomically by writing to a temp file
// in the same directory and then renaming it over the destination. Parent
// directories are created if missing.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// resolveMaybeFile returns the effective content from either an inline string
// or a file path when provided. When filePath is "-", it reads from STDIN.
// If filePath is non-empty, it takes precedence over inline.
func resolveMaybeFile(inline string, filePath string) (string, error) {
	f := strings.TrimSpace(filePath)
	if f == "" {
		return inline, nil
	}
	if f == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read STDIN: %w", err)
		}
		return string(b), nil
	}
	b, err := os.ReadFile(f)
	if err != nil {
		return "", fmt.Errorf("read file %s: %w", f, err)
	}
	return string(b), nil
}

// resolveDeveloperMessages aggregates developer messages from repeatable flags and files.
func resolveDeveloperMessages(inlines []string, files []string) ([]string, error) {
	var out []string
	for _, f := range files {
		s, err := resolveMaybeFile("", f)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	out = append(out, inlines...)
	return out, nil
}
