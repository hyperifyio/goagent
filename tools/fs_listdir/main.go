package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type listInput struct {
	Path           string   `json:"path"`
	Recursive      bool     `json:"recursive,omitempty"`
	Globs          []string `json:"globs,omitempty"`
	IncludeHidden  bool     `json:"includeHidden,omitempty"`
	MaxResults     int      `json:"maxResults,omitempty"`
}

type entry struct {
	Path      string `json:"path"`
	Type      string `json:"type"`
	SizeBytes int64  `json:"sizeBytes"`
	ModeOctal string `json:"modeOctal"`
	ModTime   string `json:"modTime"`
}

type listOutput struct {
	Entries   []entry `json:"entries"`
	Truncated bool    `json:"truncated"`
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
	out, err := list(in)
	if err != nil {
		stderrJSON(err)
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(out)
}

func readInput(r io.Reader) (listInput, error) {
	var in listInput
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

func list(in listInput) (listOutput, error) {
	var entries []entry
	wildcards := in.Globs
	if len(wildcards) == 0 {
		wildcards = []string{"**/*"}
	}
	max := in.MaxResults
	if max <= 0 {
		max = 10000
	}
	root := in.Path
	if root == "." {
		root = ""
	}
	visit := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		// Hidden filtering
		base := filepath.Base(path)
		if !in.IncludeHidden && strings.HasPrefix(base, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// Glob filtering (very simplified)
		if len(wildcards) > 0 {
			ok := false
			for _, g := range wildcards {
				if matchSimpleGlob(path, g) {
					ok = true
					break
				}
			}
			if !ok {
				if d.IsDir() {
					return nil
				}
				return nil
			}
		}
		info, _ := d.Info()
		mode := info.Mode()
		etype := "other"
		if d.IsDir() {
			etype = "dir"
		} else if mode&os.ModeSymlink != 0 {
			etype = "symlink"
		} else {
			etype = "file"
		}
		entries = append(entries, entry{
			Path:      path,
			Type:      etype,
			SizeBytes: info.Size(),
			ModeOctal: fmt.Sprintf("%04o", mode.Perm()),
			ModTime:   info.ModTime().UTC().Format("2006-01-02T15:04:05Z07:00"),
		})
		if len(entries) >= max {
			return io.EOF
		}
		return nil
	}
	if in.Recursive {
		_ = filepath.WalkDir(in.Path, visit)
	} else {
		de, err := os.ReadDir(in.Path)
		if err != nil {
			return listOutput{}, err
		}
		for _, d := range de {
			_ = visit(filepath.Join(in.Path, d.Name()), d, nil)
		}
	}
	// stable ordering: dirs first, then files, lexicographic
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Type == entries[j].Type {
			return entries[i].Path < entries[j].Path
		}
		if entries[i].Type == "dir" {
			return true
		}
		if entries[j].Type == "dir" {
			return false
		}
		return entries[i].Path < entries[j].Path
	})
	return listOutput{Entries: entries, Truncated: len(entries) >= max}, nil
}

func matchSimpleGlob(path, pattern string) bool {
	pattern = filepath.ToSlash(pattern)
	path = filepath.ToSlash(path)
	if pattern == "**/*" || pattern == "**" || pattern == "*" {
		return true
	}
	if strings.HasPrefix(pattern, "**/") {
		suffix := strings.TrimPrefix(pattern, "**/")
		if strings.HasPrefix(suffix, "*.") {
			ext := strings.TrimPrefix(suffix, "*")
			return strings.HasSuffix(path, ext)
		}
		return strings.HasSuffix(path, suffix)
	}
	if strings.HasPrefix(pattern, "*.") {
		ext := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(path, ext)
	}
	return path == pattern
}

func stderrJSON(err error) {
	msg := err.Error()
	msg = strings.ReplaceAll(msg, "\n", " ")
	fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", msg)
}
