package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// RunToolWithJSON executes the tool command with args JSON provided on stdin.
// Returns stdout bytes and an error if the command fails. The caller is responsible
// for mapping errors to deterministic JSON per product rules.
// timeNow is a package-level clock to enable deterministic tests.
// In production it defaults to time.Now.
var timeNow = time.Now

// computeToolTimeout derives the timeout for a tool execution, honoring
// spec.TimeoutSec when provided; otherwise it falls back to the default.
func computeToolTimeout(spec ToolSpec, defaultTimeout time.Duration) time.Duration {
	if spec.TimeoutSec > 0 {
		return time.Duration(spec.TimeoutSec) * time.Second
	}
	return defaultTimeout
}

// buildToolEnvironment constructs a minimal environment for the tool process
// and returns the environment slice along with the list of env keys that were
// passed through (for audit visibility).
func buildToolEnvironment(spec ToolSpec) (env []string, passedKeys []string) {
	if v := os.Getenv("PATH"); v != "" {
		env = append(env, "PATH="+v)
	}
	if v := os.Getenv("HOME"); v != "" {
		env = append(env, "HOME="+v)
	}
	if len(spec.EnvPassthrough) > 0 {
		for _, key := range spec.EnvPassthrough {
			if val, ok := os.LookupEnv(key); ok {
				env = append(env, key+"="+val)
				passedKeys = append(passedKeys, key)
			}
		}
	}
	return env, passedKeys
}

// normalizeWaitError maps timeout and process errors to deterministic errors.
func normalizeWaitError(ctx context.Context, waitErr error, stderrText string) error {
	if ctx.Err() == context.DeadlineExceeded {
		return errors.New("tool timed out")
	}
	if waitErr != nil {
		msg := stderrText
		if msg == "" {
			msg = waitErr.Error()
		}
		return errors.New(msg)
	}
	return nil
}

func RunToolWithJSON(parentCtx context.Context, spec ToolSpec, jsonInput []byte, defaultTimeout time.Duration) ([]byte, error) {
	start := time.Now()
	// Derive timeout, honoring per-tool override when provided.
	to := computeToolTimeout(spec, defaultTimeout)
	ctx, cancel := context.WithTimeout(parentCtx, to)
	defer cancel()

	cmd := exec.CommandContext(ctx, spec.Command[0], spec.Command[1:]...)
	// Build minimal environment and record passed-through keys for audit.
	env, passedKeys := buildToolEnvironment(spec)
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	// Write JSON to stdin
	if len(jsonInput) == 0 {
		jsonInput = []byte("{}")
	}
	if _, err := stdin.Write(jsonInput); err != nil {
		return nil, fmt.Errorf("write stdin: %w", err)
	}
	// Best-effort close; log failure to audit but do not fail run
	if err := stdin.Close(); err != nil {
		// Capture the close error as a best-effort audit line
		if err2 := appendAuditLog(map[string]any{
			"ts":    timeNow().UTC().Format(time.RFC3339Nano),
			"event": "stdin_close_error",
			"tool":  spec.Name,
			"error": err.Error(),
		}); err2 != nil {
			_ = err2
		}
	}

	// Read stdout and stderr fully
	outCh := make(chan []byte, 1)
	errCh := make(chan []byte, 1)
	go func() { outCh <- safeReadAll(stdout) }()
	go func() { errCh <- safeReadAll(stderr) }()

	err = cmd.Wait()
	out := <-outCh
	serr := <-errCh

	exitCode := 0
	if err != nil {
		// Try to capture exit code when available
		if ee, ok := err.(*exec.ExitError); ok && ee.ProcessState != nil {
			exitCode = ee.ProcessState.ExitCode()
		} else {
			// Unknown exit (e.g., timeout/cancel)
			exitCode = -1
		}
	}
	// Best-effort audit (failures do not affect tool result)
	writeAudit(spec, start, exitCode, len(out), len(serr), passedKeys)

	if normErr := normalizeWaitError(ctx, err, string(serr)); normErr != nil {
		return nil, normErr
	}
	return out, nil
}

// safeReadAll reads all bytes from r; on error it returns any bytes read so far (or nil).
func safeReadAll(r io.Reader) []byte {
	b, err := io.ReadAll(r)
	if err != nil {
		return b
	}
	return b
}

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

// redactSensitiveStrings applies redactSensitiveString to each element and returns a new slice.
func redactSensitiveStrings(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = redactSensitiveString(v)
	}
	return out
}

// redactSensitiveString masks occurrences of configured sensitive patterns and known secret env values.
// Patterns are sourced from GOAGENT_REDACT (comma/semicolon-separated substrings or regexes).
// Additionally, values of well-known secret env vars (OAI_API_KEY, OPENAI_API_KEY) are masked if present.
func redactSensitiveString(s string) string {
	if s == "" {
		return s
	}
	// Collect patterns
	patterns := gatherRedactionPatterns()
	// Apply regex replacements first
	for _, rx := range patterns.regexps {
		s = rx.ReplaceAllString(s, "***REDACTED***")
	}
	// Apply literal value masking
	for _, lit := range patterns.literals {
		if lit == "" {
			continue
		}
		s = strings.ReplaceAll(s, lit, "***REDACTED***")
	}
	return s
}

type redactionPatterns struct {
	regexps  []*regexp.Regexp
	literals []string
}

// gatherRedactionPatterns builds redaction patterns from environment.
// GOAGENT_REDACT may contain comma/semicolon separated regex patterns or literals.
// Known secret env values are added as literal masks.
func gatherRedactionPatterns() redactionPatterns {
	var pats redactionPatterns
	// Configurable patterns
	cfg := os.Getenv("GOAGENT_REDACT")
	if cfg != "" {
		// split by comma or semicolon
		fields := strings.FieldsFunc(cfg, func(r rune) bool { return r == ',' || r == ';' })
		for _, f := range fields {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			// Try to compile as regex; if it fails, treat as literal
			if rx, err := regexp.Compile(f); err == nil {
				pats.regexps = append(pats.regexps, rx)
			} else {
				pats.literals = append(pats.literals, f)
			}
		}
	}
	// Known secret env values (mask exact substrings)
	for _, key := range []string{"OAI_API_KEY", "OPENAI_API_KEY"} {
		if v := os.Getenv(key); v != "" {
			pats.literals = append(pats.literals, v)
		}
	}
	return pats
}
