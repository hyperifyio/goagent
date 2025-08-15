package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Input schema for fs_read_lines
// {"path":"string","startLine":int,"endLine":int,"maxBytes?":int}
// Lines are 1-based. endLine is exclusive. Newlines in output are LF (\n) normalized.
// Path must be repo-relative (no absolute, no parent escape).

type readLinesInput struct {
	Path      string `json:"path"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	MaxBytes  int    `json:"maxBytes"`
}

type readLinesOutput struct {
	Content   string `json:"content"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	EOF       bool   `json:"eof"`
}

func main() {
	if err := run(); err != nil {
		// Error contract: concise message on stderr; exit non-zero
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
		return errors.New("missing json input")
	}
	var in readLinesInput
	if err := json.Unmarshal(inBytes, &in); err != nil {
		return fmt.Errorf("bad json: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return errors.New("path is required")
	}
	if in.StartLine <= 0 {
		return errors.New("startLine must be >= 1")
	}
	if in.EndLine < in.StartLine {
		return errors.New("endLine must be >= startLine")
	}
	if filepath.IsAbs(in.Path) {
		return errors.New("path must be relative to repository root")
	}
	clean := filepath.Clean(in.Path)
	if strings.HasPrefix(clean, "..") {
		return errors.New("path escapes repository root")
	}

	f, err := os.Open(clean)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("NOT_FOUND: %s", clean)
		}
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

    reader := bufio.NewReader(f)
    var (
        lineNum         = 0
        builder         strings.Builder
        reachedFileEOI  bool
        truncatedByMax  bool
        maxBytesAllowed = in.MaxBytes
    )

	// Read lines incrementally; normalize CRLF -> LF; preserve trailing LF if present
	for {
        line, err := reader.ReadString('\n')
		if errors.Is(err, io.EOF) {
			if len(line) == 0 {
				reachedFileEOI = true
				break
			}
			// Handle last line without trailing newline
			lineNum++
            if lineNum >= in.StartLine && lineNum < in.EndLine {
                chunk := normalizeEOL(line, false)
                if maxBytesAllowed > 0 {
                    remaining := maxBytesAllowed - builder.Len()
                    if remaining <= 0 {
                        truncatedByMax = true
                        break
                    }
                    if len(chunk) > remaining {
                        builder.WriteString(chunk[:remaining])
                        truncatedByMax = true
                        break
                    }
                }
                builder.WriteString(chunk)
            }
			reachedFileEOI = true
			break
		}
		if err != nil {
			return fmt.Errorf("read line: %w", err)
		}
		lineNum++
        if lineNum >= in.StartLine && lineNum < in.EndLine {
            chunk := normalizeEOL(line, true)
            if maxBytesAllowed > 0 {
                remaining := maxBytesAllowed - builder.Len()
                if remaining <= 0 {
                    truncatedByMax = true
                    break
                }
                if len(chunk) > remaining {
                    builder.WriteString(chunk[:remaining])
                    truncatedByMax = true
                    break
                }
            }
            builder.WriteString(chunk)
        }
		if lineNum >= in.EndLine {
			// We can stop early once we've read the last requested line
			break
		}
	}

	out := readLinesOutput{
		Content:   builder.String(),
		StartLine: in.StartLine,
		EndLine:   in.EndLine,
        // EOF indicates we reached end-of-file before covering [startLine:endLine).
        // If we truncated due to maxBytes, do not claim EOF.
        EOF:       !truncatedByMax && reachedFileEOI && lineNum < in.EndLine,
	}
	b, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	fmt.Println(string(b))
	return nil
}

func normalizeEOL(s string, hadLF bool) string {
	// If s ends with CRLF, convert to LF; if it ends with LF alone, keep LF.
	if strings.HasSuffix(s, "\r\n") {
		return strings.TrimSuffix(s, "\r\n") + "\n"
	}
	if hadLF {
		// s ends with \n (and not CR before it)
		return s
	}
	// No trailing newline; return as-is (do not append newline)
	return s
}
