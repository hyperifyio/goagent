package tools

import (
	"context"
	"encoding/json"
    "fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// https://github.com/hyperifyio/goagent/issues/1
func TestRunToolWithJSON_Timeout(t *testing.T) {
	dir := t.TempDir()

	// Build a small helper that sleeps longer than timeout
	helper := filepath.Join(dir, "sleeper.go")
	if err := os.WriteFile(helper, []byte(`package main
import ("time"; "os"; "io")
func main(){_,_ = io.ReadAll(os.Stdin); time.Sleep(2*time.Second)}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	bin := filepath.Join(dir, "sleeper")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, helper).CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v: %s", err, string(out))
	}

	spec := ToolSpec{Name: "sleep", Command: []string{bin}, TimeoutSec: 1}
	_, err := RunToolWithJSON(context.Background(), spec, []byte(`{}`), 3*time.Second)
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if err.Error() != "tool timed out" {
		t.Fatalf("expected 'tool timed out', got: %v", err)
	}
}

func TestRunToolWithJSON_SuccessEcho(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "echo.go")
	if err := os.WriteFile(helper, []byte(`package main
import ("io"; "os"; "fmt")
func main(){b,_:=io.ReadAll(os.Stdin); fmt.Print(string(b))}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	bin := filepath.Join(dir, "echo")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, helper).CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v: %s", err, string(out))
	}

	spec := ToolSpec{Name: "echo", Command: []string{bin}, TimeoutSec: 2}
	out, err := RunToolWithJSON(context.Background(), spec, []byte(`{"a":1}`), 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var js map[string]any
	if err := json.Unmarshal(out, &js); err != nil {
		t.Fatalf("bad json echo: %v; out=%s", err, string(out))
	}
}

// Ensure deterministic collection order: stderr from a failing tool is surfaced as error
func TestRunToolWithJSON_NonZeroExit_ReportsStderr(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "fail.go")
	if err := os.WriteFile(helper, []byte(`package main
import ("os"; "fmt")
func main(){fmt.Fprint(os.Stderr, "boom"); os.Exit(3)}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	bin := filepath.Join(dir, "fail")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, helper).CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v: %s", err, string(out))
	}

	spec := ToolSpec{Name: "fail", Command: []string{bin}, TimeoutSec: 2}
	_, err := RunToolWithJSON(context.Background(), spec, []byte(`{}`), 5*time.Second)
	if err == nil {
		t.Fatalf("expected error")
	}
	if err.Error() != "boom" {
		t.Fatalf("expected stderr content, got: %q", err.Error())
	}
}

// https://github.com/hyperifyio/goagent/issues/92
func TestRunToolWithJSON_AuditLog_WritesLine(t *testing.T) {
	// Run a quick echo tool and verify a log line is written
	dir := t.TempDir()
	helper := filepath.Join(dir, "echo.go")
	if err := os.WriteFile(helper, []byte(`package main
import ("io"; "os"; "fmt")
func main(){b,_:=io.ReadAll(os.Stdin); fmt.Print(string(b))}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	bin := filepath.Join(dir, "echo")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, helper).CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v: %s", err, string(out))
	}

	// Ensure audit dir at repo root is empty before run
	root := findRepoRoot(t)
	if err := os.RemoveAll(filepath.Join(root, ".goagent")); err != nil {
		t.Logf("cleanup: %v", err)
	}

	spec := ToolSpec{Name: "echo", Command: []string{bin}, TimeoutSec: 2}
	out, err := RunToolWithJSON(context.Background(), spec, []byte(`{"ok":true}`), 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) == 0 {
		t.Fatalf("expected output")
	}

	// Find today's audit log under repo root
	auditDir := filepath.Join(root, ".goagent", "audit")
	logFile := waitForAuditFile(t, auditDir, 2*time.Second)
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	// Expect at least one newline-terminated JSON object
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("audit log not newline terminated")
	}
	// Quick sanity check: file mode should be 0644
	st, err := os.Stat(logFile)
	if err == nil {
		if st.Mode().Type() != fs.ModeType && (st.Mode().Perm()&0o644) == 0 {
			t.Fatalf("unexpected permissions: %v", st.Mode())
		}
	}
}

