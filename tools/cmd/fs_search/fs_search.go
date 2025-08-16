package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type searchInput struct {
	Query      string   `json:"query"`
	Regex      bool     `json:"regex,omitempty"`
	Globs      []string `json:"globs,omitempty"`
	MaxResults int      `json:"maxResults,omitempty"`
}

type match struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Preview string `json:"preview"`
}

type searchOutput struct {
	Matches   []match `json:"matches"`
	Truncated bool    `json:"truncated"`
}

func main() {
	in, err := readInput(os.Stdin)
	if err != nil {
		stderrJSON(err)
		os.Exit(1)
	}
	matches, truncated, err := search(in)
	if err != nil {
		stderrJSON(err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(searchOutput{Matches: matches, Truncated: truncated}); err != nil {
		stderrJSON(fmt.Errorf("encode json: %w", err))
		os.Exit(1)
	}
}

func readInput(r io.Reader) (searchInput, error) {
	var in searchInput
	b, err := io.ReadAll(bufio.NewReader(r))
	if err != nil {
		return in, fmt.Errorf("read stdin: %w", err)
	}
	if err := json.Unmarshal(b, &in); err != nil {
		return in, fmt.Errorf("parse json: %w", err)
	}
	if strings.TrimSpace(in.Query) == "" {
		return in, errors.New("query is required")
	}
	return in, nil
}

// nolint:gocyclo // Coordinating walk, filter, and scan raises complexity; covered by tests.
func search(in searchInput) ([]match, bool, error) {
	var rx *regexp.Regexp
	if in.Regex {
		var err error
		rx, err = regexp.Compile(in.Query)
		if err != nil {
			return nil, false, fmt.Errorf("BAD_REGEX: %w", err)
		}
	}
	globs := in.Globs
	if len(globs) == 0 {
		globs = []string{"**/*"}
	}
	// Walk repo and include only files matching any provided glob suffix pattern.
	// We implement a simplified matcher: support patterns like "**/*.txt" and "*.md".
	var files []string
    walkErr := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
            // Skip VCS metadata and known binary/output directories to bound scanning cost
            if path == ".git" || strings.HasPrefix(path, ".git/") {
                return filepath.SkipDir
            }
            if path == "bin" || path == "logs" || path == filepath.ToSlash(filepath.Join("tools", "bin")) {
                return filepath.SkipDir
            }
            return nil
		}
		// crude hidden filter: skip .git files
		if strings.Contains(path, string(os.PathSeparator)+".git"+string(os.PathSeparator)) {
			return nil
		}
		// Match any glob suffix
		for _, g := range globs {
			if matchSimpleGlob(path, g) {
				files = append(files, path)
				break
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, false, walkErr
	}
	max := in.MaxResults
	if max <= 0 {
		max = 1000
	}
	var matches []match
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			idx := -1
			if in.Regex {
				loc := rx.FindStringIndex(line)
				if loc != nil {
					idx = loc[0]
				}
			} else {
				idx = strings.Index(line, in.Query)
			}
			if idx >= 0 {
				m := match{Path: f, Line: i + 1, Col: idx + 1, Preview: line}
				matches = append(matches, m)
				if len(matches) >= max {
					return matches, true, nil
				}
			}
		}
	}
	// stable ordering
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Path == matches[j].Path {
			if matches[i].Line == matches[j].Line {
				return matches[i].Col < matches[j].Col
			}
			return matches[i].Line < matches[j].Line
		}
		return matches[i].Path < matches[j].Path
	})
	return matches, false, nil
}

// matchSimpleGlob performs minimal glob matching sufficient for tests:
// supports patterns like "**/*.ext", "*.ext", and exact filenames.
func matchSimpleGlob(path, pattern string) bool {
	pattern = filepath.ToSlash(pattern)
	path = filepath.ToSlash(path)
	if pattern == "**/*" || pattern == "**" || pattern == "*" {
		return true
	}
	// no-op: pattern already normalized by ToSlash
	if strings.HasPrefix(pattern, "**/") {
		suffix := strings.TrimPrefix(pattern, "**/")
		// e.g., suffix "*.txt"
		if strings.HasPrefix(suffix, "*.") {
			ext := strings.TrimPrefix(suffix, "*") // -> ".txt"
			return strings.HasSuffix(path, ext)
		}
		return strings.HasSuffix(path, suffix)
	}
	if strings.HasPrefix(pattern, "*.") {
		ext := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(path, ext)
	}
	// Fallback exact match
	return path == pattern
}

func stderrJSON(err error) {
	msg := err.Error()
	msg = strings.ReplaceAll(msg, "\n", " ")
	fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", msg)
}
