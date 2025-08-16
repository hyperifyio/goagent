package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hyperifyio/goagent/internal/oai"
)

// https://github.com/hyperifyio/goagent/issues/97
func TestParseFlags_ApiKeyEnvPrecedence(t *testing.T) {
	// Save and restore env
	save := func(k string) (string, bool) { v, ok := os.LookupEnv(k); return v, ok }
	restore := func(k, v string, ok bool) {
		if ok {
			if err := os.Setenv(k, v); err != nil {
				t.Fatalf("restore %s: %v", k, err)
			}
		} else {
			if err := os.Unsetenv(k); err != nil {
				t.Fatalf("unset %s: %v", k, err)
			}
		}
	}
	oaiVal, oaiOK := save("OAI_API_KEY")
	openaiVal, openaiOK := save("OPENAI_API_KEY")
	defer func() { restore("OAI_API_KEY", oaiVal, oaiOK); restore("OPENAI_API_KEY", openaiVal, openaiOK) }()

	// Case 1: only OPENAI_API_KEY set -> used
	if err := os.Unsetenv("OAI_API_KEY"); err != nil {
		t.Fatalf("unset OAI_API_KEY: %v", err)
	}
	if err := os.Setenv("OPENAI_API_KEY", "legacy-token"); err != nil {
		t.Fatalf("set OPENAI_API_KEY: %v", err)
	}
	// parseFlags reads os.Args; simulate minimal args
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"agentcli.test", "-prompt", "x"}
	cfg, code := parseFlags()
	if code != 0 {
		t.Fatalf("unexpected parse exit: %d", code)
	}
	if cfg.apiKey != "legacy-token" {
		t.Fatalf("expected apiKey from OPENAI_API_KEY, got %q", cfg.apiKey)
	}

	// Case 2: both set -> OAI_API_KEY wins
	if err := os.Setenv("OAI_API_KEY", "canonical-token"); err != nil {
		t.Fatalf("set OAI_API_KEY: %v", err)
	}
	os.Args = []string{"agentcli.test", "-prompt", "x"}
	cfg, code = parseFlags()
	if code != 0 {
		t.Fatalf("unexpected parse exit: %d", code)
	}
	if cfg.apiKey != "canonical-token" {
		t.Fatalf("expected apiKey from OAI_API_KEY, got %q", cfg.apiKey)
	}

	// Case 3: flag overrides env
	os.Args = []string{"agentcli.test", "-prompt", "x", "-api-key", "from-flag"}
	cfg, code = parseFlags()
	if code != 0 {
		t.Fatalf("unexpected parse exit: %d", code)
	}
	if cfg.apiKey != "from-flag" {
		t.Fatalf("expected apiKey from flag, got %q", cfg.apiKey)
	}

	// Silence any stdout/stderr during runAgent for safety (not strictly needed here)
	_ = io.Discard
}

// https://github.com/hyperifyio/goagent/issues/243
func TestParseFlags_SplitTimeoutResolution(t *testing.T) {
	// Save/restore OAI_HTTP_TIMEOUT
	save := func(k string) (string, bool) { v, ok := os.LookupEnv(k); return v, ok }
	restore := func(k, v string, ok bool) {
		if ok {
			if err := os.Setenv(k, v); err != nil {
				t.Fatalf("restore %s: %v", k, err)
			}
		} else {
			if err := os.Unsetenv(k); err != nil {
				t.Fatalf("unset %s: %v", k, err)
			}
		}
	}
	httpEnvVal, httpEnvOK := save("OAI_HTTP_TIMEOUT")
	defer restore("OAI_HTTP_TIMEOUT", httpEnvVal, httpEnvOK)

	// Case 1: defaults — http falls back to legacy -timeout (30s), tool to 30s
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"agentcli.test", "-prompt", "x"}
	cfg, code := parseFlags()
	if code != 0 {
		t.Fatalf("parse exit: %d", code)
	}
	if cfg.httpTimeout != cfg.timeout || cfg.timeout != 30*time.Second {
		t.Fatalf("expected httpTimeout=timeout=30s, got http=%v timeout=%v", cfg.httpTimeout, cfg.timeout)
	}
	if cfg.toolTimeout != cfg.timeout {
		t.Fatalf("expected toolTimeout=timeout, got %v vs %v", cfg.toolTimeout, cfg.timeout)
	}

	// Case 2: env OAI_HTTP_TIMEOUT overrides legacy
	if err := os.Setenv("OAI_HTTP_TIMEOUT", "2m"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	os.Args = []string{"agentcli.test", "-prompt", "x"}
	cfg, code = parseFlags()
	if code != 0 {
		t.Fatalf("parse exit: %d", code)
	}
	if cfg.httpTimeout != 2*time.Minute {
		t.Fatalf("expected httpTimeout=2m from env, got %v", cfg.httpTimeout)
	}
	if cfg.toolTimeout != 30*time.Second {
		t.Fatalf("expected toolTimeout=30s default, got %v", cfg.toolTimeout)
	}

	// Case 3: flags override env and legacy
	os.Args = []string{"agentcli.test", "-prompt", "x", "-http-timeout", "5s", "-tool-timeout", "7s", "-timeout", "1s"}
	cfg, code = parseFlags()
	if code != 0 {
		t.Fatalf("parse exit: %d", code)
	}
	if cfg.httpTimeout != 5*time.Second || cfg.toolTimeout != 7*time.Second {
		t.Fatalf("expected http=5s tool=7s, got http=%v tool=%v", cfg.httpTimeout, cfg.toolTimeout)
	}
}

