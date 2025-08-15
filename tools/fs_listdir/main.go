package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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
	Type      string `json:"type"` // file|dir|symlink|other
	SizeBytes int64  `json:"sizeBytes"`
	ModeOctal string `json:"modeOctal"`
	ModTime   string `json:"modTime"` // RFC3339
}

type listdirOutput struct {
	Entries   []listdirEntry `json:"entries"`
	Truncated bool           `json:"truncated"`
}

func main() {
	if err := run(); err != nil {
		// Error contract: concise single line to stderr, non-zero exit
		msg := strings.TrimSpace(err.Error())
		fmt.Fprintln(os.Stderr, msg)
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
	var in listdirInput
	if err := json.Unmarshal(inBytes, &in); err != nil {
		return fmt.Errorf("bad json: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return fmt.Errorf("path is required")
	}
	if filepath.IsAbs(in.Path) {
		return fmt.Errorf("path must be relative to repository root")
	}
	root := filepath.Clean(in.Path)
	if strings.HasPrefix(root, "..") {
		return fmt.Errorf("path escapes repository root")
	}

	// Default MaxResults safeguard
	if in.MaxResults <= 0 {
		in.MaxResults = 10000
	}

	var results []listdirEntry
	truncated := false

	// Helper to append entry respecting max results
	appendEntry := func(entry listdirEntry) {
		if len(results) >= in.MaxResults {
			truncated = true
			return
		}
		results = append(results, entry)
	}

	// Non-recursive: only list immediate children of root
	if !in.Recursive {
		entries, err := os.ReadDir(root)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("NOT_FOUND: %s", root)
			}
			return fmt.Errorf("readdir: %w", err)
		}
		for _, de := range entries {
			name := de.Name()
			if !in.IncludeHidden && strings.HasPrefix(name, ".") {
				continue
			}
			relPath := filepath.Join(root, name)
			// Lstat to avoid following symlinks
			info, err := os.Lstat(relPath)
			if err != nil {
				// Skip entries we cannot stat, but do not fail whole listing
				continue
			}
			entryType := classifyMode(info.Mode())
			// Prepare entry
			appendEntry(listdirEntry{
				Path:      relPath,
				Type:      entryType,
				SizeBytes: fileSizeForMode(info),
				ModeOctal: fmt.Sprintf("%#04o", uint32(info.Mode().Perm())),
				ModTime:   info.ModTime().UTC().Format(time.RFC3339),
			})
			if truncated {
				break
			}
		}
		sortEntries(results)
		return writeJSON(listdirOutput{Entries: results, Truncated: truncated})
	}

	// Recursive: Walk the tree
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Skip visit errors
			return nil
		}
		// Skip the root itself
		if path == root {
			return nil
		}
		name := filepath.Base(path)
		if !in.IncludeHidden && strings.HasPrefix(name, ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return nil
		}
		appendEntry(listdirEntry{
			Path:      path,
			Type:      classifyMode(info.Mode()),
			SizeBytes: fileSizeForMode(info),
			ModeOctal: fmt.Sprintf("%#04o", uint32(info.Mode().Perm())),
			ModTime:   info.ModTime().UTC().Format(time.RFC3339),
		})
		if truncated {
			return io.EOF // early stop WalkDir
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("walk: %w", err)
	}
	sortEntries(results)
	return writeJSON(listdirOutput{Entries: results, Truncated: truncated})
}

func writeJSON(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	fmt.Println(string(b))
	return nil
}

func classifyMode(m os.FileMode) string {
	switch {
	case m&os.ModeSymlink != 0:
		return "symlink"
	case m.IsDir():
		return "dir"
	case m.IsRegular():
		return "file"
	default:
		return "other"
	}
}

func sortEntries(entries []listdirEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		// Dirs first, then files/symlinks/others; within same type lexicographic by path
		rank := func(t string) int {
			switch t {
			case "dir":
				return 0
			case "file":
				return 1
			case "symlink":
				return 2
			default:
				return 3
			}
		}
		ri, rj := rank(entries[i].Type), rank(entries[j].Type)
		if ri != rj {
			return ri < rj
		}
		return entries[i].Path < entries[j].Path
	})
}

func fileSizeForMode(info os.FileInfo) int64 {
	// For non-regular files, size is reported by FileInfo but may not be meaningful; keep as-is
	return info.Size()
}
