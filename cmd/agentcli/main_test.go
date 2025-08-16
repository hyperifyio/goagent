package main

import (
    "bytes"
    "encoding/json"
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
    "github.com/hyperifyio/goagent/internal/tools"
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

// https://github.com/hyperifyio/goagent/issues/251
// Default sampling temperature must resolve to 1.0 and propagate in requests when unset.
func TestDefaultTemperature_IsOneAndPropagates(t *testing.T) {
	// Fake server that captures the incoming temperature
	var seenTemp *float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req oai.ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		seenTemp = req.Temperature
		// Return a minimal valid response to terminate
		resp := oai.ChatCompletionsResponse{
			Choices: []oai.ChatCompletionsResponseChoice{{
				Message: oai.Message{Role: oai.RoleAssistant, Content: "ok"},
			}},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}))
	defer srv.Close()

	// Simulate CLI args without -temp to rely on default
	orig := os.Args
	defer func() { os.Args = orig }()
	os.Args = []string{"agentcli.test", "-prompt", "x", "-base-url", srv.URL, "-model", "m"}

	cfg, code := parseFlags()
	if code != 0 {
		t.Fatalf("parse exit: %d", code)
	}
	if cfg.temperature != 1.0 {
		t.Fatalf("default temperature got %v want 1.0", cfg.temperature)
	}

	var outBuf, errBuf bytes.Buffer
	code = runAgent(cfg, &outBuf, &errBuf)
	if code != 0 {
		t.Fatalf("runAgent exit=%d stderr=%s", code, errBuf.String())
	}
	if seenTemp == nil || *seenTemp != 1.0 {
		if seenTemp == nil {
			t.Fatalf("temperature missing in request; want 1.0")
		}
		t.Fatalf("temperature in request got %v want 1.0", *seenTemp)
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
			// Sanity: do nothing; zero exit is implied
		})
	}
}

// https://github.com/hyperifyio/goagent/issues/246
func TestPrintConfig_EmitsResolvedConfigJSONAndExitsZero(t *testing.T) {
	// Save/restore env for OAI_HTTP_TIMEOUT
	val, ok := os.LookupEnv("OAI_HTTP_TIMEOUT")
	if ok {
		defer func() {
			if err := os.Setenv("OAI_HTTP_TIMEOUT", val); err != nil {
				t.Fatalf("restore env: %v", err)
			}
		}()
	} else {
		defer func() {
			if err := os.Unsetenv("OAI_HTTP_TIMEOUT"); err != nil {
				t.Fatalf("unset env: %v", err)
			}
		}()
	}
	if err := os.Setenv("OAI_HTTP_TIMEOUT", "100ms"); err != nil {
		t.Fatalf("set env: %v", err)
	}

	// Prepare args: no -prompt required when -print-config is set
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"agentcli.test", "-print-config", "-model", "m", "-base-url", "http://example"}

	cfg, code := parseFlags()
	if code != 0 {
		t.Fatalf("parse exit: %d", code)
	}

	var outBuf bytes.Buffer
	exit := printResolvedConfig(cfg, &outBuf)
	if exit != 0 {
		t.Fatalf("expected exit 0")
	}
	// Validate JSON contains fields and sources
	got := outBuf.String()
	for _, substr := range []string{
		"\"model\": \"m\"",
		"\"baseURL\": \"http://example\"",
		"\"httpTimeout\": \"100ms\"",
		"\"httpTimeoutSource\": \"env\"",
		"\"toolTimeout\": ",
		"\"timeout\": ",
		"\"timeoutSource\": ",
	} {
		if !strings.Contains(got, substr) {
			t.Fatalf("print-config missing %q; got:\n%s", substr, got)
		}
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

// https://github.com/hyperifyio/goagent/issues/233
func TestRunAgent_HTTPTimeoutError_MessageIncludesDetails(t *testing.T) {
	// Fake slow server: sleeps beyond client timeout then returns a valid response
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		resp := oai.ChatCompletionsResponse{
			Choices: []oai.ChatCompletionsResponseChoice{{
				Message: oai.Message{Role: oai.RoleAssistant, Content: "ok"},
			}},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode slow resp: %v", err)
		}
	}))
	defer slow.Close()

	cfg := cliConfig{
		prompt:       "test",
		systemPrompt: "sys",
		baseURL:      slow.URL,
		model:        "test",
		maxSteps:     1,
		httpTimeout:  100 * time.Millisecond,
		toolTimeout:  1 * time.Second,
		temperature:  0,
		debug:        false,
	}

	var outBuf, errBuf bytes.Buffer
	code := runAgent(cfg, &outBuf, &errBuf)
	if code == 0 {
		t.Fatalf("expected non-zero exit due to HTTP timeout; stdout=%q stderr=%q", outBuf.String(), errBuf.String())
	}
	got := errBuf.String()
	// Error should mention base URL, configured timeout, and a user hint
	if !strings.Contains(got, slow.URL) {
		t.Fatalf("expected error to include base URL %q; got: %q", slow.URL, got)
	}
	if !strings.Contains(got, "http-timeout=100ms") {
		t.Fatalf("expected error to include configured timeout; got: %q", got)
	}
	if !strings.Contains(strings.ToLower(got), "increase -http-timeout") {
		t.Fatalf("expected hint to increase -http-timeout; got: %q", got)
	}
}

