package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// writeAudit emits an NDJSON line capturing tool execution metadata.
func writeAudit(spec ToolSpec, start time.Time, exitCode, stdoutBytes, stderrBytes int, envKeys []string) {
	type auditEntry struct {
		TS          string   `json:"ts"`
		Tool        string   `json:"tool"`
		Argv        []string `json:"argv"`
		CWD         string   `json:"cwd"`
		Exit        int      `json:"exit"`
		MS          int64    `json:"ms"`
		StdoutBytes int      `json:"stdoutBytes"`
		StderrBytes int      `json:"stderrBytes"`
		Truncated   bool     `json:"truncated"`
		EnvKeys     []string `json:"envKeys,omitempty"`
	}

	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	entry := auditEntry{
		TS:          timeNow().UTC().Format(time.RFC3339Nano),
		Tool:        spec.Name,
		Argv:        redactSensitiveStrings(append([]string(nil), spec.Command...)),
		CWD:         redactSensitiveString(cwd),
		Exit:        exitCode,
		MS:          time.Since(start).Milliseconds(),
		StdoutBytes: stdoutBytes,
		StderrBytes: stderrBytes,
		Truncated:   false,
		EnvKeys:     append([]string(nil), envKeys...),
	}
	if err := appendAuditLog(entry); err != nil {
		_ = err
	}
}

// appendAuditLog writes an NDJSON audit line to .goagent/audit/YYYYMMDD.log under the repository root.
// The repository root is determined by walking upward from the current working directory
// until a directory containing go.mod is found. If no go.mod is found, falls back to CWD.
func appendAuditLog(entry any) error {
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	root := moduleRoot()
	dir := filepath.Join(root, ".goagent", "audit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	fname := timeNow().UTC().Format("20060102") + ".log"
	path := filepath.Join(dir, fname)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			_ = err
		}
	}()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// moduleRoot walks upward from the current working directory to locate the directory
// containing go.mod. If none is found, it returns the current working directory.
func moduleRoot() string {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		return "."
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root; fallback to original cwd
			return cwd
		}
		dir = parent
	}
}
