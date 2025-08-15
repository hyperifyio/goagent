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
)

type writeInput struct {
	Path            string `json:"path"`
	ContentBase64   string `json:"contentBase64"`
	CreateModeOctal string `json:"createModeOctal,omitempty"`
}

type writeOutput struct {
	BytesWritten int `json:"bytesWritten"`
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
	data, err := base64.StdEncoding.DecodeString(in.ContentBase64)
	if err != nil {
		stderrJSON(fmt.Errorf("BAD_BASE64: %w", err))
		os.Exit(1)
	}
    // Require parent directory to exist; do not create it implicitly
    parent := filepath.Dir(in.Path)
    if st, err := os.Stat(parent); err != nil || !st.IsDir() {
        if err == nil {
            // exists but not a directory
            stderrJSON(fmt.Errorf("MISSING_PARENT: %s is not a directory", parent))
        } else if os.IsNotExist(err) {
            stderrJSON(fmt.Errorf("MISSING_PARENT: %s", parent))
        } else {
            stderrJSON(fmt.Errorf("MISSING_PARENT: %v", err))
        }
        os.Exit(1)
    }
	mode := os.FileMode(0o644)
	if strings.TrimSpace(in.CreateModeOctal) != "" {
		if m, perr := parseOctalMode(in.CreateModeOctal); perr == nil {
			mode = m
		}
	}
	if err := atomicWriteFile(in.Path, data, mode); err != nil {
		stderrJSON(err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(writeOutput{BytesWritten: len(data)})
}

func readInput(r io.Reader) (writeInput, error) {
	var in writeInput
	br := bufio.NewReader(r)
	b, err := io.ReadAll(br)
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
		return fmt.Errorf("ABSOLUTE_PATH: %s", p)
	}
	clean := filepath.ToSlash(filepath.Clean(p))
	if strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return fmt.Errorf("PATH_ESCAPE: %s", p)
	}
	return nil
}

func parseOctalMode(s string) (os.FileMode, error) {
	var m uint32
	_, err := fmt.Sscanf(s, "%o", &m)
	if err != nil {
		return 0, err
	}
	return os.FileMode(m), nil
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func stderrJSON(err error) {
	msg := err.Error()
	msg = strings.ReplaceAll(msg, "\n", " ")
	fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", msg)
}
