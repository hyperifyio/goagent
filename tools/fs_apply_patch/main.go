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

type applyInput struct {
	UnifiedDiff string `json:"unifiedDiff"`
	DryRun      bool   `json:"dryRun,omitempty"`
}

type applyOutput struct {
	FilesChanged int `json:"filesChanged"`
}

func main() {
	in, err := readInput(os.Stdin)
	if err != nil {
		stderrJSON(err)
		os.Exit(1)
	}
	if strings.TrimSpace(in.UnifiedDiff) == "" {
		stderrJSON(errors.New("unifiedDiff is required"))
		os.Exit(1)
	}
	// Minimal implementation to create a new file per S02 for clean new-file apply
	changed, err := applyNewFileOnly(in.UnifiedDiff, in.DryRun)
	if err != nil {
		stderrJSON(err)
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(applyOutput{FilesChanged: changed})
}

func readInput(r io.Reader) (applyInput, error) {
	var in applyInput
	b, err := io.ReadAll(bufio.NewReader(r))
	if err != nil {
		return in, fmt.Errorf("read stdin: %w", err)
	}
	if err := json.Unmarshal(b, &in); err != nil {
		return in, fmt.Errorf("parse json: %w", err)
	}
	return in, nil
}

// applyNewFileOnly parses a unified diff that creates exactly one new file and applies it.
func applyNewFileOnly(diff string, dryRun bool) (int, error) {
	// Expect minimal format:
	// --- /dev/null
	// +++ b/<path>
	// @@ ...
	// <content lines starting with +>
	lines := strings.Split(diff, "\n")
	if len(lines) < 3 {
		return 0, errors.New("BAD_DIFF: too short")
	}
	var path string
	seenOld := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, "--- ") {
			if !strings.Contains(ln, "/dev/null") {
				return 0, errors.New("BAD_DIFF: old file must be /dev/null")
			}
			seenOld = true
			continue
		}
		if strings.HasPrefix(ln, "+++ ") {
			if !seenOld {
				return 0, errors.New("BAD_DIFF: missing old file header")
			}
			p := strings.TrimSpace(strings.TrimPrefix(ln, "+++ "))
			if strings.HasPrefix(p, "b/") {
				p = strings.TrimPrefix(p, "b/")
			}
			path = p
			break
		}
	}
	if path == "" {
		return 0, errors.New("BAD_DIFF: missing new file path")
	}
	if err := validateRelPath(path); err != nil {
		return 0, err
	}
    var content strings.Builder
    // Collect added lines exactly; do not add extra blank lines
    for _, ln := range lines {
        if strings.HasPrefix(ln, "+") && !strings.HasPrefix(ln, "+++") {
            s := strings.TrimPrefix(ln, "+")
            s = strings.ReplaceAll(s, "\r\n", "\n")
            s = strings.ReplaceAll(s, "\r", "\n")
            if strings.HasSuffix(s, "\n") {
                content.WriteString(s)
            } else {
                content.WriteString(s)
                content.WriteString("\n")
            }
        }
    }
    // Dry run: report number of files that would change (1 if create, 0 if identical exists)
    if dryRun {
        if existing, err := os.ReadFile(path); err == nil {
            if string(existing) == content.String() {
                return 0, nil
            }
        }
        return 1, nil
    }
    if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}
    // Idempotence and conflict
    if existing, err := os.ReadFile(path); err == nil {
        if string(existing) == content.String() {
            return 0, nil
        }
        return 0, errors.New("target exists with different content")
    }
    if err := os.WriteFile(path, []byte(content.String()), 0o644); err != nil {
		return 0, err
	}
	return 1, nil
}

func validateRelPath(p string) error {
	if filepath.IsAbs(p) {
		return fmt.Errorf("ABSOLUTE_PATH: %s", p)
	}
	clean := filepath.ToSlash(filepath.Clean(p))
	if strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return fmt.Errorf("PATH_ESCAPE: %s", p)
	}
	return nil
}

func stderrJSON(err error) {
	msg := err.Error()
	msg = strings.ReplaceAll(msg, "\n", " ")
	fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", msg)
}
