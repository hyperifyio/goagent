package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	aio "syscall"
)

// inputSpec models the stdin JSON contract for fs_write_file.
// {"path":"string","contentBase64":"string","createModeOctal?":"0644"}
// Path is repository-relative (no absolute paths, no .. escapes).
// The write must be atomic: write to a temp file in the same directory and rename.
// On success, print single-line JSON {"bytesWritten":int} to stdout and exit 0.
// On error, print a single line to stderr and exit non-zero. For missing parent
// directory, include marker MISSING_PARENT in the message for deterministic tests.

type inputSpec struct {
	Path            string `json:"path"`
	ContentBase64   string `json:"contentBase64"`
	CreateModeOctal string `json:"createModeOctal"`
}

type outputSpec struct {
	BytesWritten int `json:"bytesWritten"`
}

func main() {
	if err := run(); err != nil {
		msg := strings.TrimSpace(err.Error())
		if strings.Contains(strings.ToUpper(msg), "MISSING_PARENT") {
			fmt.Fprintln(os.Stderr, msg)
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
	if strings.TrimSpace(in.ContentBase64) == "" {
		return fmt.Errorf("contentBase64 is required")
	}
	if filepath.IsAbs(in.Path) {
		return fmt.Errorf("path must be relative to repository root")
	}
	clean := filepath.Clean(in.Path)
	if strings.HasPrefix(clean, "..") {
		return fmt.Errorf("path escapes repository root")
	}

	// Ensure parent directory exists
	dir := filepath.Dir(clean)
	if dir == "." || dir == "" {
		// relative path at repo root: ok
	} else {
		st, err := os.Stat(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("MISSING_PARENT: %s", dir)
			}
			return fmt.Errorf("stat parent: %w", err)
		}
		if !st.IsDir() {
			return fmt.Errorf("parent is not a directory: %s", dir)
		}
	}

	// Decode content
	data, err := base64.StdEncoding.DecodeString(in.ContentBase64)
	if err != nil {
		return fmt.Errorf("decode base64: %v", err)
	}

	// Determine file mode
	var mode os.FileMode = 0o644
	if info, err := os.Lstat(clean); err == nil {
		// Preserve existing permissions on overwrite
		mode = info.Mode().Perm()
	} else if errors.Is(err, os.ErrNotExist) {
		if strings.TrimSpace(in.CreateModeOctal) != "" {
			if parsed, perr := parseOctalPerm(in.CreateModeOctal); perr == nil {
				mode = parsed
			} else {
				return fmt.Errorf("bad createModeOctal: %v", perr)
			}
		}
	} else if err != nil {
		return fmt.Errorf("lstat target: %w", err)
	}

	// Write to temp file in same directory
	baseDir := dir
	if baseDir == "." || baseDir == "" {
		baseDir = "."
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		// If directory was supposed to exist, this is unexpected but report
		return fmt.Errorf("ensure dir: %w", err)
	}
	tmp, err := os.CreateTemp(baseDir, ".fswrite-*.")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	// Ensure restrictive mode during write; chmod later to desired mode
	_ = tmp.Chmod(0o600)

	written := 0
	if len(data) > 0 {
		n, werr := tmp.Write(data)
		if werr != nil {
			_ = tmp.Close()
			return fmt.Errorf("write temp: %w", werr)
		}
		written = n
	}
	// Fsync and close
	if ferr := tmp.Sync(); ferr != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsync temp: %w", ferr)
	}
	if cerr := tmp.Close(); cerr != nil {
		return fmt.Errorf("close temp: %w", cerr)
	}
	// Set mode on temp before rename
	if cherr := os.Chmod(tmpPath, mode); cherr != nil {
		return fmt.Errorf("chmod temp: %w", cherr)
	}

	// Rename atomically into place
	if rerr := os.Rename(tmpPath, clean); rerr != nil {
		// On cross-device link errors, attempt copy+rename fallback
		if errors.Is(rerr, aio.EXDEV) || strings.Contains(strings.ToLower(rerr.Error()), "cross-device") {
			if cerr := copyFile(tmpPath, clean, mode); cerr != nil {
				return fmt.Errorf("move into place: %w", cerr)
			}
			_ = os.Remove(tmpPath)
		} else {
			return fmt.Errorf("rename temp: %w", rerr)
		}
	}

	out := outputSpec{BytesWritten: written}
	b, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	fmt.Println(string(b))
	return nil
}

func parseOctalPerm(s string) (os.FileMode, error) {
	// Allow forms like "0644" or "644"
	ss := strings.TrimSpace(s)
	if ss == "" {
		return 0, fmt.Errorf("empty")
	}
	if strings.HasPrefix(ss, "0") {
		ss = ss[1:]
	}
	val, err := strconv.ParseUint(ss, 8, 32)
	if err != nil {
		return 0, err
	}
	return os.FileMode(val), nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sf.Close()
	df, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() { _ = df.Close() }()
	if _, err := io.Copy(df, sf); err != nil {
		return err
	}
	if err := df.Sync(); err != nil {
		return err
	}
	return nil
}
