package wasmrun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// findRepoRoot locates repository root containing go.mod.
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

func TestObservability_AuditLineWritten_Unimplemented(t *testing.T) {
	root := findRepoRoot(t)
	_ = os.RemoveAll(filepath.Join(root, ".goagent")) //nolint:errcheck // best-effort cleanup

	req := map[string]any{
		"module_b64": "AA==", // one zero byte
		"entry":      "main",
		"input":      "",
		"limits":     map[string]any{"output_kb": 1, "wall_ms": 10, "mem_pages": 1},
	}
	b, _ := json.Marshal(req) //nolint:errcheck // inputs are deterministic
	stdout, stderr, err := Run(b)
	if err == nil || len(stdout) != 0 {
		t.Fatalf("expected unimplemented error with no stdout, got err=%v stdout=%s", err, string(stdout))
	}
	var e struct{ Code, Message string }
	if jerr := json.Unmarshal(stderr, &e); jerr != nil {
		t.Fatalf("stderr not JSON: %v: %s", jerr, string(stderr))
	}
	if e.Code != "UNIMPLEMENTED" {
		t.Fatalf("expected UNIMPLEMENTED, got %q", e.Code)
	}

	auditDir := filepath.Join(root, ".goagent", "audit")
	logFile := waitForAuditFile(t, auditDir, 2*time.Second)
	data, rerr := os.ReadFile(logFile)
	if rerr != nil {
		t.Fatalf("read audit: %v", rerr)
	}
	content := string(data)
	if !strings.Contains(content, "\"tool\":\"code.sandbox.wasm.run\"") {
		t.Fatalf("audit missing tool field: %s", content)
	}
	if !strings.Contains(content, "\"span\":\"tools.wasm.run\"") {
		t.Fatalf("audit missing span field: %s", content)
	}
	if !strings.Contains(content, "\"module_bytes\":1") {
		t.Fatalf("audit missing module_bytes field: %s", content)
	}
	if !strings.Contains(content, "\"wall_ms\":10") {
		t.Fatalf("audit missing wall_ms field: %s", content)
	}
	if !strings.Contains(content, "\"mem_pages_used\":0") {
		t.Fatalf("audit missing mem_pages_used field: %s", content)
	}
	if !strings.Contains(content, "\"bytes_out\":0") {
		t.Fatalf("audit missing bytes_out field: %s", content)
	}
	if !strings.Contains(content, "\"event\":\"UNIMPLEMENTED\"") {
		t.Fatalf("audit missing UNIMPLEMENTED event: %s", content)
	}
}
