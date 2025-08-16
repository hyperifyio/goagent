package main

// https://github.com/hyperifyio/goagent/issues/1

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	testutil "github.com/hyperifyio/goagent/tools/testutil"
)

type fsMkdirpOutput struct {
	Created bool `json:"created"`
}

// Build via shared helper in tools/testutil.

// runFsMkdirp runs the built fs_mkdirp tool with the given JSON input.
func runFsMkdirp(t *testing.T, bin string, input any) (fsMkdirpOutput, string, int) {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	cmd := exec.Command(bin)
	cmd.Dir = "."
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	var out fsMkdirpOutput
	_ = json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &out)
	return out, stderr.String(), code
}

func TestFsMkdirp_DeepCreateAndIdempotence(t *testing.T) {
	bin := testutil.BuildTool(t, "fs_mkdirp")

	dir := makeRepoRelTempDir(t, "fsmkdirp-")
	deep := filepath.Join(dir, "a", "b", "c")

	// First call should create directories
	out1, stderr1, code1 := runFsMkdirp(t, bin, map[string]any{
		"path":      deep,
		"modeOctal": "0755",
	})
	if code1 != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code1, stderr1)
	}
	if !out1.Created {
		t.Fatalf("expected created=true on first call")
	}
	if info, err := os.Stat(deep); err != nil || !info.IsDir() {
		t.Fatalf("expected directory to exist, err=%v", err)
	}

	// Second call should be idempotent (created=false)
	out2, stderr2, code2 := runFsMkdirp(t, bin, map[string]any{
		"path":      deep,
		"modeOctal": "0755",
	})
	if code2 != 0 {
		t.Fatalf("expected success on second call, got exit=%d stderr=%q", code2, stderr2)
	}
	if out2.Created {
		t.Fatalf("expected created=false on second call")
	}
}

// TestFsMkdirp_ErrorJSON verifies the standardized error contract: on failure,
// the tool must write a single-line JSON object to stderr with an "error" key
// and exit non-zero. Use an absolute path to trigger validation failure.
func TestFsMkdirp_ErrorJSON(t *testing.T) {
	bin := testutil.BuildTool(t, "fs_mkdirp")

	// Absolute path should be rejected per repo-relative constraint.
	abs := string(os.PathSeparator) + filepath.Join("tmp", "mkabs")

	_, stderr, code := runFsMkdirp(t, bin, map[string]any{
		"path": abs,
	})
	if code == 0 {
		t.Fatalf("expected non-zero exit on invalid absolute path")
	}
	// Must be single-line JSON with {"error":...}
	line := strings.TrimSpace(stderr)
	var obj map[string]any
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		t.Fatalf("stderr is not JSON: %q err=%v", line, err)
	}
	if _, ok := obj["error"]; !ok {
		t.Fatalf("stderr JSON missing 'error' key: %v", obj)
	}
}
