package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type statInput struct {
	Path           string `json:"path"`
	FollowSymlinks bool   `json:"followSymlinks,omitempty"`
	Hash           string `json:"hash,omitempty"`
}

type statOutput struct {
	Exists    bool   `json:"exists"`
	Type      string `json:"type"`
	SizeBytes int64  `json:"sizeBytes"`
	ModeOctal string `json:"modeOctal"`
	ModTime   string `json:"modTime"`
	Sha256    string `json:"sha256,omitempty"`
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
	out, err := statPath(in)
	if err != nil {
		stderrJSON(err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		stderrJSON(fmt.Errorf("encode json: %w", err))
		os.Exit(1)
	}
}

func readInput(r io.Reader) (statInput, error) {
	var in statInput
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

func statPath(in statInput) (statOutput, error) {
	var out statOutput
	var fi os.FileInfo
	var err error
	if in.FollowSymlinks {
		fi, err = os.Stat(in.Path)
	} else {
		fi, err = os.Lstat(in.Path)
	}
	if err != nil {
		if os.IsNotExist(err) {
			return statOutput{Exists: false}, nil
		}
		return statOutput{}, err
	}
	mode := fi.Mode()
	typeStr := "other"
	if mode.IsRegular() {
		typeStr = "file"
	} else if mode.IsDir() {
		typeStr = "dir"
	} else if mode&os.ModeSymlink != 0 {
		typeStr = "symlink"
	}
	out = statOutput{
		Exists:    true,
		Type:      typeStr,
		SizeBytes: fi.Size(),
		ModeOctal: fmt.Sprintf("%04o", mode.Perm()),
		ModTime:   fi.ModTime().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if in.Hash == "sha256" && typeStr == "file" {
		data, err := os.ReadFile(in.Path)
		if err == nil {
			h := sha256.Sum256(data)
			out.Sha256 = hex.EncodeToString(h[:])
		}
	}
	return out, nil
}

func stderrJSON(err error) {
	msg := err.Error()
	msg = strings.ReplaceAll(msg, "\n", " ")
	fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", msg)
}
