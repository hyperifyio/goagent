package examples

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGPT5_LiveSmoke_DefaultTemperature runs the CLI against a live GPT-5 endpoint
// when GPT5_OPENAI_API_URL and GPT5_OPENAI_API_KEY are exported. It asserts that the
// debug request dump includes temperature: 1 with no sampling flags.
func TestGPT5_LiveSmoke_DefaultTemperature(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("GPT5_OPENAI_API_URL"))
	apiKey := strings.TrimSpace(os.Getenv("GPT5_OPENAI_API_KEY"))
	if baseURL == "" || apiKey == "" {
		t.Skip("set GPT5_OPENAI_API_URL and GPT5_OPENAI_API_KEY to run this live smoke test")
	}

	// Build agent CLI binary from repo root for correctness
	root := findRepoRoot(t)
	tmp := t.TempDir()
	agentBin := filepath.Join(tmp, "agentcli")
	cmdBuild := exec.Command("go", "build", "-o", agentBin, "./cmd/agentcli")
	cmdBuild.Dir = root
	if out, err := cmdBuild.CombinedOutput(); err != nil {
		t.Fatalf("build agentcli: %v: %s", err, string(out))
	}

	// Run the agent binary with -debug so request JSON is dumped locally before HTTP
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(agentBin,
		"-prompt", "Say ok",
		"-base-url", baseURL,
		"-api-key", apiKey,
		"-model", "gpt-5",
		"-max-steps", "1",
		"-http-timeout", "30s",
		"-debug",
	)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run() // do not require success; we only need the local request dump

	tr := stderr.String()
	if !strings.Contains(tr, "--- chat.request step=1 ---") {
		t.Fatalf("missing debug request dump; stderr=\n%s", tr)
	}
	if !strings.Contains(tr, "\"temperature\": 1") {
		t.Fatalf("expected temperature 1 in request; stderr=\n%s", tr)
	}

	// Note: Reasoning controls (verbosity/reasoning_effort) are independent of temperature
	// and may be configured per-provider. This smoke only asserts default temperature.
}
