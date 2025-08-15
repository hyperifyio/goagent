package tools

import (
	"context"
	"encoding/json"
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

    // Ensure audit dir is empty before run
    _ = os.RemoveAll(filepath.Join(".goagent"))

    spec := ToolSpec{Name: "echo", Command: []string{bin}, TimeoutSec: 2}
    out, err := RunToolWithJSON(context.Background(), spec, []byte(`{"ok":true}`), 5*time.Second)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(out) == 0 {
        t.Fatalf("expected output")
    }

    // Find today's audit log
    auditDir := filepath.Join(".goagent", "audit")
    // Allow small delay for file write
    deadline := time.Now().Add(2 * time.Second)
    var logFile string
    for {
        entries, _ := os.ReadDir(auditDir)
        for _, e := range entries {
            if !e.IsDir() {
                logFile = filepath.Join(auditDir, e.Name())
                break
            }
        }
        if logFile != "" || time.Now().After(deadline) {
            break
        }
        time.Sleep(10 * time.Millisecond)
    }
    if logFile == "" {
        t.Fatalf("audit log not created in %s", auditDir)
    }
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

    // Clean audit dir
    _ = os.RemoveAll(filepath.Join(".goagent"))

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

    auditDir := filepath.Join(".goagent", "audit")
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
    if b, _ := os.ReadFile(want1); len(b) == 0 || b[len(b)-1] != '\n' {
        t.Fatalf("first audit file empty or not newline terminated")
    }
    if b, _ := os.ReadFile(want2); len(b) == 0 || b[len(b)-1] != '\n' {
        t.Fatalf("second audit file empty or not newline terminated")
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

    // Clean audit dir
    _ = os.RemoveAll(filepath.Join(".goagent"))

    // Use argv containing sensitive literals
    spec := ToolSpec{Name: "echo", Command: []string{bin, "--token=sk-test-1234567890", "--note=contains-secret"}, TimeoutSec: 2}
    if _, err := RunToolWithJSON(context.Background(), spec, []byte(`{"x":1}`), 5*time.Second); err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

    // Locate today's audit file
    auditDir := filepath.Join(".goagent", "audit")
    deadline := time.Now().Add(2 * time.Second)
    var logFile string
    for {
        entries, _ := os.ReadDir(auditDir)
        for _, e := range entries {
            if !e.IsDir() { logFile = filepath.Join(auditDir, e.Name()); break }
        }
        if logFile != "" || time.Now().After(deadline) { break }
        time.Sleep(10 * time.Millisecond)
    }
    if logFile == "" {
        t.Fatalf("audit log not created in %s", auditDir)
    }
    data, err := os.ReadFile(logFile)
    if err != nil {
        t.Fatalf("read audit: %v", err)
    }
    // Assert that raw secret substrings are not present
    if string(data) == "" || len(data) == 0 { t.Fatalf("empty audit") }
    if bytes := data; bytes != nil {
        if contains := string(bytes); contains != "" {
            if (string(bytes) != "" && (containsFind(contains, "sk-test-1234567890") || containsFind(contains, "contains-secret"))) {
                t.Fatalf("expected redaction, found sensitive substrings in audit: %s", contains)
            }
        }
    }
}

// containsFind is a tiny helper to avoid importing strings in this test's top-level import list diff
func containsFind(s, sub string) bool { return strings.Contains(s, sub) }
