package oai

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// audit context keys are unexported to avoid collisions. Use helper to set.
type auditCtxKey string

const (
	auditCtxKeyStage auditCtxKey = "audit_stage"
)

// WithAuditStage returns a child context that carries an audit stage label
// (e.g., "prep") that will be included in HTTP audit entries.
func WithAuditStage(parent context.Context, stage string) context.Context {
	stage = strings.TrimSpace(stage)
	if stage == "" {
		return parent
	}
    // Ensure a non-nil parent context to avoid panic in context.WithValue
    if parent == nil {
        parent = context.Background()
    }
    return context.WithValue(parent, auditCtxKeyStage, stage)
}

func auditStageFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v := ctx.Value(auditCtxKeyStage); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// logHTTPAttempt appends an NDJSON line describing an HTTP attempt and planned backoff.
func logHTTPAttempt(stage, idemKey string, attempt, maxAttempts, status int, backoffMs int64, endpoint, errStr string) {
	type audit struct {
		TS             string `json:"ts"`
		Event          string `json:"event"`
		Stage          string `json:"stage,omitempty"`
		IdempotencyKey string `json:"idempotency_key,omitempty"`
		Attempt        int    `json:"attempt"`
		Max            int    `json:"max"`
		Status         int    `json:"status"`
		BackoffMs      int64  `json:"backoffMs"`
		Endpoint       string `json:"endpoint"`
		Error          string `json:"error,omitempty"`
	}
	entry := audit{
		TS:             time.Now().UTC().Format(time.RFC3339Nano),
		Event:          "http_attempt",
		Stage:          stage,
		IdempotencyKey: idemKey,
		Attempt:        attempt,
		Max:            maxAttempts,
		Status:         status,
		BackoffMs:      backoffMs,
		Endpoint:       endpoint,
		Error:          truncate(errStr, 500),
	}
	if err := appendAuditLog(entry); err != nil {
		_ = err
	}
}

// logHTTPTiming appends detailed HTTP timing metrics to the audit log.
func logHTTPTiming(stage, idemKey string, attempt int, endpoint string, status int, start time.Time, dnsDur, connDur, tlsDur time.Duration, wroteAt, firstByteAt, end time.Time, cause, hint string) {
	type timing struct {
		TS             string `json:"ts"`
		Event          string `json:"event"`
		Stage          string `json:"stage,omitempty"`
		IdempotencyKey string `json:"idempotency_key,omitempty"`
		Attempt        int    `json:"attempt"`
		Endpoint       string `json:"endpoint"`
		Status         int    `json:"status"`
		DNSMs          int64  `json:"dnsMs"`
		ConnectMs      int64  `json:"connectMs"`
		TLSMs          int64  `json:"tlsMs"`
		WroteMs        int64  `json:"wroteMs"`
		TTFBMs         int64  `json:"ttfbMs"`
		ReadMs         int64  `json:"readMs"`
		TotalMs        int64  `json:"totalMs"`
		Cause          string `json:"cause"`
		Hint           string `json:"hint,omitempty"`
	}
	var wroteMs, ttfbMs, readMs int64
	if !wroteAt.IsZero() {
		wroteMs = wroteAt.Sub(start).Milliseconds()
	}
	if !firstByteAt.IsZero() {
		if !wroteAt.IsZero() && firstByteAt.After(wroteAt) {
			ttfbMs = firstByteAt.Sub(wroteAt).Milliseconds()
		} else {
			ttfbMs = firstByteAt.Sub(start).Milliseconds()
		}
		if end.After(firstByteAt) {
			readMs = end.Sub(firstByteAt).Milliseconds()
		}
	}
	entry := timing{
		TS:             time.Now().UTC().Format(time.RFC3339Nano),
		Event:          "http_timing",
		Stage:          stage,
		IdempotencyKey: idemKey,
		Attempt:        attempt,
		Endpoint:       endpoint,
		Status:         status,
		DNSMs:          dnsDur.Milliseconds(),
		ConnectMs:      connDur.Milliseconds(),
		TLSMs:          tlsDur.Milliseconds(),
		WroteMs:        wroteMs,
		TTFBMs:         ttfbMs,
		ReadMs:         readMs,
		TotalMs:        end.Sub(start).Milliseconds(),
		Cause:          cause,
		Hint:           hint,
	}
	if err := appendAuditLog(entry); err != nil {
		_ = err
	}
}

// appendAuditLog writes an NDJSON audit line to .goagent/audit/YYYYMMDD.log (same location used by tool runner).
func appendAuditLog(entry any) error {
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	// Primary location under module root
	root := moduleRoot()
	if err := writeAuditLine(root, b); err != nil {
		return err
	}
	// Also mirror under current working directory to ease local tooling/tests
	if cwd, _ := os.Getwd(); cwd != root {
		_ = writeAuditLine(cwd, b)
	}
	return nil
}

func writeAuditLine(base string, line []byte) error {
	dir := filepath.Join(base, ".goagent", "audit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	fname := time.Now().UTC().Format("20060102") + ".log"
	path := filepath.Join(dir, fname)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(line, '\n')); err != nil {
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
