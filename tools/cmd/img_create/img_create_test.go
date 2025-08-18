package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

func buildTool(t *testing.T) string {
	// Build this package into a temp binary
	bin := filepath.Join(t.TempDir(), "img_create-test-bin")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, string(out))
	}
	return bin
}

func runTool(t *testing.T, bin string, in any) (stdout, stderr string, code int) {
	data, _ := json.Marshal(in)
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(data)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	return outBuf.String(), errBuf.String(), code
}

func TestMissingPrompt(t *testing.T) {
	bin := buildTool(t)
	_, stderr, code := runTool(t, bin, map[string]any{})
	if code == 0 {
		t.Fatalf("expected non-zero exit")
	}
	if len(stderr) == 0 {
		t.Fatalf("expected stderr error json")
	}
}