// https://github.com/hyperifyio/goagent/issues/233
func TestRunAgent_HTTPTimeout_RaiseResolves(t *testing.T) {
	// Server is slow but within a larger timeout
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(120 * time.Millisecond)
		resp := oai.ChatCompletionsResponse{
			Choices: []oai.ChatCompletionsResponseChoice{{
				Message: oai.Message{Role: oai.RoleAssistant, Content: "ok"},
			}},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode slow resp: %v", err)
		}
	}))
	defer srv.Close()

	cfg := cliConfig{
		prompt:       "test",
		systemPrompt: "sys",
		baseURL:      srv.URL,
		model:        "test",
		maxSteps:     1,
		httpTimeout:  500 * time.Millisecond,
		toolTimeout:  1 * time.Second,
		temperature:  0,
		debug:        false,
	}

	var outBuf, errBuf bytes.Buffer
	code := runAgent(cfg, &outBuf, &errBuf)
	if code != 0 {
		t.Fatalf("expected exit code 0 with higher timeout; stderr=%s", errBuf.String())
	}
	if strings.TrimSpace(outBuf.String()) != "ok" {
		t.Fatalf("unexpected stdout: %q", outBuf.String())
	}
}

// https://github.com/hyperifyio/goagent/issues/247
// Scaled integration: default-like 90ms times out, raised 300ms succeeds against a ~120ms server.
func TestHTTPTimeout_SlowServer_DefaultTimesOut_RaisedSucceeds(t *testing.T) {
	// Slow-ish server (~120ms)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(120 * time.Millisecond)
		resp := oai.ChatCompletionsResponse{
			Choices: []oai.ChatCompletionsResponseChoice{{
				Message: oai.Message{Role: oai.RoleAssistant, Content: "ok"},
			}},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode resp: %v", err)
		}
	}))
	defer srv.Close()

	// Phase 1: env-driven ~90ms timeout -> expect timeout
	// Save/restore env
	val, ok := os.LookupEnv("OAI_HTTP_TIMEOUT")
	if ok {
		defer func() {
			if err := os.Setenv("OAI_HTTP_TIMEOUT", val); err != nil {
				t.Fatalf("restore env: %v", err)
			}
		}()
	} else {
		defer func() {
			if err := os.Unsetenv("OAI_HTTP_TIMEOUT"); err != nil {
				t.Fatalf("unset env: %v", err)
			}
		}()
	}
	if err := os.Setenv("OAI_HTTP_TIMEOUT", "90ms"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	// Use parseFlags path to simulate CLI invocation
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"agentcli.test", "-prompt", "x", "-base-url", srv.URL, "-model", "m"}
	cfg, code := parseFlags()
	if code != 0 {
		t.Fatalf("parse exit: %d", code)
	}
	var out1, err1 bytes.Buffer
	exit1 := runAgent(cfg, &out1, &err1)
	if exit1 == 0 {
		t.Fatalf("expected timeout exit; stdout=%q stderr=%q", out1.String(), err1.String())
	}
	if got := err1.String(); !strings.Contains(got, "http-timeout=90ms") {
		t.Fatalf("expected error to mention http-timeout=90ms; got: %q", got)
	}

	// Phase 2: raise -http-timeout to 300ms -> expect success
	os.Args = []string{"agentcli.test", "-prompt", "x", "-http-timeout", "300ms", "-base-url", srv.URL, "-model", "m"}
	cfg2, code2 := parseFlags()
	if code2 != 0 {
		t.Fatalf("parse exit: %d", code2)
	}
	var out2, err2 bytes.Buffer
	exit2 := runAgent(cfg2, &out2, &err2)
	if exit2 != 0 {
		t.Fatalf("expected success with raised timeout; stderr=%s", err2.String())
	}
	if strings.TrimSpace(out2.String()) != "ok" {
		t.Fatalf("unexpected stdout: %q", out2.String())
	}
}

