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

type inputSpec struct {
	Query      string   `json:"query"`
	Regex      bool     `json:"regex"`
	Globs      []string `json:"globs"`
	MaxResults int      `json:"maxResults"`
}

type match struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Preview string `json:"preview"`
}

type outputSpec struct {
	Matches   []match `json:"matches"`
	Truncated bool    `json:"truncated"`
}

func main() {
	if err := run(); err != nil {
		// Minimal JSON error contract on stderr
		enc := json.NewEncoder(os.Stderr)
		_ = enc.Encode(map[string]string{"error": err.Error()})
		os.Exit(1)
	}
}

func run() error {
	var in inputSpec
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		return fmt.Errorf("invalid json: %w", err)
	}
	if strings.TrimSpace(in.Query) == "" {
		return errors.New("query must be non-empty")
	}
	if in.MaxResults < 0 {
		in.MaxResults = 0
	}
	// Only literal search supported for now
	if in.Regex {
		return errors.New("regex not supported yet")
	}

	allowedExts := deriveAllowedExtensions(in.Globs)

	var (
		results   []match
		truncated bool
	)
	walkErr := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip .git directory and vendor-like dirs for speed
		if d.IsDir() {
			base := filepath.Base(path)
			if base == ".git" || base == ".hg" || base == ".svn" || base == "node_modules" || base == "bin" || base == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}

		if len(allowedExts) > 0 {
			ext := strings.ToLower(filepath.Ext(path))
			if _, ok := allowedExts[ext]; !ok {
				return nil
			}
		}

		fileMatches, stop, err := searchFileLiteral(path, in.Query, in.MaxResults, len(results))
		if err != nil {
			// Ignore unreadable files to keep tool robust
			return nil
		}
		results = append(results, fileMatches...)
		if stop {
			truncated = true
			return io.EOF // stop walking early
		}
		return nil
	})
	if walkErr != nil && walkErr != io.EOF {
		return walkErr
	}

	out := outputSpec{Matches: results, Truncated: truncated}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}

func deriveAllowedExtensions(globs []string) map[string]struct{} {
	allowed := make(map[string]struct{})
	for _, g := range globs {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		// Very small subset: patterns like "*.ext" or "**/*.ext"
		if strings.HasPrefix(g, "**/*.") || strings.HasPrefix(g, "*.") {
			idx := strings.LastIndex(g, ".")
			if idx >= 0 && idx < len(g)-1 {
				ext := "." + strings.ToLower(g[idx+1:])
				allowed[ext] = struct{}{}
			}
			continue
		}
		// Exact filename with extension
		if strings.Contains(g, ".") && !strings.ContainsAny(g, "[]?{}!") {
			ext := strings.ToLower(filepath.Ext(g))
			if ext != "" {
				allowed[ext] = struct{}{}
			}
		}
	}
	return allowed
}

func searchFileLiteral(path, query string, maxResults int, already int) ([]match, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	rel := path
	if strings.HasPrefix(rel, "./") {
		rel = strings.TrimPrefix(rel, "./")
	}

	scanner := bufio.NewScanner(f)
	// Increase buffer for long lines
	buf := make([]byte, 0, 256*1024)
	scanner.Buffer(buf, 10*1024*1024)
	lineNum := 0
	var matches []match
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		idx := 0
		for {
			pos := strings.Index(line[idx:], query)
			if pos == -1 {
				break
			}
			m := match{
				Path:    rel,
				Line:    lineNum,
				Col:     idx + pos + 1,
				Preview: line,
			}
			matches = append(matches, m)
			if maxResults > 0 && already+len(matches) >= maxResults {
				return matches, true, nil
			}
			idx = idx + pos + len(query)
			if idx >= len(line) {
				break
			}
		}
	}
	_ = scanner.Err()
	return matches, false, nil
}
