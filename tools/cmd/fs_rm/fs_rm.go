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

type rmInput struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive,omitempty"`
	Force     bool   `json:"force,omitempty"`
}

type rmOutput struct {
	Removed bool `json:"removed"`
}

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
	removed, err := removePath(in.Path, in.Recursive, in.Force)
	if err != nil {
		stderrJSON(err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(rmOutput{Removed: removed}); err != nil {
		stderrJSON(fmt.Errorf("encode json: %w", err))
		os.Exit(1)
	}
}

func readInput(r io.Reader) (rmInput, error) {
	var in rmInput
	b, err := io.ReadAll(bufio.NewReader(r))
	if err != nil {
		return in, fmt.Errorf("read stdin: %w", err)
	}
	if err := json.Unmarshal(b, &in); err != nil {
		return in, fmt.Errorf("parse json: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return in, fmt.Errorf("path is required")
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

func removePath(path string, recursive, force bool) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			if force {
				return false, nil
			}
			return false, fmt.Errorf("NOT_FOUND: %s", path)
		}
		return false, err
	}
	if info.IsDir() {
		if !recursive {
			return false, fmt.Errorf("IS_DIR: %s", path)
		}
		return true, os.RemoveAll(path)
	}
	return true, os.Remove(path)
}

func stderrJSON(err error) {
	msg := err.Error()
	msg = strings.ReplaceAll(msg, "\n", " ")
	fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", msg)
}
