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
)

// inputSpec models the stdin JSON contract for fs_read_file.
// {"path":"string","offsetBytes?:int,"maxBytes?:int}
type inputSpec struct {
	Path        string `json:"path"`
	OffsetBytes int64  `json:"offsetBytes"`
	MaxBytes    int64  `json:"maxBytes"`
}

// outputSpec is the stdout JSON contract on success.
// {"contentBase64":"string","sizeBytes":int,"eof":bool}
type outputSpec struct {
	ContentBase64 string `json:"contentBase64"`
	SizeBytes     int64  `json:"sizeBytes"`
	EOF           bool   `json:"eof"`
}

func main() {
	if err := run(); err != nil {
		// Error contract: single-line message to stderr.
		// Include NOT_FOUND marker when applicable for deterministic tests.
		msg := strings.TrimSpace(err.Error())
		if errors.Is(err, os.ErrNotExist) || strings.Contains(strings.ToUpper(msg), "NOT_FOUND") {
			fmt.Fprintln(os.Stderr, "NOT_FOUND: "+msg)
		} else {
			fmt.Fprintln(os.Stderr, msg)
		}
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
	var in inputSpec
	if err := json.Unmarshal(inBytes, &in); err != nil {
		return fmt.Errorf("bad json: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return fmt.Errorf("path is required")
	}
	// Enforce repo-relative paths: disallow absolute and path escape above CWD.
	if filepath.IsAbs(in.Path) {
		return fmt.Errorf("path must be relative to repository root")
	}
	clean := filepath.Clean(in.Path)
	if strings.HasPrefix(clean, "..") {
		return fmt.Errorf("path escapes repository root")
	}
	if in.OffsetBytes < 0 {
		return fmt.Errorf("offsetBytes must be >= 0")
	}
	// Open and stat to determine file size.
	f, err := os.Open(clean)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("NOT_FOUND: %s", clean)
		}
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}
	size := info.Size()

	// If offset beyond end, return empty content with eof=true.
	if in.OffsetBytes >= size {
		out := outputSpec{ContentBase64: "", SizeBytes: size, EOF: true}
		return writeJSON(out)
	}

	if _, err := f.Seek(in.OffsetBytes, io.SeekStart); err != nil {
		return fmt.Errorf("seek: %w", err)
	}

	// Determine how many bytes to read.
	var toRead int64 = size - in.OffsetBytes
	if in.MaxBytes > 0 && in.MaxBytes < toRead {
		toRead = in.MaxBytes
	}
	if toRead < 0 {
		toRead = 0
	}

	// Read the requested range.
	buf := make([]byte, toRead)
	var readTotal int64
	for readTotal < toRead {
		n, rerr := f.Read(buf[readTotal:])
		if n > 0 {
			readTotal += int64(n)
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				break
			}
			return fmt.Errorf("read: %w", rerr)
		}
	}
	eof := in.OffsetBytes+readTotal >= size
	out := outputSpec{ContentBase64: base64.StdEncoding.EncodeToString(buf[:readTotal]), SizeBytes: size, EOF: eof}
	return writeJSON(out)
}

func writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	fmt.Println(string(b))
	return nil
}