// https://github.com/hyperifyio/goagent/issues/92
func TestRunToolWithJSON_AuditLog_RotationAcrossDateBoundary(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "echo.go")
	if err := os.WriteFile(helper, []byte(`package main
import ("io"; "os"; "fmt")
func main(){b,_:=io.ReadAll(os.Stdin); fmt.Print(string(b))}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	bin := filepath.Join(dir, "echo")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, helper).CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v: %s", err, string(out))
	}

	// Clean audit dir at repo root
	root := findRepoRoot(t)
	if err := os.RemoveAll(filepath.Join(root, ".goagent")); err != nil {
		t.Logf("cleanup: %v", err)
	}

	// Freeze time around midnight UTC so successive calls land in different files
	orig := timeNow
	defer func() { timeNow = orig }()
	t1 := time.Date(2025, 1, 2, 23, 59, 59, 0, time.UTC)
	t2 := t1.Add(2 * time.Second) // 2025-01-03 00:00:01 UTC

	spec := ToolSpec{Name: "echo", Command: []string{bin}, TimeoutSec: 2}

	timeNow = func() time.Time { return t1 }
	if _, err := RunToolWithJSON(context.Background(), spec, []byte(`{"a":1}`), 5*time.Second); err != nil {
		t.Fatalf("unexpected error first run: %v", err)
	}

	timeNow = func() time.Time { return t2 }
	if _, err := RunToolWithJSON(context.Background(), spec, []byte(`{"b":2}`), 5*time.Second); err != nil {
		t.Fatalf("unexpected error second run: %v", err)
	}

	auditDir := filepath.Join(root, ".goagent", "audit")
	want1 := filepath.Join(auditDir, "20250102.log")
	want2 := filepath.Join(auditDir, "20250103.log")

	// Allow brief delay for filesystem flush
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err1 := os.Stat(want1); err1 == nil {
			if _, err2 := os.Stat(want2); err2 == nil {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := os.Stat(want1); err != nil {
		t.Fatalf("expected first log file %s: %v", want1, err)
	}
	if _, err := os.Stat(want2); err != nil {
		t.Fatalf("expected second log file %s: %v", want2, err)
	}

	// Ensure each file has at least one line
	if b, err := os.ReadFile(want1); err == nil {
		if len(b) == 0 || b[len(b)-1] != '\n' {
			t.Fatalf("first audit file empty or not newline terminated")
		}
	} else {
		t.Fatalf("read %s: %v", want1, err)
	}
	if b, err := os.ReadFile(want2); err == nil {
		if len(b) == 0 || b[len(b)-1] != '\n' {
			t.Fatalf("second audit file empty or not newline terminated")
		}
	} else {
		t.Fatalf("read %s: %v", want2, err)
	}
}

// https://github.com/hyperifyio/goagent/issues/92
func TestRunToolWithJSON_AuditLog_Redaction(t *testing.T) {
	// Arrange: set env secrets and GOAGENT_REDACT patterns
	t.Setenv("OAI_API_KEY", "sk-test-1234567890")
	t.Setenv("GOAGENT_REDACT", "secret,sk-[a-z0-9]+")

	dir := t.TempDir()
	helper := filepath.Join(dir, "echo.go")
	if err := os.WriteFile(helper, []byte(`package main
import ("io"; "os"; "fmt")
func main(){b,_:=io.ReadAll(os.Stdin); fmt.Print(string(b))}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	bin := filepath.Join(dir, "echo")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, helper).CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v: %s", err, string(out))
	}

	// Clean audit dir at repo root
	root := findRepoRoot(t)
	if err := os.RemoveAll(filepath.Join(root, ".goagent")); err != nil {
		t.Logf("cleanup: %v", err)
	}

	// Use argv containing sensitive literals
	spec := ToolSpec{Name: "echo", Command: []string{bin, "--token=sk-test-1234567890", "--note=contains-secret"}, TimeoutSec: 2}
	if _, err := RunToolWithJSON(context.Background(), spec, []byte(`{"x":1}`), 5*time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Locate today's audit file under repo root
	auditDir := filepath.Join(root, ".goagent", "audit")
	logFile := waitForAuditFile(t, auditDir, 2*time.Second)
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	// Assert that raw secret substrings are not present
	if string(data) == "" || len(data) == 0 {
		t.Fatalf("empty audit")
	}
	if bytes := data; bytes != nil {
		if contains := string(bytes); contains != "" {
			if string(bytes) != "" && (containsFind(contains, "sk-test-1234567890") || containsFind(contains, "contains-secret")) {
				t.Fatalf("expected redaction, found sensitive substrings in audit: %s", contains)
			}
		}
	}
}