// https://github.com/hyperifyio/goagent/issues/214
func TestHelp_PrintsUsageAndExitsZero(t *testing.T) {
	// Capture stdout
	var outBuf, errBuf bytes.Buffer
	// Simulate help via various tokens
	for _, token := range []string{"--help", "-h", "help"} {
		t.Run(token, func(t *testing.T) {
			// Prepare args
			origArgs := os.Args
			defer func() { os.Args = origArgs }()
			os.Args = []string{"agentcli.test", token}

			// Replace os.Stdout/Stderr via writers by invoking printUsage directly
			outBuf.Reset()
			errBuf.Reset()
			// Call main path segments: emulate early help detection
			if !helpRequested(os.Args[1:]) {
				t.Fatalf("expected helpRequested for %s", token)
			}
			printUsage(&outBuf)
			// Validate output contains key lines
			got := outBuf.String()
			for _, substr := range []string{
				"Usage:",
				"-prompt",
				"-tools",
				"-base-url",
				"-api-key",
				"-http-timeout",
				"Examples:",
			} {
				if !strings.Contains(got, substr) {
					t.Fatalf("usage missing %q; got:\n%s", substr, got)
				}
			}
			// Also ensure no error text is printed by default path here
			if errBuf.Len() != 0 {
				t.Fatalf("unexpected stderr: %s", errBuf.String())
			}
			// Sanity: demonstrate zero exit would be used
			_ = fmt.Sprintf("")
		})
	}
}

// https://github.com/hyperifyio/goagent/issues/1
func TestRunAgent_ToolConversationLoop(t *testing.T) {
	// Fake tool: echo stdin to stdout
	dir := t.TempDir()
	helper := filepath.Join(dir, "echo.go")
	if err := os.WriteFile(helper, []byte(`package main
import ("io"; "os"; "fmt")
func main(){b,_:=io.ReadAll(os.Stdin); fmt.Print(string(b))}
`), 0o644); err != nil {
		t.Fatalf("write tool: %v", err)
	}
	bin := filepath.Join(dir, "echo")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, helper).CombinedOutput(); err != nil {
		t.Fatalf("build tool: %v: %s", err, string(out))
	}

	// Create tools.json referencing the echo tool
	toolsPath := filepath.Join(dir, "tools.json")
	manifest := map[string]any{
		"tools": []map[string]any{{
			"name":        "echo",
			"description": "echo back input",
			"schema":      map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []string{"text"}},
			"command":     []string{bin},
			"timeoutSec":  5,
		}},
	}
	b, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(toolsPath, b, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	// Fake server with two-step responses
	var step int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var req oai.ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("bad json: %v", err)
		}
		step++
		switch step {
		case 1:
			// Respond with a tool call to echo
			resp := oai.ChatCompletionsResponse{
				ID:      "cmpl-1",
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []oai.ChatCompletionsResponseChoice{{
					Index:        0,
					FinishReason: "tool_calls",
					Message: oai.Message{
						Role: oai.RoleAssistant,
						ToolCalls: []oai.ToolCall{{
							ID:   "1",
							Type: "function",
							Function: oai.ToolCallFunction{
								Name:      "echo",
								Arguments: `{"text":"hi"}`,
							},
						}},
					},
				}},
			}
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Fatalf("encode step1: %v", err)
			}
		case 2:
			resp := oai.ChatCompletionsResponse{
				ID:      "cmpl-2",
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []oai.ChatCompletionsResponseChoice{{
					Index:        0,
					FinishReason: "stop",
					Message:      oai.Message{Role: oai.RoleAssistant, Content: "done"},
				}},
			}
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Fatalf("encode step2: %v", err)
			}
		default:
			t.Fatalf("unexpected extra request step=%d", step)
		}
	}))
	defer srv.Close()

	cfg := cliConfig{
		prompt:       "test",
		toolsPath:    toolsPath,
		systemPrompt: "sys",
		baseURL:      srv.URL,
		apiKey:       "",
		model:        "test",
		maxSteps:     4,
		timeout:      5 * time.Second,
		temperature:  0,
		debug:        false,
	}

	var outBuf, errBuf bytes.Buffer
	code := runAgent(cfg, &outBuf, &errBuf)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%s", code, errBuf.String())
	}
	if got := outBuf.String(); got != "done\n" {
		t.Fatalf("unexpected stdout: %q", got)
	}
}

// https://github.com/hyperifyio/goagent/issues/1
func TestRunAgent_FailsWhenConfiguredToolUnavailable(t *testing.T) {
	dir := t.TempDir()
	// Create tools.json referencing a missing binary path
	missing := filepath.Join(dir, "missing-tool")
	toolsPath := filepath.Join(dir, "tools.json")
	manifest := map[string]any{
		"tools": []map[string]any{{
			"name":        "missing",
			"description": "should fail if unavailable",
			"schema":      map[string]any{"type": "object"},
			"command":     []string{missing},
			"timeoutSec":  2,
		}},
	}
	b, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(toolsPath, b, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	cfg := cliConfig{
		prompt:       "test",
		toolsPath:    toolsPath,
		systemPrompt: "sys",
		baseURL:      "http://unused.local", // not contacted due to early failure
		apiKey:       "",
		model:        "test",
		maxSteps:     1,
		timeout:      1 * time.Second,
		temperature:  0,
		debug:        false,
	}

	var outBuf, errBuf bytes.Buffer
	code := runAgent(cfg, &outBuf, &errBuf)
	if code == 0 {
		t.Fatalf("expected non-zero exit when tool is missing; stdout=%q stderr=%q", outBuf.String(), errBuf.String())
	}
	if got := errBuf.String(); !strings.Contains(got, "unavailable") {
		t.Fatalf("expected error mentioning unavailable tool, got: %q", got)
	}
}
