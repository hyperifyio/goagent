package main

import (
    "bufio"
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strings"
)

type readLinesInput struct {
	Path      string `json:"path"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	MaxBytes  int    `json:"maxBytes,omitempty"`
}

type readLinesOutput struct {
	Content   string `json:"content"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	EOF       bool   `json:"eof"`
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
	content, eof, err := readRange(in.Path, in.StartLine, in.EndLine, in.MaxBytes)
	if err != nil {
		stderrJSON(err)
		os.Exit(1)
	}
    if err := json.NewEncoder(os.Stdout).Encode(readLinesOutput{
        Content:   content,
        StartLine: in.StartLine,
        EndLine:   in.EndLine,
        EOF:       eof,
    }); err != nil {
        stderrJSON(fmt.Errorf("encode json: %w", err))
        os.Exit(1)
    }
}

func readInput(r io.Reader) (readLinesInput, error) {
	var in readLinesInput
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
	if in.StartLine < 1 || in.EndLine < in.StartLine {
		return in, fmt.Errorf("invalid range")
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

func readRange(path string, start, end, maxBytes int) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	// Normalize CRLF to LF
	norm := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	lines := bytes.Split(norm, []byte("\n"))
	// lines includes an empty last element if file ends with LF; that is fine
	idxStart := start - 1
	idxEnd := end - 1 // exclusive line index in zero-based terms for slicing content
	if idxStart < 0 {
		idxStart = 0
	}
	if idxEnd > len(lines) {
		idxEnd = len(lines)
	}
	selected := lines[idxStart:idxEnd]
	content := bytes.Join(selected, []byte("\n"))
	if len(selected) > 0 && idxEnd <= len(lines) {
		// When slicing between lines, add trailing LF
		content = append(content, '\n')
	}
	if maxBytes > 0 && len(content) > maxBytes {
		content = content[:maxBytes]
	}
	// EOF is true only if end exceeds file end
	eof := end > len(lines)
	return string(content), eof, nil
}

func stderrJSON(err error) {
	msg := err.Error()
	msg = strings.ReplaceAll(msg, "\n", " ")
	fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", msg)
}
