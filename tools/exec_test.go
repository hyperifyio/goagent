package main

import (
    "bytes"
    "encoding/json"
    "os/exec"
    "runtime"
    "strings"
    "testing"

    testutil "github.com/hyperifyio/goagent/tools/testutil"
)

// execOutput models the expected stdout JSON contract from tools/exec.go
type execOutput struct {
	ExitCode   int    `json:"exitCode"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"durationMs"`
}

// runExec runs the built exec tool with the given JSON input and decodes stdout.
func runExec(t *testing.T, bin string, input any) execOutput {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("exec tool failed to run: %v, stderr=%s", err, stderr.String())
	}
	// Output must be single-line JSON
	out := strings.TrimSpace(stdout.String())
	var parsed execOutput
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("failed to parse exec output JSON: %v; raw=%q", err, out)
	}
	if parsed.DurationMs < 0 {
		t.Fatalf("durationMs must be >= 0, got %d", parsed.DurationMs)
	}
	return parsed
}

func TestExec_SuccessEcho(t *testing.T) {
    bin := testutil.BuildTool(t, "exec")
	// Use /bin/echo on Unix; on Windows, use cmd /c echo via a small program is complex.
	if runtime.GOOS == "windows" {
		t.Skip("windows not supported in this test environment")
	}
	out := runExec(t, bin, map[string]any{
		"cmd":  "/bin/echo",
		"args": []string{"hello"},
	})
	if out.ExitCode != 0 {
		t.Fatalf("expected exitCode 0, got %d (stderr=%q)", out.ExitCode, out.Stderr)
	}
	if strings.TrimSpace(out.Stdout) != "hello" {
		t.Fatalf("unexpected stdout: %q", out.Stdout)
	}
}

func TestExec_NonZeroExit(t *testing.T) {
    bin := testutil.BuildTool(t, "exec")
	if runtime.GOOS == "windows" {
		t.Skip("windows not supported in this test environment")
	}
	// /bin/false exits with code 1
	out := runExec(t, bin, map[string]any{
		"cmd":  "/bin/false",
		"args": []string{},
	})
	if out.ExitCode == 0 {
		t.Fatalf("expected non-zero exitCode, got 0")
	}
	if out.Stdout != "" {
		t.Fatalf("expected empty stdout for /bin/false, got %q", out.Stdout)
	}
}

func TestExec_Timeout(t *testing.T) {
    bin := testutil.BuildTool(t, "exec")
	if runtime.GOOS == "windows" {
		t.Skip("windows not supported in this test environment")
	}
	out := runExec(t, bin, map[string]any{
		"cmd":        "/bin/sleep",
		"args":       []string{"2"},
		"timeoutSec": 1,
	})
	if out.ExitCode == 0 {
		t.Fatalf("expected timeout to produce non-zero exitCode, got 0")
	}
	if !strings.Contains(strings.ToLower(out.Stderr), "timeout") {
		t.Fatalf("stderr should mention timeout, got %q", out.Stderr)
	}
	if out.DurationMs < 900 || out.DurationMs > 3000 {
		t.Fatalf("durationMs out of expected range: %d", out.DurationMs)
	}
}

func TestExec_CwdAndEnv(t *testing.T) {
    bin := testutil.BuildTool(t, "exec")
	if runtime.GOOS == "windows" {
		t.Skip("windows not supported in this test environment")
	}
	tmpDir := t.TempDir()
	out := runExec(t, bin, map[string]any{
		"cmd":  "/bin/pwd",
		"args": []string{},
		"cwd":  tmpDir,
		"env": map[string]string{
			"FOO": "BAR",
		},
	})
	if strings.TrimSpace(out.Stdout) != tmpDir {
		t.Fatalf("pwd did not respect cwd: expected %q, got %q", tmpDir, out.Stdout)
	}

	// Now verify env via /usr/bin/env
	out2 := runExec(t, bin, map[string]any{
		"cmd":  "/usr/bin/env",
		"args": []string{},
		"env": map[string]string{
			"HELLO": "WORLD",
		},
	})
	if !strings.Contains(out2.Stdout, "HELLO=WORLD") {
		t.Fatalf("env var not present in stdout: %q", out2.Stdout)
	}
}

func TestExec_StdinPassthrough(t *testing.T) {
    bin := testutil.BuildTool(t, "exec")
	if runtime.GOOS == "windows" {
		t.Skip("windows not supported in this test environment")
	}
	out := runExec(t, bin, map[string]any{
		"cmd":   "/bin/cat",
		"args":  []string{},
		"stdin": "xyz",
	})
	if out.Stdout != "xyz" {
		t.Fatalf("stdin passthrough failed, got %q", out.Stdout)
	}
}
