package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	testutil "github.com/hyperifyio/goagent/tools/testutil"
)

type timeOutput struct {
	Timezone string `json:"timezone"`
	ISO8601  string `json:"iso8601"`
}

func runTimeTool(t *testing.T, bin string, input any) (timeOutput, string, int) {
	t.Helper()
	b, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(b)
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
	var out timeOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &out); err != nil && code == 0 {
		t.Fatalf("unmarshal stdout: %v; raw=%q", err, stdout.String())
	}
	return out, stderr.String(), code
}

func TestTimeCLI_AcceptsTimezoneAndOutputsISO8601(t *testing.T) {
	bin := testutil.BuildTool(t, "get_time")
	out, stderr, code := runTimeTool(t, bin, map[string]any{"timezone": "UTC"})
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if out.Timezone != "UTC" || out.ISO8601 == "" {
		t.Fatalf("unexpected output: %+v", out)
	}
	if _, err := time.Parse(time.RFC3339, out.ISO8601); err != nil {
		t.Fatalf("iso8601 not RFC3339: %v", err)
	}
}

func TestTimeCLI_AcceptsAliasTZ(t *testing.T) {
	bin := testutil.BuildTool(t, "get_time")
	out, stderr, code := runTimeTool(t, bin, map[string]any{"tz": "Europe/Helsinki"})
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if out.Timezone != "Europe/Helsinki" || out.ISO8601 == "" {
		t.Fatalf("unexpected output: %+v", out)
	}
	if _, err := time.Parse(time.RFC3339, out.ISO8601); err != nil {
		t.Fatalf("iso8601 not RFC3339: %v", err)
	}
}

func TestTimeCLI_MissingTimezone_ErrorContract(t *testing.T) {
	bin := testutil.BuildTool(t, "get_time")
	out, stderr, code := runTimeTool(t, bin, map[string]any{})
	if code == 0 {
		t.Fatalf("expected non-zero exit for missing timezone, got 0; stderr=%q", stderr)
	}
	if out.Timezone != "" || out.ISO8601 != "" {
		t.Fatalf("stdout should be empty on error, got: %+v", out)
	}
	s := strings.TrimSpace(stderr)
	if s == "" || !strings.Contains(s, "\"error\"") {
		t.Fatalf("stderr should contain JSON error, got: %q", stderr)
	}
}

func TestTimeCLI_InvalidTimezone_ErrorContract(t *testing.T) {
	bin := testutil.BuildTool(t, "get_time")
	out, stderr, code := runTimeTool(t, bin, map[string]any{"timezone": "Not/AZone"})
	if code == 0 {
		t.Fatalf("expected non-zero exit for invalid timezone, got 0; stderr=%q", stderr)
	}
	if out.Timezone != "" || out.ISO8601 != "" {
		t.Fatalf("stdout should be empty on error, got: %+v", out)
	}
	s := strings.TrimSpace(stderr)
	if s == "" || !strings.Contains(s, "\"error\"") {
		t.Fatalf("stderr should contain JSON error, got: %q", stderr)
	}
}

func TestToolbeltDiagramExists(t *testing.T) {
	if _, err := os.Stat("../../../docs/diagrams/toolbelt-seq.md"); err != nil {
		t.Fatalf("missing docs/diagrams/toolbelt-seq.md: %v", err)
	}
}
