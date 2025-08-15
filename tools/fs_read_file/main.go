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

type readReq struct {
	Path        string `json:"path"`
	OffsetBytes int    `json:"offsetBytes,omitempty"`
	MaxBytes    int    `json:"maxBytes,omitempty"`
}

type readOutput struct {
	ContentBase64 string `json:"contentBase64"`
	SizeBytes     int    `json:"sizeBytes"`
	EOF           bool   `json:"eof"`
}

func main() {
    in, err := readInputJSON(os.Stdin)
	if err != nil {
		stderrJSON(err)
		os.Exit(1)
	}
	if err := validatePath(in.Path); err != nil {
		stderrJSON(err)
		os.Exit(1)
	}
    content, size, eof, err := readFileRange(in.Path, in.OffsetBytes, in.MaxBytes)
	if err != nil {
		stderrJSON(err)
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(readOutput{ContentBase64: content, SizeBytes: size, EOF: eof})
}

func readInputJSON(r io.Reader) (readReq, error) {
    var in readReq
	b, err := io.ReadAll(bufio.NewReader(r))
	if err != nil {
		return in, fmt.Errorf("read stdin: %w", err)
	}
	if err := json.Unmarshal(b, &in); err != nil {
		return in, fmt.Errorf("parse json: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return in, errors.New("path is required")
	}
	if in.OffsetBytes < 0 || in.MaxBytes < 0 {
		return in, errors.New("offsetBytes/maxBytes must be >= 0")
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

func readFileRange(path string, offset, max int) (string, int, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", 0, false, fmt.Errorf("NOT_FOUND: %s", path)
		}
		return "", 0, false, err
	}
	size := len(data)
	if offset > size {
		offset = size
	}
	end := size
	if max > 0 && offset+max < end {
		end = offset + max
	}
	eof := end >= size
	chunk := data[offset:end]
	return base64.StdEncoding.EncodeToString(chunk), size, eof, nil
}

func stderrJSON(err error) {
	msg := err.Error()
	msg = strings.ReplaceAll(msg, "\n", " ")
	fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", msg)
}