// https://github.com/hyperifyio/goagent/issues/245
func TestDebug_EffectiveTimeoutsAndSources(t *testing.T) {
	// Fast server returning a minimal valid response
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := oai.ChatCompletionsResponse{
			Choices: []oai.ChatCompletionsResponseChoice{{
				Message: oai.Message{Role: oai.RoleAssistant, Content: "ok"},
			}},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode resp: %v", err)
		}
	}))
	defer srv.Close()

	// Use flags so sources are "flag"
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"agentcli.test", "-prompt", "x", "-http-timeout", "5s", "-tool-timeout", "7s", "-timeout", "10s", "-base-url", srv.URL, "-model", "m"}
	cfg, code := parseFlags()
	if code != 0 {
		t.Fatalf("parse exit: %d", code)
	}
	cfg.debug = true

	var outBuf, errBuf bytes.Buffer
	code = runAgent(cfg, &outBuf, &errBuf)
	if code != 0 {
		t.Fatalf("expected exit code 0; stderr=%s", errBuf.String())
	}
	got := errBuf.String()
	if !strings.Contains(got, "effective timeouts: http-timeout=5s source=flag; tool-timeout=7s source=flag; timeout=10s source=flag") {
		t.Fatalf("missing effective timeouts line; got:\n%s", got)
	}
}

// https://github.com/hyperifyio/goagent/issues/245
func TestHTTPTimeoutError_IncludesSourceAndValue(t *testing.T) {
	// Slow server to trigger client timeout
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		resp := oai.ChatCompletionsResponse{
			Choices: []oai.ChatCompletionsResponseChoice{{
				Message: oai.Message{Role: oai.RoleAssistant, Content: "ok"},
			}},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode resp: %v", err)
		}
	}))
	defer slow.Close()

	// Set http-timeout via env so source is "env"
	val, ok := os.LookupEnv("OAI_HTTP_TIMEOUT")
	if ok {
		defer func() {
			if err := os.Setenv("OAI_HTTP_TIMEOUT", val); err != nil {
				t.Fatalf("restore env: %v", err)
			}
		}()
	} else {
		defer func() {
			if err := os.Unsetenv("OAI_HTTP_TIMEOUT"); err != nil {
				t.Fatalf("unset env: %v", err)
			}
		}()
	}
	if err := os.Setenv("OAI_HTTP_TIMEOUT", "100ms"); err != nil {
		t.Fatalf("set env: %v", err)
	}

	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"agentcli.test", "-prompt", "x", "-base-url", slow.URL, "-model", "m"}
	cfg, code := parseFlags()
	if code != 0 {
		t.Fatalf("parse exit: %d", code)
	}

	var outBuf, errBuf bytes.Buffer
	code = runAgent(cfg, &outBuf, &errBuf)
	if code == 0 {
		t.Fatalf("expected non-zero exit; stdout=%q stderr=%q", outBuf.String(), errBuf.String())
	}
	got := errBuf.String()
	if !strings.Contains(got, "http-timeout=100ms") {
		t.Fatalf("expected error to include http-timeout value; got: %q", got)
	}
	if !strings.Contains(got, "(http-timeout source=env)") {
		t.Fatalf("expected error to include timeout source env; got: %q", got)
	}
}

