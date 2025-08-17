package oai

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"
)

// waitForAuditFile polls the audit directory until a file appears or timeout elapses.
func waitForAuditFile(t *testing.T, auditDir string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		entries, err := os.ReadDir(auditDir)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					return filepath.Join(auditDir, e.Name())
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("audit log not created in %s", auditDir)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// findRepoRoot walks upward from CWD to locate the directory containing go.mod.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	start, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found from %s upward", start)
		}
		dir = parent
	}
}

// TestObservabilityTemperatureAudit encodes the desired behavior: we emit
// structured audit with temperature_effective and temperature_in_payload.
func TestObservabilityTemperatureAudit(t *testing.T) {
	// Clean audit dir at repo root
	root := findRepoRoot(t)
	_ = os.RemoveAll(filepath.Join(root, ".goagent"))

	// Fake server to accept request and return minimal success
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		// Read and discard, but ensure it is valid JSON
		var req ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		resp := ChatCompletionsResponse{Choices: []ChatCompletionsResponseChoice{{Message: Message{Role: RoleAssistant, Content: "ok"}}}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// Supported model and explicit temperature
	model := "oss-gpt-20b"
	temp := 0.7
	req := ChatCompletionsRequest{
		Model:    model,
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Temperature: &temp,
	}

	c := NewClientWithRetry(srv.URL, "", 3*time.Second, RetryPolicy{MaxRetries: 0})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := c.CreateChatCompletion(ctx, req); err != nil {
		t.Fatalf("call: %v", err)
	}

	// Locate today's audit file and read it
	auditDir := filepath.Join(root, ".goagent", "audit")
	logFile := waitForAuditFile(t, auditDir, 2*time.Second)
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "temperature_effective") {
		t.Fatalf("missing temperature_effective in audit; got:\n%s", truncate(content, 1000))
	}
	if !strings.Contains(content, "temperature_in_payload") {
		t.Fatalf("missing temperature_in_payload in audit; got:\n%s", truncate(content, 1000))
	}
}
