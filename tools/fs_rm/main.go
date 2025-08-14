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

type rmInput struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
	Force     bool   `json:"force"`
}

type rmOutput struct {
	Removed bool `json:"removed"`
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
	var in rmInput
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
	st, err := os.Lstat(clean)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if in.Force {
				return writeJSON(rmOutput{Removed: false})
			}
			return fmt.Errorf("NOT_FOUND: %s", clean)
		}
		return fmt.Errorf("lstat: %w", err)
	}
	if st.IsDir() {
		if !in.Recursive {
			return fmt.Errorf("refuse to remove directory without recursive=true")
		}
		if err := os.RemoveAll(clean); err != nil {
			return fmt.Errorf("remove dir: %w", err)
		}
	} else {
		if err := os.Remove(clean); err != nil {
			return fmt.Errorf("remove file: %w", err)
		}
	}
	return writeJSON(rmOutput{Removed: true})
}

func writeJSON(v any) error {
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
	return nil
}
