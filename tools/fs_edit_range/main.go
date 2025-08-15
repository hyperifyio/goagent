package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type editRangeInput struct {
	Path              string `json:"path"`
	StartByte         int    `json:"startByte"`
	EndByte           int    `json:"endByte"`
	ReplacementBase64 string `json:"replacementBase64"`
	ExpectedSha256    string `json:"expectedSha256"`
}

type editRangeOutput struct {
	BytesReplaced int    `json:"bytesReplaced"`
	NewSha256     string `json:"newSha256"`
}

func main() {
	if err := run(); err != nil {
		// Emit a concise error on stderr and non-zero exit
		fmt.Fprintln(os.Stderr, strings.TrimSpace(err.Error()))
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
	var in editRangeInput
	if err := json.Unmarshal(inBytes, &in); err != nil {
		return fmt.Errorf("bad json: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return errors.New("path is required")
	}
	if filepath.IsAbs(in.Path) {
		return errors.New("path must be relative to repository root")
	}
	clean := filepath.Clean(in.Path)
	if strings.HasPrefix(clean, "..") {
		return errors.New("path escapes repository root")
	}
	if in.StartByte < 0 || in.EndByte < 0 {
		return errors.New("startByte and endByte must be non-negative")
	}
	if in.EndByte < in.StartByte {
		return errors.New("endByte must be >= startByte")
	}
    repl, err := base64.StdEncoding.DecodeString(in.ReplacementBase64)
	if err != nil {
		return errors.New("replacementBase64 must be valid base64")
	}
    // Serialize edits using a sidecar advisory lock file to survive renames
    lockPath := clean + ".lock"
    lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
    if err != nil {
        return fmt.Errorf("open lock: %w", err)
    }
    defer func() {
        _ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
        _ = lockFile.Close()
        _ = os.Remove(lockPath)
    }()
    if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
        return fmt.Errorf("flock: %w", err)
    }

    orig, err := os.ReadFile(clean)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	size := len(orig)
	if in.StartByte > size || in.EndByte > size {
		return errors.New("startByte/endByte out of range")
	}
	// Splice: [0:start) + repl + [end:]
	var newContent []byte
	newContent = append(newContent, orig[:in.StartByte]...)
	newContent = append(newContent, repl...)
	newContent = append(newContent, orig[in.EndByte:]...)

	// Compute SHA-256 of new content
	sum := sha256.Sum256(newContent)
	newHex := hex.EncodeToString(sum[:])
	if strings.TrimSpace(in.ExpectedSha256) != "" && !strings.EqualFold(in.ExpectedSha256, newHex) {
		return errors.New("expectedSha256 mismatch")
	}

	// Atomic write: temp file in same directory then rename
	dir := filepath.Dir(clean)
	base := filepath.Base(clean)
	tmp, err := os.CreateTemp(dir, base+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	// Ensure temp is cleaned on failure
	defer func() { _ = os.Remove(tmp.Name()) }()

	// Preserve original file mode if possible
	if info, statErr := os.Stat(clean); statErr == nil {
		_ = tmp.Chmod(info.Mode())
	}
	if _, err := tmp.Write(newContent); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmp.Name(), clean); err != nil {
		return fmt.Errorf("rename into place: %w", err)
	}
	// Best-effort directory sync
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}

	out := editRangeOutput{BytesReplaced: len(repl), NewSha256: newHex}
	b, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	fmt.Println(string(b))
	return nil
}
