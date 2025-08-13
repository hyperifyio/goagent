package tools

import (
    "context"
    "encoding/json"
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
    if runtime.GOOS == "windows" { bin += ".exe" }
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
    if runtime.GOOS == "windows" { bin += ".exe" }
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