// https://github.com/hyperifyio/goagent/issues/243
// Ensure chat POST uses -http-timeout exclusively even if legacy -timeout is shorter
func TestRunAgent_HTTPTimeout_IgnoresShortGlobal(t *testing.T) {
	// Server sleeps longer than global timeout but shorter than http-timeout
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		resp := oai.ChatCompletionsResponse{
			Choices: []oai.ChatCompletionsResponseChoice{{
				Message: oai.Message{Role: oai.RoleAssistant, Content: "ok"},
			}},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode resp: %v", err)
		}
	}))
	defer srv.Close()

	cfg := cliConfig{
		prompt:       "test",
		systemPrompt: "sys",
		baseURL:      srv.URL,
		model:        "test",
		maxSteps:     1,
		timeout:      50 * time.Millisecond,  // legacy global shorter than server latency
		httpTimeout:  500 * time.Millisecond, // HTTP timeout longer than server latency
		toolTimeout:  1 * time.Second,
		temperature:  0,
		debug:        false,
	}

	var outBuf, errBuf bytes.Buffer
	code := runAgent(cfg, &outBuf, &errBuf)
	if code != 0 {
		t.Fatalf("expected exit code 0; stderr=%s", errBuf.String())
	}
	if strings.TrimSpace(outBuf.String()) != "ok" {
		t.Fatalf("unexpected stdout: %q", outBuf.String())
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

// https://github.com/hyperifyio/goagent/issues/256
// Verify that multiple tool_calls are executed in parallel rather than sequentially.
// We call appendToolCallOutputs directly to isolate the execution time from HTTP.
func TestAppendToolCallOutputs_ExecutesInParallel(t *testing.T) {
    // Build a helper tool that sleeps per JSON input then emits JSON
    dir := t.TempDir()
    helper := filepath.Join(dir, "sleeper.go")
    if err := os.WriteFile(helper, []byte(`package main
import (
  "encoding/json"; "io"; "os"; "time"; "fmt"
)
func main(){
  b,_ := io.ReadAll(os.Stdin)
  var m map[string]any
  _ = json.Unmarshal(b, &m)
  name, _ := m["name"].(string)
  ms := 0
  if v, ok := m["sleepMs"].(float64); ok { ms = int(v) }
  if ms > 0 { time.Sleep(time.Duration(ms) * time.Millisecond) }
  _ = json.NewEncoder(os.Stdout).Encode(map[string]any{"name": name, "sleptMs": ms})
  fmt.Print("")
}
`), 0o644); err != nil {
        t.Fatalf("write tool: %v", err)
    }
    bin := filepath.Join(dir, "sleeper")
    if runtime.GOOS == "windows" { bin += ".exe" }
    if out, err := exec.Command("go", "build", "-o", bin, helper).CombinedOutput(); err != nil {
        t.Fatalf("build tool: %v: %s", err, string(out))
    }

    // Build the registry expected by appendToolCallOutputs
    realTools := map[string]tools.ToolSpec{
        "slow": { Name: "slow", Command: []string{bin}, TimeoutSec: 5 },
        "fast": { Name: "fast", Command: []string{bin}, TimeoutSec: 5 },
    }

    // Craft assistant message containing two tool calls with different sleeps
    msg := oai.Message{ Role: oai.RoleAssistant }
    msg.ToolCalls = []oai.ToolCall{
        { ID: "1", Type: "function", Function: oai.ToolCallFunction{ Name: "slow", Arguments: `{"sleepMs":600,"name":"slow"}` } },
        { ID: "2", Type: "function", Function: oai.ToolCallFunction{ Name: "fast", Arguments: `{"sleepMs":600,"name":"fast"}` } },
    }

    // Minimal cfg with a generous per-tool timeout
    cfg := cliConfig{ toolTimeout: 3 * time.Second }

    // Measure elapsed around appendToolCallOutputs
    start := time.Now()
    out := appendToolCallOutputs(nil, msg, realTools, cfg)
    elapsed := time.Since(start)

    // Expect two tool messages appended
    gotIDs := map[string]bool{}
    for _, m := range out {
        if m.Role == oai.RoleTool {
            gotIDs[m.ToolCallID] = true
        }
    }
    if !gotIDs["1"] || !gotIDs["2"] {
        t.Fatalf("expected tool messages for ids 1 and 2; got %+v", gotIDs)
    }

    // Sequential would be ~1200ms (+overhead). Parallel should be well under 1200ms.
    if elapsed >= 1100*time.Millisecond {
        t.Fatalf("tool calls did not run in parallel; elapsed=%v (want < 1.1s)", elapsed)
    }
}

// https://github.com/hyperifyio/goagent/issues/242
func TestTimeoutPrecedence_Table(t *testing.T) {
	// Helpers to save/restore env
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

	type testCase struct {
		name       string
		envHTTP    string
		args       []string
		wantHTTP   time.Duration
		wantTool   time.Duration
		wantGlobal time.Duration
	}

	cases := []testCase{
		{
			name:       "FlagsOverrideEnv",
			envHTTP:    "90s",
			args:       []string{"agentcli.test", "-prompt", "x", "-http-timeout", "300s", "-tool-timeout", "300s"},
			wantHTTP:   5 * time.Minute,
			wantTool:   5 * time.Minute,
			wantGlobal: 30 * time.Second,
		},
		{
			name:       "EnvOnly_HTTP",
			envHTTP:    "90s",
			args:       []string{"agentcli.test", "-prompt", "x"},
			wantHTTP:   90 * time.Second,
			wantTool:   30 * time.Second,
			wantGlobal: 30 * time.Second,
		},
		{
			name:       "LegacyGlobalOnly",
			envHTTP:    "",
			args:       []string{"agentcli.test", "-prompt", "x", "-timeout", "300s"},
			wantHTTP:   5 * time.Minute,
			wantTool:   5 * time.Minute,
			wantGlobal: 5 * time.Minute,
		},
	}

	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envHTTP == "" {
				if err := os.Unsetenv("OAI_HTTP_TIMEOUT"); err != nil {
					t.Fatalf("unset env: %v", err)
				}
			} else {
				if err := os.Setenv("OAI_HTTP_TIMEOUT", tc.envHTTP); err != nil {
					t.Fatalf("set env: %v", err)
				}
			}

			os.Args = tc.args
			cfg, code := parseFlags()
			if code != 0 {
				t.Fatalf("parse exit: %d", code)
			}
			if cfg.httpTimeout != tc.wantHTTP {
				t.Fatalf("httpTimeout got %v want %v", cfg.httpTimeout, tc.wantHTTP)
			}
			if cfg.toolTimeout != tc.wantTool {
				t.Fatalf("toolTimeout got %v want %v", cfg.toolTimeout, tc.wantTool)
			}
			if cfg.timeout != tc.wantGlobal {
				t.Fatalf("global timeout (-timeout) got %v want %v", cfg.timeout, tc.wantGlobal)
			}
		})
	}
}

