package oai

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func randomSuffix(t *testing.T) string {
	t.Helper()
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func readAuditLinesFromRoot(t *testing.T) []map[string]any {
	t.Helper()
	root := moduleRoot()
	path := filepath.Join(root, ".goagent", "audit", time.Now().UTC().Format("20060102")+".log")
	f, err := os.Open(path)
	if err != nil {
		// When the file does not exist yet, return empty slice
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		t.Fatalf("open audit log: %v", err)
	}
	defer func() { _ = f.Close() }()
	s := bufio.NewScanner(f)
	var out []map[string]any
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err == nil {
			out = append(out, m)
		}
	}
	return out
}

func findAudit(t *testing.T, pred func(map[string]any) bool) map[string]any {
	t.Helper()
	lines := readAuditLinesFromRoot(t)
	for i := len(lines) - 1; i >= 0; i-- { // scan backwards to find newest first
		if pred(lines[i]) {
			return lines[i]
		}
	}
	return nil
}

func TestWithAuditStage_AndAuditStageFromContext(t *testing.T) {
	ctx := WithAuditStage(nil, "  ")
	if got := auditStageFromContext(ctx); got != "" {
		t.Fatalf("expected empty stage on blanks, got %q", got)
	}
	ctx = WithAuditStage(nil, "prep")
	if got := auditStageFromContext(ctx); got != "prep" {
		t.Fatalf("expected stage prep, got %q", got)
	}
}

func TestTruncate_Bounds(t *testing.T) {
	if got := truncate("abc", 5); got != "abc" {
		t.Fatalf("expected passthrough, got %q", got)
	}
	long := strings.Repeat("x", 600)
	got := truncate(long, 500)
	if len(got) != 500 {
		t.Fatalf("expected 500 chars, got %d", len(got))
	}
}

func TestLogHTTPAttempt_WritesEntry_WithUniqueStage(t *testing.T) {
	stage := "teststage-" + randomSuffix(t)
	logHTTPAttempt(stage, "idem-123", 2, 5, 429, 1234, "https://api.example/chat/completions", strings.Repeat("e", 700))
	entry := findAudit(t, func(m map[string]any) bool {
		return m["event"] == "http_attempt" && m["stage"] == stage
	})
	if entry == nil {
		t.Fatalf("did not find http_attempt entry with stage %q", stage)
	}
	if entry["attempt"].(float64) != 2 || entry["max"].(float64) != 5 || entry["status"].(float64) != 429 {
		t.Fatalf("unexpected numeric fields: %+v", entry)
	}
	if v, ok := entry["error"].(string); !ok || len(v) > 500 {
		t.Fatalf("expected truncated error <=500, got len=%d", len(v))
	}
}

func TestLogHTTPTiming_WritesEntry_WithDurations(t *testing.T) {
	stage := "timing-" + randomSuffix(t)
	start := time.Now().Add(-2 * time.Second)
	dns := 20 * time.Millisecond
	conn := 30 * time.Millisecond
	wrote := start.Add(100 * time.Millisecond)
	first := wrote.Add(200 * time.Millisecond)
	end := first.Add(300 * time.Millisecond)
	logHTTPTiming(stage, "idem-xyz", 1, "https://api.example/chat/completions", 200, start, dns, conn, 0, wrote, first, end, "success", "")
	entry := findAudit(t, func(m map[string]any) bool {
		return m["event"] == "http_timing" && m["stage"] == stage
	})
	if entry == nil {
		t.Fatalf("did not find http_timing entry with stage %q", stage)
	}
	// sanity: totalMs should be > 0 and cause == success
	if total, ok := entry["totalMs"].(float64); !ok || total <= 0 {
		t.Fatalf("expected positive totalMs, got %v", entry["totalMs"])
	}
	if entry["cause"] != "success" {
		t.Fatalf("expected cause=success, got %v", entry["cause"])
	}
}

func TestLogLengthBackoff_WritesEntry(t *testing.T) {
	model := "mdl-" + randomSuffix(t)
	LogLengthBackoff(model, 1024, 800, 8192, 5000)
	entry := findAudit(t, func(m map[string]any) bool {
		return m["event"] == "length_backoff" && m["model"] == model
	})
	if entry == nil {
		t.Fatalf("did not find length_backoff for model %q", model)
	}
}

func TestEmitChatMetaAudit_WritesEntry(t *testing.T) {
	model := "gpt-5-" + randomSuffix(t)
	temp := 0.7
	req := ChatCompletionsRequest{Model: model, Temperature: &temp}
	emitChatMetaAudit(req)
	entry := findAudit(t, func(m map[string]any) bool {
		return m["event"] == "chat_meta" && m["model"] == model
	})
	if entry == nil {
		t.Fatalf("did not find chat_meta for model %q", model)
	}
	if _, ok := entry["temperature_effective"]; !ok {
		t.Fatalf("expected temperature_effective present")
	}
}
