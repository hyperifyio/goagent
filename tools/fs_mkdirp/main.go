package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type mkdirpInput struct {
	Path      string `json:"path"`
	ModeOctal string `json:"modeOctal"`
}

type mkdirpOutput struct {
	Created bool `json:"created"`
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
	var in mkdirpInput
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
	mode := os.FileMode(0o755)
	if strings.TrimSpace(in.ModeOctal) != "" {
		mstr := in.ModeOctal
		if strings.HasPrefix(mstr, "0") {
			mstr = mstr[1:]
		}
		mv, perr := strconv.ParseUint(mstr, 8, 32)
		if perr != nil {
			return fmt.Errorf("invalid modeOctal")
		}
		mode = os.FileMode(mv)
	}
	if info, err := os.Stat(clean); err == nil {
		if info.IsDir() {
			return writeJSON(mkdirpOutput{Created: false})
		}
		return fmt.Errorf("path exists and is not a directory")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat path: %w", err)
	}
	if err := os.MkdirAll(clean, mode); err != nil {
		return fmt.Errorf("mkdirall: %w", err)
	}
	return writeJSON(mkdirpOutput{Created: true})
}

func writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	fmt.Println(string(b))
	return nil
}
