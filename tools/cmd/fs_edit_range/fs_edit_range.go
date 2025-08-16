package main

import (
	"bufio"
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
	"sync"
)

type editInput struct {
	Path             string `json:"path"`
	StartByte        int    `json:"startByte"`
	EndByte          int    `json:"endByte"`
	ReplacementBase64 string `json:"replacementBase64"`
	ExpectedSha256   string `json:"expectedSha256,omitempty"`
}

type editOutput struct {
	BytesReplaced int    `json:"bytesReplaced"`
	NewSha256     string `json:"newSha256"`
}

var editLocks sync.Map

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
	out, err := applyEdit(in)
	if err != nil {
		stderrJSON(err)
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(out)
}

func readInput(r io.Reader) (editInput, error) {
	var in editInput
	b, err := io.ReadAll(bufio.NewReader(r))
	if err != nil {
		return in, fmt.Errorf("read stdin: %w", err)
	}
	if err := json.Unmarshal(b, &in); err != nil {
		return in, fmt.Errorf("parse json: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		return in, errors.New("path is required")
	}
	if in.StartByte < 0 || in.EndByte < in.StartByte {
		return in, errors.New("invalid range")
	}
	if _, err := base64.StdEncoding.DecodeString(in.ReplacementBase64); err != nil {
		return in, fmt.Errorf("BAD_BASE64: %w", err)
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

func applyEdit(in editInput) (editOutput, error) {
	muIface, _ := editLocks.LoadOrStore(in.Path, &sync.Mutex{})
	mu := muIface.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	data, err := os.ReadFile(in.Path)
	if err != nil {
		return editOutput{}, err
	}
	replacement, _ := base64.StdEncoding.DecodeString(in.ReplacementBase64)
	if in.StartByte > len(data) {
		in.StartByte = len(data)
	}
	if in.EndByte > len(data) {
		in.EndByte = len(data)
	}
	newData := append(append(append([]byte{}, data[:in.StartByte]...), replacement...), data[in.EndByte:]...)
	if in.ExpectedSha256 != "" {
		h := sha256.Sum256(newData)
		if hex.EncodeToString(h[:]) != strings.ToLower(in.ExpectedSha256) {
			return editOutput{}, errors.New("SHA_MISMATCH")
		}
	}
	if err := os.WriteFile(in.Path, newData, 0o644); err != nil {
		return editOutput{}, err
	}
	h := sha256.Sum256(newData)
	return editOutput{BytesReplaced: len(replacement), NewSha256: hex.EncodeToString(h[:])}, nil
}

func stderrJSON(err error) {
	msg := err.Error()
	msg = strings.ReplaceAll(msg, "\n", " ")
	fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", msg)
}
