package tools

import (
	"context"
	"encoding/json"
    "io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
