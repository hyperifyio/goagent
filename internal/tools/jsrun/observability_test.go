package jsrun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// findRepoRoot is adapted to locate repository root containing go.mod.
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

func TestObservability_AuditLineWritten_OnSuccess(t *testing.T) {
	root := findRepoRoot(t)
	_ = os.RemoveAll(filepath.Join(root, ".goagent")) //nolint:errcheck // best-effort cleanup

	req := map[string]any{
		"source": "emit('ok')",
		"input":  "",
		"limits": map[string]any{"output_kb": 1},
	}
	b, _ := json.Marshal(req) //nolint:errcheck // inputs are deterministic
	stdout, stderr, err := Run(b)
	if err != nil || len(stderr) != 0 {
		t.Fatalf("unexpected error: %v stderr=%s", err, string(stderr))
	}
	if !json.Valid(stdout) {
		t.Fatalf("stdout not json: %s", string(stdout))
	}

	auditDir := filepath.Join(root, ".goagent", "audit")
	logFile := waitForAuditFile(t, auditDir, 2*time.Second)
	data, rerr := os.ReadFile(logFile)
	if rerr != nil {
		t.Fatalf("read audit: %v", rerr)
	}
	content := string(data)
	if !strings.Contains(content, "\"tool\":\"code.sandbox.js.run\"") {
		t.Fatalf("audit missing tool field: %s", content)
	}
	if !strings.Contains(content, "\"span\":\"tools.js.run\"") {
		t.Fatalf("audit missing span field: %s", content)
	}
	if !strings.Contains(content, "\"bytes_out\":") {
		t.Fatalf("audit missing bytes_out field: %s", content)
	}
	if !strings.Contains(content, "\"event\":\"success\"") {
		t.Fatalf("audit missing success event: %s", content)
	}
}
