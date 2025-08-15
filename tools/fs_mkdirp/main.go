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

type mkdirpInput struct {
	Path     string `json:"path"`
	ModeOctal string `json:"modeOctal,omitempty"`
}

type mkdirpOutput struct {
	Created bool `json:"created"`
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
	mode := os.FileMode(0o755)
	if strings.TrimSpace(in.ModeOctal) != "" {
		if m, perr := parseOctalMode(in.ModeOctal); perr == nil {
			mode = m
		}
	}
	created := false
	if _, err := os.Stat(in.Path); os.IsNotExist(err) {
		created = true
	}
	if err := os.MkdirAll(in.Path, mode); err != nil {
		stderrJSON(err)
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(mkdirpOutput{Created: created})
}

func readInput(r io.Reader) (mkdirpInput, error) {
	var in mkdirpInput
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

func parseOctalMode(s string) (os.FileMode, error) {
	var m uint32
	_, err := fmt.Sscanf(s, "%o", &m)
	if err != nil {
		return 0, err
	}
	return os.FileMode(m), nil
}

func stderrJSON(err error) {
	msg := err.Error()
	msg = strings.ReplaceAll(msg, "\n", " ")
	fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", msg)
}