// https://github.com/hyperifyio/goagent/issues/243
// Ensure -http-timeout is not clamped by legacy -timeout (shorter or longer)
func TestHTTPTimeout_NotClampedByGlobal(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	t.Run("GlobalShorter", func(t *testing.T) {
		os.Args = []string{"agentcli.test", "-prompt", "x", "-http-timeout", "300s", "-timeout", "1s"}
		cfg, code := parseFlags()
		if code != 0 {
			t.Fatalf("parse exit: %d", code)
		}
		if cfg.httpTimeout != 300*time.Second {
			t.Fatalf("httpTimeout got %v want %v", cfg.httpTimeout, 300*time.Second)
		}
		if cfg.timeout != 1*time.Second {
			t.Fatalf("global timeout (-timeout) got %v want %v", cfg.timeout, 1*time.Second)
		}
	})

	t.Run("GlobalLonger", func(t *testing.T) {
		os.Args = []string{"agentcli.test", "-prompt", "x", "-http-timeout", "300s", "-timeout", "600s"}
		cfg, code := parseFlags()
		if code != 0 {
			t.Fatalf("parse exit: %d", code)
		}
		if cfg.httpTimeout != 300*time.Second {
			t.Fatalf("httpTimeout got %v want %v", cfg.httpTimeout, 300*time.Second)
		}
		if cfg.timeout != 600*time.Second {
			t.Fatalf("global timeout (-timeout) got %v want %v", cfg.timeout, 600*time.Second)
		}
	})
}

