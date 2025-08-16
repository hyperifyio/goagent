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

func RunToolWithJSON(parentCtx context.Context, spec ToolSpec, jsonInput []byte, defaultTimeout time.Duration) ([]byte, error) {
	start := time.Now()
	// Derive timeout: spec.TimeoutSec overrides when >0
	to := defaultTimeout
	if spec.TimeoutSec > 0 {
		to = time.Duration(spec.TimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(parentCtx, to)
	defer cancel()

	cmd := exec.CommandContext(ctx, spec.Command[0], spec.Command[1:]...)
	// Scrub environment to a minimal allowlist: PATH and HOME only
	var env []string
	if v := os.Getenv("PATH"); v != "" {
		env = append(env, "PATH="+v)
	}
	if v := os.Getenv("HOME"); v != "" {
		env = append(env, "HOME="+v)
	}
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
	_ = stdin.Close()

	// Read stdout and stderr fully
	outCh := make(chan []byte, 1)
	errCh := make(chan []byte, 1)
	go func() { b, _ := io.ReadAll(stdout); outCh <- b }()
	go func() { b, _ := io.ReadAll(stderr); errCh <- b }()

	err = cmd.Wait()
	out := <-outCh
	serr := <-errCh

	// Prepare audit entry
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
	}

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
	cwd, _ := os.Getwd()
	// Redact sensitive values from argv and cwd before auditing
	redactedArgv := redactSensitiveStrings(append([]string(nil), spec.Command...))
	redactedCWD := redactSensitiveString(cwd)
	entry := auditEntry{
		TS:          timeNow().UTC().Format(time.RFC3339Nano),
		Tool:        spec.Name,
		Argv:        redactedArgv,
		CWD:         redactedCWD,
		Exit:        exitCode,
		MS:          time.Since(start).Milliseconds(),
		StdoutBytes: len(out),
		StderrBytes: len(serr),
		Truncated:   false,
	}
	// Best-effort append (failures do not affect tool result)
	_ = appendAuditLog(entry)

	if ctx.Err() == context.DeadlineExceeded {
		// Normalize timeout error to a deterministic string per product rules
		return nil, errors.New("tool timed out")
	}
	if err != nil {
		// Prefer stderr text when available for context
		msg := string(serr)
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return out, nil
}

// appendAuditLog writes an NDJSON audit line to .goagent/audit/YYYYMMDD.log
func appendAuditLog(entry any) error {
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	dir := filepath.Join(".goagent", "audit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	fname := timeNow().UTC().Format("20060102") + ".log"
	path := filepath.Join(dir, fname)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
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