// New test encoding the centralized root behavior: logs must be written under
// the repository root's .goagent/audit, not the package working directory.
func TestRunToolWithJSON_AuditLog_CentralizedToRepoRoot(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "echo.go")
	if err := os.WriteFile(helper, []byte(`package main
import ("io"; "os"; "fmt")
func main(){b,_:=io.ReadAll(os.Stdin); fmt.Print(string(b))}
`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	bin := filepath.Join(dir, "echo")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, helper).CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v: %s", err, string(out))
	}

	root := findRepoRoot(t)
	// Clean both potential locations to start from a known state
	if err := os.RemoveAll(filepath.Join(root, ".goagent")); err != nil {
		t.Logf("cleanup root .goagent: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(".goagent")); err != nil {
		t.Logf("cleanup cwd .goagent: %v", err)
	}

	spec := ToolSpec{Name: "echo", Command: []string{bin}, TimeoutSec: 2}
	if _, err := RunToolWithJSON(context.Background(), spec, []byte(`{"ok":true}`), 5*time.Second); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect audit under repo root
	auditDirRoot := filepath.Join(root, ".goagent", "audit")
	// This helper will fail if no file appears in the expected root directory
	_ = waitForAuditFile(t, auditDirRoot, 2*time.Second)
}

// New test for env passthrough: only allowlisted keys are visible to the child,
// and audit logs include only key names (not values).
func TestRunToolWithJSON_EnvPassthrough_KeysOnlyAudit(t *testing.T) {
    dir := t.TempDir()
    helper := filepath.Join(dir, "printenv.go")
    if err := os.WriteFile(helper, []byte(`package main
import (
  "encoding/json"; "os"; "fmt"
)
func main(){
  out := map[string]string{
    "OAI_API_KEY": os.Getenv("OAI_API_KEY"),
    "OAI_BASE_URL": os.Getenv("OAI_BASE_URL"),
    "UNSAFE": os.Getenv("UNSAFE"),
  }
  b,_ := json.Marshal(out)
  fmt.Print(string(b))
}
`), 0o644); err != nil {
        t.Fatalf("write: %v", err)
    }
    bin := filepath.Join(dir, "printenv")
    if runtime.GOOS == "windows" { bin += ".exe" }
    if out, err := exec.Command("go", "build", "-o", bin, helper).CombinedOutput(); err != nil {
        t.Fatalf("build helper: %v: %s", err, string(out))
    }

    // Set env in parent
    t.Setenv("OAI_API_KEY", "sk-live-should-not-appear-in-audit")
    t.Setenv("OAI_BASE_URL", "https://example.invalid")
    t.Setenv("UNSAFE", "DO-NOT-PASS")

    // Clean audit dir at repo root
    root := findRepoRoot(t)
    if err := os.RemoveAll(filepath.Join(root, ".goagent")); err != nil { t.Logf("cleanup: %v", err) }

    // Allowlist only OAI_API_KEY and OAI_BASE_URL
    spec := ToolSpec{Name: "printenv", Command: []string{bin}, TimeoutSec: 2, EnvPassthrough: []string{"OAI_API_KEY", "OAI_BASE_URL"}}
    out, err := RunToolWithJSON(context.Background(), spec, []byte(`{}`), 5*time.Second)
    if err != nil { t.Fatalf("unexpected error: %v", err) }

    // Child should see allowed keys and not see UNSAFE
    var got map[string]string
    if err := json.Unmarshal(out, &got); err != nil { t.Fatalf("bad json: %v", err) }
    if got["OAI_API_KEY"] == "" || got["OAI_BASE_URL"] == "" {
        t.Fatalf("allowed envs not visible to child: %v", got)
    }
    if got["UNSAFE"] != "" { t.Fatalf("unexpected UNSAFE in child env: %q", got["UNSAFE"]) }

    // Audit should include envKeys but never the secret values
    auditDir := filepath.Join(root, ".goagent", "audit")
    logFile := waitForAuditFile(t, auditDir, 2*time.Second)
    data, errRead := os.ReadFile(logFile)
    if errRead != nil { t.Fatalf("read audit: %v", errRead) }
    s := string(data)
    // Must mention the keys
    if !strings.Contains(s, "\"envKeys\"") || !(strings.Contains(s, "OAI_API_KEY") && strings.Contains(s, "OAI_BASE_URL")) {
        t.Fatalf("audit missing envKeys or keys: %s", s)
    }
    // Must not contain the actual secret value
    if strings.Contains(s, "sk-live-should-not-appear-in-audit") || strings.Contains(s, fmt.Sprintf("%q", "sk-live-should-not-appear-in-audit")) {
        t.Fatalf("secret value leaked into audit: %s", s)
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

// containsFind is a tiny helper to avoid importing strings in this test's top-level import list diff
func containsFind(s, sub string) bool { return strings.Contains(s, sub) }