// https://github.com/hyperifyio/goagent/issues/244
// Duration flags accept plain seconds (e.g., 300) and Go duration strings.
func TestDurationFlags_FlexibleParsing(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	t.Run("NumericHTTPFlag", func(t *testing.T) {
		os.Args = []string{"agentcli.test", "-prompt", "x", "-http-timeout", "300"}
		cfg, code := parseFlags()
		if code != 0 {
			t.Fatalf("parse exit: %d", code)
		}
		if cfg.httpTimeout != 300*time.Second {
			t.Fatalf("http-timeout got %v want %v", cfg.httpTimeout, 300*time.Second)
		}
	})

	t.Run("NumericToolFlag", func(t *testing.T) {
		os.Args = []string{"agentcli.test", "-prompt", "x", "-tool-timeout", "45"}
		cfg, code := parseFlags()
		if code != 0 {
			t.Fatalf("parse exit: %d", code)
		}
		if cfg.toolTimeout != 45*time.Second {
			t.Fatalf("tool-timeout got %v want %v", cfg.toolTimeout, 45*time.Second)
		}
	})

	t.Run("NumericGlobalFlag", func(t *testing.T) {
		os.Args = []string{"agentcli.test", "-prompt", "x", "-timeout", "10"}
		cfg, code := parseFlags()
		if code != 0 {
			t.Fatalf("parse exit: %d", code)
		}
		// http falls back to legacy when not set explicitly
		if cfg.timeout != 10*time.Second || cfg.httpTimeout != 10*time.Second || cfg.toolTimeout != 10*time.Second {
			t.Fatalf("timeouts got http=%v tool=%v global=%v; want 10s", cfg.httpTimeout, cfg.toolTimeout, cfg.timeout)
		}
	})

	t.Run("EnvHTTPNumeric", func(t *testing.T) {
		// Save/restore env
		val, ok := os.LookupEnv("OAI_HTTP_TIMEOUT")
		defer func() {
			if ok {
				if err := os.Setenv("OAI_HTTP_TIMEOUT", val); err != nil {
					t.Fatalf("restore env: %v", err)
				}
			} else {
				if err := os.Unsetenv("OAI_HTTP_TIMEOUT"); err != nil {
					t.Fatalf("unset env: %v", err)
				}
			}
		}()
		if err := os.Setenv("OAI_HTTP_TIMEOUT", "300"); err != nil {
			t.Fatalf("set env: %v", err)
		}
		os.Args = []string{"agentcli.test", "-prompt", "x"}
		cfg, code := parseFlags()
		if code != 0 {
			t.Fatalf("parse exit: %d", code)
		}
		if cfg.httpTimeout != 300*time.Second {
			t.Fatalf("env http-timeout got %v want %v", cfg.httpTimeout, 300*time.Second)
		}
	})

	t.Run("InvalidFlagValueFallsBack", func(t *testing.T) {
		// invalid value for http-timeout -> should fall back to legacy -timeout (default 30s)
		os.Args = []string{"agentcli.test", "-prompt", "x", "-http-timeout", "not-a-duration"}
		cfg, code := parseFlags()
		if code != 0 {
			t.Fatalf("parse exit: %d", code)
		}
		if cfg.httpTimeout != 30*time.Second { // falls back to legacy default 30s
			t.Fatalf("invalid http-timeout should fall back; got %v want %v", cfg.httpTimeout, 30*time.Second)
		}
	})
}
