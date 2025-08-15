package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
    "path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type listdirInput struct {
	Path          string   `json:"path"`
	Recursive     bool     `json:"recursive"`
	Globs         []string `json:"globs"`
	IncludeHidden bool     `json:"includeHidden"`
	MaxResults    int      `json:"maxResults"`
}

type listdirEntry struct {
	Path      string `json:"path"`
	Type      string `json:"type"`
	SizeBytes int64  `json:"sizeBytes"`
	ModeOctal string `json:"modeOctal"`
	ModTime   string `json:"modTime"`
}

type listdirOutput struct {
	Entries   []listdirEntry `json:"entries"`
	Truncated bool           `json:"truncated"`
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
	var in listdirInput
	if err := json.Unmarshal(inBytes, &in); err != nil {
		return fmt.Errorf("invalid json: %w", err)
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

	max := in.MaxResults
	if max <= 0 {
		max = int(^uint(0) >> 1)
	}

    var out listdirOutput
    hasGlobs := len(in.Globs) > 0

	// Choose traversal based on recursive flag
	if in.Recursive {
		// Walk subtree
        err = filepath.WalkDir(clean, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			// Skip hidden directories if includeHidden=false
			base := filepath.Base(p)
			if !in.IncludeHidden && strings.HasPrefix(base, ".") {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
            }
        // Skip the root itself; only include contents
        if p == clean {
				return nil
			}
            // Glob filtering (match against repo-relative slash path)
            if hasGlobs && !matchesAnyGlob(p, in.Globs) {
                return nil
            }
            if entry, ok := makeEntry(p, d); ok {
				out.Entries = append(out.Entries, entry)
				if len(out.Entries) >= max {
					out.Truncated = true
					return fmt.Errorf("truncated")
				}
			}
			return nil
		})
		if err != nil && err.Error() != "truncated" {
			// ignore non-fatal walk errors; they are skipped
		}
	} else {
		// Non-recursive: list direct children only
		entries, readErr := os.ReadDir(clean)
		if readErr != nil {
			return fmt.Errorf("read dir: %w", readErr)
		}
		// Stable ordering: dirs first, then others, lexicographic by path
		sort.SliceStable(entries, func(i, j int) bool {
			iIsDir := entries[i].IsDir()
			jIsDir := entries[j].IsDir()
			if iIsDir != jIsDir {
				return iIsDir && !jIsDir
			}
			return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
		})
		for _, d := range entries {
			name := d.Name()
			if !in.IncludeHidden && strings.HasPrefix(name, ".") {
				continue
			}
			p := filepath.Join(clean, name)
            if hasGlobs && !matchesAnyGlob(p, in.Globs) {
                continue
            }
			if entry, ok := makeEntry(p, d); ok {
				out.Entries = append(out.Entries, entry)
				if len(out.Entries) >= max {
					out.Truncated = true
					break
				}
			}
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("encode output: %w", err)
	}
	return nil
}

func toSlashRel(p string) string {
	if strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	return filepath.ToSlash(p)
}

// matchesAnyGlob reports whether path p (repo-relative) matches at least one
// of the provided glob patterns. Supports patterns like "**/*.go" by matching
// the basename against the suffix when prefixed with "**/" similar to fs_search.
func matchesAnyGlob(p string, patterns []string) bool {
    s := filepath.ToSlash(strings.TrimPrefix(p, "./"))
    for _, pat := range patterns {
        if pat == "" {
            continue
        }
        if strings.HasPrefix(pat, "**/") {
            rest := strings.TrimPrefix(pat, "**/")
            if ok, _ := path.Match(rest, path.Base(s)); ok {
                return true
            }
            continue
        }
        if ok, _ := path.Match(pat, s); ok {
            return true
        }
    }
    return false
}

func makeEntry(p string, d os.DirEntry) (listdirEntry, bool) {
	fi, err := d.Info()
	if err != nil {
		return listdirEntry{}, false
	}
	t := detectType(fi)
	mode := fi.Mode().Perm()
	return listdirEntry{
		Path:      toSlashRel(p),
		Type:      t,
		SizeBytes: sizeFor(fi),
		ModeOctal: fmt.Sprintf("%04o", uint32(mode)),
		ModTime:   fi.ModTime().UTC().Format(time.RFC3339),
	}, true
}

func detectType(fi os.FileInfo) string {
	m := fi.Mode()
	if m&os.ModeSymlink != 0 {
		return "symlink"
	}
	if m.IsDir() {
		return "dir"
	}
	if m.IsRegular() {
		return "file"
	}
	return "other"
}

func sizeFor(fi os.FileInfo) int64 {
	if fi.IsDir() {
		return 0
	}
	return fi.Size()
}
