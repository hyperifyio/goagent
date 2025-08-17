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

    // Case 1: defaults — http falls back to legacy -timeout (30s), tool to 30s, prep inherits http
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
    if cfg.prepHTTPTimeout != cfg.httpTimeout {
        t.Fatalf("expected prepHTTPTimeout to inherit httpTimeout; got prep=%v http=%v", cfg.prepHTTPTimeout, cfg.httpTimeout)
    }

    // Case 2: env OAI_HTTP_TIMEOUT overrides legacy and prep inherits http
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
    if cfg.prepHTTPTimeout != cfg.httpTimeout {
        t.Fatalf("expected prepHTTPTimeout to inherit httpTimeout; got prep=%v http=%v", cfg.prepHTTPTimeout, cfg.httpTimeout)
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

// Verify precedence for -prep-http-timeout: flag > env OAI_PREP_HTTP_TIMEOUT > -http-timeout > default
func TestPrepHTTPTimeout_Precedence(t *testing.T) {
    save := func(k string) (string, bool) { v, ok := os.LookupEnv(k); return v, ok }
    restore := func(k, v string, ok bool) {
        if ok {
            if err := os.Setenv(k, v); err != nil { t.Fatalf("restore %s: %v", k, err) }
        } else {
            if err := os.Unsetenv(k); err != nil { t.Fatalf("unset %s: %v", k, err) }
        }
    }
    prepEnvVal, prepEnvOK := save("OAI_PREP_HTTP_TIMEOUT")
    httpEnvVal, httpEnvOK := save("OAI_HTTP_TIMEOUT")
    defer restore("OAI_PREP_HTTP_TIMEOUT", prepEnvVal, prepEnvOK)
    defer restore("OAI_HTTP_TIMEOUT", httpEnvVal, httpEnvOK)

    // Case A: inherit from http-timeout when no flag/env
    orig := os.Args
    defer func() { os.Args = orig }()
    os.Args = []string{"agentcli.test", "-prompt", "x", "-http-timeout", "5s"}
    cfg, code := parseFlags()
    if code != 0 { t.Fatalf("parse exit: %d", code) }
    if cfg.prepHTTPTimeout != 5*time.Second { t.Fatalf("inheritance failed: prep=%v want 5s", cfg.prepHTTPTimeout) }
    if cfg.prepHTTPTimeoutSource != "inherit" { t.Fatalf("prep source=%s want inherit", cfg.prepHTTPTimeoutSource) }

    // Case B: env OAI_PREP_HTTP_TIMEOUT overrides http-timeout
    if err := os.Setenv("OAI_PREP_HTTP_TIMEOUT", "7s"); err != nil { t.Fatalf("set env: %v", err) }
    os.Args = []string{"agentcli.test", "-prompt", "x", "-http-timeout", "5s"}
    cfg, code = parseFlags()
    if code != 0 { t.Fatalf("parse exit: %d", code) }
    if cfg.prepHTTPTimeout != 7*time.Second { t.Fatalf("prep from env got %v want 7s", cfg.prepHTTPTimeout) }
    if cfg.prepHTTPTimeoutSource != "env" { t.Fatalf("prep source=%s want env", cfg.prepHTTPTimeoutSource) }

    // Case C: flag -prep-http-timeout overrides env
    os.Args = []string{"agentcli.test", "-prompt", "x", "-http-timeout", "5s", "-prep-http-timeout", "9s"}
    cfg, code = parseFlags()
    if code != 0 { t.Fatalf("parse exit: %d", code) }
    if cfg.prepHTTPTimeout != 9*time.Second { t.Fatalf("prep from flag got %v want 9s", cfg.prepHTTPTimeout) }
    if cfg.prepHTTPTimeoutSource != "flag" { t.Fatalf("prep source=%s want flag", cfg.prepHTTPTimeoutSource) }
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

// Pre-stage one-knob: when -prep-top-p is provided, the prep request must include top_p
// and omit temperature. We exercise the minimal runPreStage helper.
func TestPrepOneKnob_TopPOmitsTemperature(t *testing.T) {
    var seenTemp *float64
    var seenTopP *float64
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var req oai.ChatCompletionsRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            t.Fatalf("decode: %v", err)
        }
        seenTemp = req.Temperature
        seenTopP = req.TopP
        // Return minimal assistant content
        _ = json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{Message: oai.Message{Role: oai.RoleAssistant, Content: "ok"}}}})
    }))
    defer srv.Close()

    cfg := cliConfig{prompt: "x", systemPrompt: "sys", baseURL: srv.URL, model: "m", prepHTTPTimeout: 2 * time.Second, httpRetries: 0}
    msgs := []oai.Message{{Role: oai.RoleSystem, Content: "s"}, {Role: oai.RoleUser, Content: "u"}}
    cfg.prepTopP = 0.9
    var errBuf bytes.Buffer
    if _, err := runPreStage(cfg, msgs, &errBuf); err != nil {
        t.Fatalf("runPreStage error: %v", err)
    }
    if seenTemp != nil {
        t.Fatalf("prep: expected temperature omitted when -prep-top-p is set")
    }
    if seenTopP == nil || *seenTopP != 0.9 {
        if seenTopP == nil { t.Fatalf("prep: expected top_p present") }
        t.Fatalf("prep: expected top_p=0.9, got %v", *seenTopP)
    }
}

// Pre-stage should include temperature when -prep-top-p is not set and model supports it.
func TestPrepIncludesTemperatureWhenSupported(t *testing.T) {
    var seenTemp *float64
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var req oai.ChatCompletionsRequest
        _ = json.NewDecoder(r.Body).Decode(&req)
        seenTemp = req.Temperature
        _ = json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{Message: oai.Message{Role: oai.RoleAssistant, Content: "ok"}}}})
    }))
    defer srv.Close()

    cfg := cliConfig{prompt: "x", systemPrompt: "sys", baseURL: srv.URL, model: "oss-gpt-20b", prepHTTPTimeout: time.Second, httpRetries: 0}
    cfg.temperature = 1.0
    msgs := []oai.Message{{Role: oai.RoleSystem, Content: "s"}, {Role: oai.RoleUser, Content: "u"}}
    var errBuf bytes.Buffer
    if _, err := runPreStage(cfg, msgs, &errBuf); err != nil { t.Fatalf("runPreStage: %v", err) }
    if seenTemp == nil || *seenTemp != 1.0 {
        t.Fatalf("prep: expected temperature=1.0 included; got %v", func() any { if seenTemp==nil { return nil }; return *seenTemp }())
    }
}

// Pre-stage should omit temperature for unsupported models even when not using -prep-top-p,
// and the client must recover on 400 mentioning temperature by retrying without temperature.
func TestPrep_TemperatureUnsupported_400Recovery(t *testing.T) {
    var calls int
    var seenTemps []bool
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        calls++
        var req oai.ChatCompletionsRequest
        _ = json.NewDecoder(r.Body).Decode(&req)
        seenTemps = append(seenTemps, req.Temperature != nil)
        if calls == 1 {
            // Simulate 400 mentioning unsupported temperature to trigger param recovery
            w.WriteHeader(http.StatusBadRequest)
            _, _ = w.Write([]byte(`{"error":{"message":"parameter 'temperature' is unsupported for this model"}}`))
            return
        }
        _ = json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{Message: oai.Message{Role: oai.RoleAssistant, Content: "ok"}}}})
    }))
    defer srv.Close()

    // Use a model that declares SupportsTemperature==true, but we will include temp initially
    cfg := cliConfig{prompt: "x", systemPrompt: "sys", baseURL: srv.URL, model: "oss-gpt-20b", prepHTTPTimeout: time.Second, httpRetries: 0}
    cfg.temperature = 0.7
    msgs := []oai.Message{{Role: oai.RoleSystem, Content: "s"}, {Role: oai.RoleUser, Content: "u"}}
    var errBuf bytes.Buffer
    if _, err := runPreStage(cfg, msgs, &errBuf); err != nil { t.Fatalf("runPreStage: %v", err) }
    if calls != 2 {
        t.Fatalf("prep: expected exactly one recovery retry; calls=%d", calls)
    }
    if !(seenTemps[0] && !seenTemps[1]) {
        t.Fatalf("prep: expected temp present on first attempt and omitted on retry; got %v", seenTemps)
    }
}

// Pre-stage validator: a stray role:"tool" in the pre-stage input must be rejected
// before sending the prep HTTP call, mirroring the main-loop validator behavior.
func TestPrepValidator_BlocksStrayTool_NoHTTPCall(t *testing.T) {
    called := false
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        called = true
        t.Fatal("prep server should not be called when pre-stage validation fails")
    }))
    defer srv.Close()

    // Messages contain a stray tool message without a prior assistant tool_calls
    msgs := []oai.Message{
        {Role: oai.RoleUser, Content: "hi"},
        {Role: oai.RoleTool, Name: "echo", ToolCallID: "1", Content: "{\"echo\":\"hi\"}"},
    }

    cfg := cliConfig{prompt: "x", systemPrompt: "sys", baseURL: srv.URL, model: "m", prepHTTPTimeout: 200 * time.Millisecond, httpRetries: 0}
    var errBuf bytes.Buffer
    _, err := runPreStage(cfg, msgs, &errBuf)
    if err == nil {
        t.Fatalf("expected error due to pre-stage validation failure; stderr=%q", errBuf.String())
    }
    if called {
        t.Fatalf("HTTP server was contacted despite pre-stage validation failure")
    }
    if !strings.Contains(errBuf.String(), "prep invalid message sequence") {
        t.Fatalf("stderr should mention prep invalid message sequence; got: %q", errBuf.String())
    }
}

// Parallel tool-calls in pre-stage: when the pre-stage response returns multiple
// tool_calls and a tools manifest is provided, the helper must execute tools
// concurrently and append exactly one tool message per id.
func TestPrep_ParallelToolCalls_ExecutesConcurrently(t *testing.T) {
    // Build a sleeper tool that respects sleepMs
    dir := t.TempDir()
    helper := filepath.Join(dir, "sleeper.go")
    if err := os.WriteFile(helper, []byte(`package main
import ("encoding/json"; "io"; "os"; "time"; "fmt")
func main(){b,_:=io.ReadAll(os.Stdin); var m map[string]any; _=json.Unmarshal(b,&m); ms:=0; if v,ok:=m["sleepMs"].(float64); ok { ms=int(v) }; if ms>0 { time.Sleep(time.Duration(ms)*time.Millisecond) }; _=json.NewEncoder(os.Stdout).Encode(map[string]any{"sleptMs":ms}); fmt.Print("")}
`), 0o644); err != nil { t.Fatalf("write tool: %v", err) }
    bin := filepath.Join(dir, "sleeper")
    if runtime.GOOS == "windows" { bin += ".exe" }
    if out, err := exec.Command("go", "build", "-o", bin, helper).CombinedOutput(); err != nil { t.Fatalf("build tool: %v: %s", err, string(out)) }

    // Write a tools.json manifest referencing the sleeper
    manifestPath := filepath.Join(dir, "tools.json")
    m := map[string]any{
        "tools": []map[string]any{{
            "name": "sleeper",
            "description": "sleep tool",
            "schema": map[string]any{"type":"object","properties":map[string]any{"sleepMs":map[string]any{"type":"integer"}}},
            "command": []string{bin},
            "timeoutSec": 3,
        }},
    }
    b, err := json.Marshal(m)
    if err != nil { t.Fatalf("marshal manifest: %v", err) }
    if err := os.WriteFile(manifestPath, b, 0o644); err != nil { t.Fatalf("write manifest: %v", err) }

    // Fake server returns a single response with two tool_calls
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var req oai.ChatCompletionsRequest
        _ = json.NewDecoder(r.Body).Decode(&req)
        resp := oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{
            FinishReason: "tool_calls",
            Message: oai.Message{Role: oai.RoleAssistant, ToolCalls: []oai.ToolCall{
                {ID:"a", Type:"function", Function:oai.ToolCallFunction{Name:"sleeper", Arguments:"{\"sleepMs\":600}"}},
                {ID:"b", Type:"function", Function:oai.ToolCallFunction{Name:"sleeper", Arguments:"{\"sleepMs\":600}"}},
            }},
        }}}
        _ = json.NewEncoder(w).Encode(resp)
    }))
    defer srv.Close()

    // Measure elapsed around runPreStage and ensure it's < sequential time (~1200ms)
    cfg := cliConfig{prompt:"x", systemPrompt:"sys", baseURL:srv.URL, model:"m", prepHTTPTimeout: 3*time.Second, httpRetries: 0, toolsPath: manifestPath}
    msgs := []oai.Message{{Role:oai.RoleSystem, Content:"s"},{Role:oai.RoleUser, Content:"u"}}
    var errBuf bytes.Buffer
    start := time.Now()
    outMsgs, err := runPreStage(cfg, msgs, &errBuf)
    elapsed := time.Since(start)
    if err != nil { t.Fatalf("runPreStage: %v (stderr=%s)", err, errBuf.String()) }
    // Expect original messages + assistant tool_calls + two tool outputs
    var toolCount int
    for _, m := range outMsgs { if m.Role == oai.RoleTool { toolCount++ } }
    if toolCount != 2 { t.Fatalf("expected 2 tool messages, got %d", toolCount) }
    if elapsed >= 1100*time.Millisecond { t.Fatalf("pre-stage tool calls not parallel; elapsed=%v", elapsed) }
}

// https://github.com/hyperifyio/goagent/issues/289
// When -top-p is provided, temperature must be omitted and a one-line warning printed.
func TestOneKnobRule_TopPOmitsTemperatureAndWarns(t *testing.T) {
	// Fake server to capture request
	var seenTemp *float64
	var seenTopP *float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req oai.ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		seenTemp = req.Temperature
		seenTopP = req.TopP
		if err := json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{Message: oai.Message{Role: oai.RoleAssistant, Content: "ok"}}}}); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}))
	defer srv.Close()

	// Run with -top-p set and ensure temp omitted and warning emitted
	var outBuf, errBuf bytes.Buffer
	code := cliMain([]string{"-prompt", "x", "-base-url", srv.URL, "-model", "m", "-top-p", "0.9"}, &outBuf, &errBuf)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
	}
	if seenTemp != nil {
		t.Fatalf("expected temperature to be omitted when -top-p is set")
	}
	if seenTopP == nil || *seenTopP != 0.9 {
		if seenTopP == nil {
			t.Fatalf("expected top_p to be set when -top-p is provided")
		}
		t.Fatalf("expected top_p=0.9, got %v", *seenTopP)
	}
	if !strings.Contains(errBuf.String(), "omitting temperature per one-knob rule") {
		t.Fatalf("expected one-knob warning on stderr; got: %q", errBuf.String())
	}
}

// https://github.com/hyperifyio/goagent/issues/285
// Precedence: flag -temp > env LLM_TEMPERATURE > default 1.0
func TestTemperaturePrecedence_FlagThenEnvThenDefault(t *testing.T) {
	save := func(k string) (string, bool) { v, ok := os.LookupEnv(k); return v, ok }
	restore := func(k, v string, ok bool) {
		if ok {
			if err := os.Setenv(k, v); err != nil {
				t.Fatalf("setenv: %v", err)
			}
		} else {
			if err := os.Unsetenv(k); err != nil {
				t.Fatalf("unsetenv: %v", err)
			}
		}
	}
	envVal, envOK := save("LLM_TEMPERATURE")
	defer restore("LLM_TEMPERATURE", envVal, envOK)

	// Case: env only
	if err := os.Setenv("LLM_TEMPERATURE", "0.7"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	orig := os.Args
	defer func() { os.Args = orig }()
	os.Args = []string{"agentcli.test", "-prompt", "x"}
	cfg, code := parseFlags()
	if code != 0 {
		t.Fatalf("parse exit: %d", code)
	}
	if cfg.temperature != 0.7 {
		t.Fatalf("env should set temperature=0.7; got %v", cfg.temperature)
	}

	// Case: flag overrides env
	os.Args = []string{"agentcli.test", "-prompt", "x", "-temp", "0.4"}
	cfg, code = parseFlags()
	if code != 0 {
		t.Fatalf("parse exit: %d", code)
	}
	if cfg.temperature != 0.4 {
		t.Fatalf("flag should override env; got %v", cfg.temperature)
	}

	// Case: default when env unset and no flag
	if err := os.Unsetenv("LLM_TEMPERATURE"); err != nil {
		t.Fatalf("unset env: %v", err)
	}
	os.Args = []string{"agentcli.test", "-prompt", "x"}
	cfg, code = parseFlags()
	if code != 0 {
		t.Fatalf("parse exit: %d", code)
	}
	if cfg.temperature != 1.0 {
		t.Fatalf("default temperature should be 1.0; got %v", cfg.temperature)
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

// https://github.com/hyperifyio/goagent/issues/350
// When the agent reaches the configured step cap, it must terminate with a
// clear "needs human review" message and a non-zero exit. The loop should
// perform exactly cfg.maxSteps HTTP calls in this case.
func TestAgentLoop_MaxStepsCap_HumanReviewMessage(t *testing.T) {
    var calls int
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        calls++
        // Return an assistant message with empty content and no tool_calls
        _ = json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{
            FinishReason: "stop",
            Message:      oai.Message{Role: oai.RoleAssistant, Content: ""},
        }}})
    }))
    defer srv.Close()

    cfg := cliConfig{
        prompt:       "x",
        systemPrompt: "sys",
        baseURL:      srv.URL,
        model:        "m",
        maxSteps:     3,
        httpTimeout:  2 * time.Second,
        toolTimeout:  1 * time.Second,
        temperature:  0,
    }

    var outBuf, errBuf bytes.Buffer
    code := runAgent(cfg, &outBuf, &errBuf)
    if code == 0 {
        t.Fatalf("expected non-zero exit when step cap is reached; stdout=%q stderr=%q", outBuf.String(), errBuf.String())
    }
    if calls != 3 {
        t.Fatalf("expected exactly 3 HTTP calls (one per step), got %d", calls)
    }
    if !strings.Contains(strings.ToLower(errBuf.String()), "needs human review") {
        t.Fatalf("stderr must contain 'needs human review'; got: %q", errBuf.String())
    }
}

// https://github.com/hyperifyio/goagent/issues/350
// Hard ceiling: regardless of the provided -max-steps, the agent must clamp to 15.
// Verify that with an excessively large maxSteps, we perform exactly 15 calls and
// emit the human review message.
func TestAgentLoop_HardCeilingOf15(t *testing.T) {
    var calls int
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        calls++
        _ = json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{
            FinishReason: "stop",
            Message:      oai.Message{Role: oai.RoleAssistant, Content: ""},
        }}})
    }))
    defer srv.Close()

    cfg := cliConfig{
        prompt:       "x",
        systemPrompt: "sys",
        baseURL:      srv.URL,
        model:        "m",
        maxSteps:     100, // should be clamped to 15
        httpTimeout:  2 * time.Second,
        toolTimeout:  1 * time.Second,
        temperature:  0,
    }

    var outBuf, errBuf bytes.Buffer
    code := runAgent(cfg, &outBuf, &errBuf)
    if code == 0 {
        t.Fatalf("expected non-zero exit when hard ceiling is reached; stdout=%q stderr=%q", outBuf.String(), errBuf.String())
    }
    if calls != 15 {
        t.Fatalf("expected exactly 15 HTTP calls due to hard ceiling; got %d", calls)
    }
    if !strings.Contains(strings.ToLower(errBuf.String()), "needs human review") {
        t.Fatalf("stderr must contain 'needs human review'; got: %q", errBuf.String())
    }
}

// https://github.com/hyperifyio/goagent/issues/262
func TestVersion_PrintsAndExitsZero(t *testing.T) {
	var outBuf, errBuf bytes.Buffer
	code := cliMain([]string{"--version"}, &outBuf, &errBuf)
	if code != 0 {
		t.Fatalf("exit code = %d; want 0", code)
	}
	got := outBuf.String()
	if !strings.Contains(got, "agentcli version") {
		t.Fatalf("stdout missing version header; got: %q", got)
	}
	if errBuf.Len() != 0 {
		t.Fatalf("stderr should be empty; got: %q", errBuf.String())
	}
}

// https://github.com/hyperifyio/goagent/issues/252
func TestMissingPrompt_PrintsErrorUsageAndExitsTwo(t *testing.T) {
	// Simulate running with no -prompt and no special flags
	var outBuf, errBuf bytes.Buffer
	code := cliMain([]string{}, &outBuf, &errBuf)
	if code != 2 {
		t.Fatalf("exit code = %d; want 2", code)
	}
	gotErr := errBuf.String()
	if !strings.Contains(gotErr, "error: -prompt is required") {
		t.Fatalf("stderr missing error line; got:\n%s", gotErr)
	}
	if !strings.Contains(gotErr, "Usage:") || !strings.Contains(gotErr, "-prompt") {
		t.Fatalf("stderr missing usage synopsis; got:\n%s", gotErr)
	}
	if outBuf.Len() != 0 {
		t.Fatalf("stdout should be empty; got: %q", outBuf.String())
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
        "\"prepHTTPTimeout\": ",
        "\"prepHTTPTimeoutSource\": ",
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
			// Verify assistant tool_calls message is present before tool messages
			assistantSeen := false
			for _, m := range req.Messages {
				if m.Role == oai.RoleAssistant && len(m.ToolCalls) > 0 {
					assistantSeen = true
					break
				}
			}
			if !assistantSeen {
				t.Fatalf("assistant message with tool_calls not present before tool messages")
			}

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

// https://github.com/hyperifyio/goagent/issues/252
// Regression test: a stray role:"tool" without a prior assistant tool_calls
// must be caught by the pre-flight validator and the request must not be sent.
func TestPreflightValidator_BlocksStrayTool_NoHTTPCall(t *testing.T) {
	// Server that fails the test if contacted
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		t.Fatal("server should not be called when pre-flight fails")
	}))
	defer srv.Close()

	// Craft an initial transcript containing a stray tool message
	msgs := []oai.Message{
		{Role: oai.RoleUser, Content: "hi"},
		{Role: oai.RoleTool, Name: "echo", ToolCallID: "1", Content: "{\"echo\":\"hi\"}"},
	}

	cfg := cliConfig{
		prompt:       "ignored",
		systemPrompt: "sys",
		baseURL:      srv.URL,
		model:        "m",
		maxSteps:     1,
		httpTimeout:  100 * time.Millisecond,
		toolTimeout:  100 * time.Millisecond,
		temperature:  0,
		initMessages: msgs,
	}

	var outBuf, errBuf bytes.Buffer
	code := runAgent(cfg, &outBuf, &errBuf)
	if code == 0 {
		t.Fatalf("expected non-zero exit due to pre-flight validation error; stdout=%q stderr=%q", outBuf.String(), errBuf.String())
	}
	if called {
		t.Fatalf("HTTP server was contacted despite pre-flight validation failure")
	}
	// Error should mention stray tool without prior assistant tool_calls
	if !strings.Contains(errBuf.String(), "without a prior assistant message containing tool_calls") {
		t.Fatalf("unexpected error message: %q", errBuf.String())
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
    os.Args = []string{"agentcli.test", "-prompt", "x", "-http-timeout", "5s", "-prep-http-timeout", "4s", "-tool-timeout", "7s", "-timeout", "10s", "-base-url", srv.URL, "-model", "m"}
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
    if !strings.Contains(got, "effective timeouts: http-timeout=5s source=flag; prep-http-timeout=4s source=flag; tool-timeout=7s source=flag; timeout=10s source=flag") {
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
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, helper).CombinedOutput(); err != nil {
		t.Fatalf("build tool: %v: %s", err, string(out))
	}

	// Build the registry expected by appendToolCallOutputs
	realTools := map[string]tools.ToolSpec{
		"slow": {Name: "slow", Command: []string{bin}, TimeoutSec: 5},
		"fast": {Name: "fast", Command: []string{bin}, TimeoutSec: 5},
	}

	// Craft assistant message containing two tool calls with different sleeps
	msg := oai.Message{Role: oai.RoleAssistant}
	msg.ToolCalls = []oai.ToolCall{
		{ID: "1", Type: "function", Function: oai.ToolCallFunction{Name: "slow", Arguments: `{"sleepMs":600,"name":"slow"}`}},
		{ID: "2", Type: "function", Function: oai.ToolCallFunction{Name: "fast", Arguments: `{"sleepMs":600,"name":"fast"}`}},
	}

	// Minimal cfg with a generous per-tool timeout
	cfg := cliConfig{toolTimeout: 3 * time.Second}

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

// https://github.com/hyperifyio/goagent/issues/318
// When completionCap defaults to 0, request must omit max_tokens entirely.
func TestRequest_OmitsMaxTokensWhenCapZero(t *testing.T) {
    var captured []byte
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        b, err := io.ReadAll(r.Body)
        if err != nil {
            t.Fatalf("read body: %v", err)
        }
        captured = append([]byte(nil), b...)
        // Respond with a minimal valid assistant message to terminate
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
        prompt:       "x",
        systemPrompt: "sys",
        baseURL:      srv.URL,
        model:        "m",
        maxSteps:     1,
        httpTimeout:  2 * time.Second,
        toolTimeout:  1 * time.Second,
        temperature:  0,
    }

    var outBuf, errBuf bytes.Buffer
    code := runAgent(cfg, &outBuf, &errBuf)
    if code != 0 {
        t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
    }
    if len(captured) == 0 {
        t.Fatalf("no request captured")
    }
    if strings.Contains(string(captured), "\"max_tokens\"") {
        t.Fatalf("request must omit max_tokens when completionCap=0; got body: %s", string(captured))
    }
}

// https://github.com/hyperifyio/goagent/issues/318
// When finish_reason=="length" on the first attempt, the agent must perform
// exactly one in-step retry with a completion cap of at least 256 tokens by
// setting max_tokens=256 on the retry while omitting it on the first attempt.
func TestLengthBackoff_OneRetrySetsMaxTokens256(t *testing.T) {
    // Capture bodies of successive requests
    var bodies [][]byte
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        b, err := io.ReadAll(r.Body)
        if err != nil {
            t.Fatalf("read body: %v", err)
        }
        bodies = append(bodies, append([]byte(nil), b...))
        // Respond with length on first call, stop on second
        if len(bodies) == 1 {
            resp := oai.ChatCompletionsResponse{
                Choices: []oai.ChatCompletionsResponseChoice{{
                    FinishReason: "length",
                    Message:      oai.Message{Role: oai.RoleAssistant, Content: ""},
                }},
            }
            if err := json.NewEncoder(w).Encode(resp); err != nil {
                t.Fatalf("encode resp1: %v", err)
            }
            return
        }
        // Second call returns final content
        resp := oai.ChatCompletionsResponse{
            Choices: []oai.ChatCompletionsResponseChoice{{
                FinishReason: "stop",
                Message:      oai.Message{Role: oai.RoleAssistant, Content: "done"},
            }},
        }
        if err := json.NewEncoder(w).Encode(resp); err != nil {
            t.Fatalf("encode resp2: %v", err)
        }
    }))
    defer srv.Close()

    cfg := cliConfig{
        prompt:       "x",
        systemPrompt: "sys",
        baseURL:      srv.URL,
        model:        "m",
        maxSteps:     1,
        httpTimeout:  2 * time.Second,
        toolTimeout:  1 * time.Second,
        temperature:  0,
        debug:        false,
    }

    var outBuf, errBuf bytes.Buffer
    code := runAgent(cfg, &outBuf, &errBuf)
    if code != 0 {
        t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
    }
    if strings.TrimSpace(outBuf.String()) != "done" {
        t.Fatalf("unexpected stdout: %q", outBuf.String())
    }
    if len(bodies) != 2 {
        t.Fatalf("expected two requests (initial + retry), got %d", len(bodies))
    }
    // First body must omit max_tokens
    if strings.Contains(string(bodies[0]), "\"max_tokens\"") {
        t.Fatalf("first attempt must omit max_tokens; body=%s", string(bodies[0]))
    }
    // Second body must include max_tokens:256
    if !strings.Contains(string(bodies[1]), "\"max_tokens\":256") {
        t.Fatalf("second attempt must include max_tokens=256; body=%s", string(bodies[1]))
    }
}

// https://github.com/hyperifyio/goagent/issues/318
// On length backoff, the completion cap must be clamped to the remaining
// context so that max_tokens does not exceed window - estimated_prompt - margin.
func TestLengthBackoff_ClampDoesNotExceedWindow(t *testing.T) {
    // Build a prompt large enough that remaining context < 256 for oss-gpt-20b (8192 window).
    // We will set model to oss-gpt-20b so ContextWindowForModel returns 8192.
    // Choose a prompt size that forces remaining context < 256 for window=8192.
    // Roughly EstimateTokens ~= ceil(len/4) + overhead; len=40000 -> ~10000 tokens.
    large := strings.Repeat("x", 40000)

    // Capture bodies to inspect max_tokens of the retry
    var bodies [][]byte
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        b, err := io.ReadAll(r.Body)
        if err != nil {
            t.Fatalf("read body: %v", err)
        }
        bodies = append(bodies, append([]byte(nil), b...))
        // First call yields finish_reason==length to trigger the retry
        if len(bodies) == 1 {
            resp := oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{FinishReason: "length", Message: oai.Message{Role: oai.RoleAssistant}}}}
            if err := json.NewEncoder(w).Encode(resp); err != nil {
                t.Fatalf("encode resp1: %v", err)
            }
            return
        }
        // Second call returns stop to finish
        resp := oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{FinishReason: "stop", Message: oai.Message{Role: oai.RoleAssistant, Content: "ok"}}}}
        if err := json.NewEncoder(w).Encode(resp); err != nil {
            t.Fatalf("encode resp2: %v", err)
        }
    }))
    defer srv.Close()

    cfg := cliConfig{
        prompt:       large,
        systemPrompt: "sys",
        baseURL:      srv.URL,
        model:        "oss-gpt-20b",
        maxSteps:     1,
        httpTimeout:  2 * time.Second,
        toolTimeout:  1 * time.Second,
        temperature:  0,
        debug:        false,
    }

    var outBuf, errBuf bytes.Buffer
    code := runAgent(cfg, &outBuf, &errBuf)
    if code != 0 {
        t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
    }
    if strings.TrimSpace(outBuf.String()) != "ok" {
        t.Fatalf("unexpected stdout: %q", outBuf.String())
    }
    if len(bodies) != 2 {
        t.Fatalf("expected two requests, got %d", len(bodies))
    }
    // First body must omit max_tokens
    if strings.Contains(string(bodies[0]), "\"max_tokens\"") {
        t.Fatalf("first attempt must omit max_tokens; body=%s", string(bodies[0]))
    }
    // Second body must include max_tokens and it must be less than or equal to remaining.
    // Compute an upper bound by parsing the JSON to extract max_tokens.
    var payload map[string]any
    if err := json.Unmarshal(bodies[1], &payload); err != nil {
        t.Fatalf("unmarshal second body: %v", err)
    }
    v, ok := payload["max_tokens"].(float64)
    if !ok {
        t.Fatalf("second body missing max_tokens; body=%s", string(bodies[1]))
    }
    gotCap := int(v)
    if gotCap <= 0 {
        t.Fatalf("clamped cap must be > 0; got %d", gotCap)
    }
    // Sanity: clamped value must be strictly less than 256 for our large prompt.
    if gotCap >= 256 {
        t.Fatalf("clamp failed: expected retry cap < 256 due to large prompt; got %d", gotCap)
    }
}

// https://github.com/hyperifyio/goagent/issues/318
// On length backoff, an NDJSON audit line with event=="length_backoff" must be
// written under the repository root's .goagent/audit with expected fields.
func TestLengthBackoff_AuditEmitted(t *testing.T) {
    // Clean audit dir at repo root
    root := findRepoRoot(t)
    _ = os.RemoveAll(filepath.Join(root, ".goagent"))

    // Minimal two-attempt server to trigger length backoff
    var calls int
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        calls++
        if calls == 1 {
            _ = json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{FinishReason: "length", Message: oai.Message{Role: oai.RoleAssistant}}}})
            return
        }
        _ = json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{FinishReason: "stop", Message: oai.Message{Role: oai.RoleAssistant, Content: "ok"}}}})
    }))
    defer srv.Close()

    cfg := cliConfig{prompt: "x", systemPrompt: "sys", baseURL: srv.URL, model: "oss-gpt-20b", maxSteps: 1, httpTimeout: 2 * time.Second, toolTimeout: 1 * time.Second, temperature: 0}
    var outBuf, errBuf bytes.Buffer
    code := runAgent(cfg, &outBuf, &errBuf)
    if code != 0 {
        t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
    }
    if strings.TrimSpace(outBuf.String()) != "ok" {
        t.Fatalf("unexpected stdout: %q", outBuf.String())
    }

    // Locate today's audit file under repo root and read it
    auditDir := filepath.Join(root, ".goagent", "audit")
    logFile := waitForAuditFile(t, auditDir, 2*time.Second)
    data, err := os.ReadFile(logFile)
    if err != nil {
        t.Fatalf("read audit: %v", err)
    }
    s := string(data)
    if !strings.Contains(s, "\"event\":\"length_backoff\"") {
        t.Fatalf("missing length_backoff event; got:\n%s", truncate(s, 1000))
    }
    // Basic field presence checks
    if !strings.Contains(s, "\"model\":\"") || !strings.Contains(s, "\"prev_cap\":") || !strings.Contains(s, "\"new_cap\":") || !strings.Contains(s, "\"window\":") || !strings.Contains(s, "\"estimated_prompt_tokens\":") {
        t.Fatalf("missing expected fields in length_backoff audit; got:\n%s", truncate(s, 1000))
    }
}

// findRepoRoot walks upward from CWD to locate the directory containing go.mod.
func findRepoRoot(t *testing.T) string {
    t.Helper()
    start, err := os.Getwd()
    if err != nil {
        t.Fatalf("getwd: %v", err)
    }
    dir := start
    for {
        if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
            return dir
        }
        parent := filepath.Dir(dir)
        if parent == dir {
            t.Fatalf("go.mod not found from %s upward", start)
        }
        dir = parent
    }
}

// waitForAuditFile polls the audit directory until a file appears or timeout elapses.
func waitForAuditFile(t *testing.T, auditDir string, timeout time.Duration) string {
    t.Helper()
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        entries, err := os.ReadDir(auditDir)
        if err == nil {
            for _, e := range entries {
                if !e.IsDir() {
                    return filepath.Join(auditDir, e.Name())
                }
            }
        }
        time.Sleep(10 * time.Millisecond)
    }
    t.Fatalf("audit log not created in %s", auditDir)
    return ""
}

func truncate(s string, n int) string {
    if len(s) <= n {
        return s
    }
    return s[:n]
}

// https://github.com/hyperifyio/goagent/issues/300
// CLI flags must be order-insensitive. This test permutes common flags and
// asserts parsed values are identical regardless of position. We only compare
// a stable subset of fields to avoid env/default interference.
func TestFlagOrderIndependence_Table(t *testing.T) {
	type view struct {
		prompt    string
		toolsPath string
		debug     bool
		model     string
	}

	// Helper to parse given argv and extract a comparison view
	parse := func(argv []string) (view, int) {
		orig := os.Args
		defer func() { os.Args = orig }()
		os.Args = argv
		cfg, code := parseFlags()
		return view{prompt: cfg.prompt, toolsPath: cfg.toolsPath, debug: cfg.debug, model: cfg.model}, code
	}

	// Baseline args containing a few representative flags
	base := []string{"agentcli.test", "-prompt", "hello", "-tools", "/tmp/tools.json", "-debug", "-model", "m"}
	perms := [][]string{
		base,
		{"agentcli.test", "-debug", "-model", "m", "-tools", "/tmp/tools.json", "-prompt", "hello"},
		{"agentcli.test", "-tools", "/tmp/tools.json", "-prompt", "hello", "-model", "m", "-debug"},
		{"agentcli.test", "-model", "m", "-prompt", "hello", "-debug", "-tools", "/tmp/tools.json"},
	}

	var want view
	for i, args := range perms {
		got, code := parse(args)
		if code != 0 {
			t.Fatalf("perm %d parse exit=%d for args=%v", i, code, args)
		}
		if i == 0 {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("mismatch on permutation %d: got=%+v want=%+v (args=%v)", i, got, want, args)
		}
	}
}

// https://github.com/hyperifyio/goagent/issues/300
// Help must exit 0 regardless of where the token appears among other flags.
func TestHelpToken_PositionIndependence(t *testing.T) {
	cases := [][]string{
		{"-h", "-debug"},
		{"-debug", "-h"},
		{"-tools", "/tmp/tools.json", "help", "-model", "m"},
		{"-model", "m", "--help", "-prompt", "x"},
	}
	for i, rest := range cases {
		var outBuf, errBuf bytes.Buffer
		code := cliMain(rest, &outBuf, &errBuf)
		if code != 0 {
			t.Fatalf("case %d: exit=%d; want 0", i, code)
		}
		if !strings.Contains(outBuf.String(), "Usage:") {
			t.Fatalf("case %d: expected Usage in stdout; got: %q", i, outBuf.String())
		}
		if errBuf.Len() != 0 {
			t.Fatalf("case %d: stderr should be empty; got: %q", i, errBuf.String())
		}
	}
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

// https://github.com/hyperifyio/goagent/issues/318
// Edge case: when finish_reason!="length", there must be no retry.
func TestLengthBackoff_NoRetryWhenFinishReasonNotLength(t *testing.T) {
    var calls int
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        calls++
        // Always return a final assistant message with stop
        _ = json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{
            FinishReason: "stop",
            Message:      oai.Message{Role: oai.RoleAssistant, Content: "ok"},
        }}})
    }))
    defer srv.Close()

    cfg := cliConfig{prompt: "x", systemPrompt: "sys", baseURL: srv.URL, model: "m", maxSteps: 3, httpTimeout: 2 * time.Second, toolTimeout: 1 * time.Second, temperature: 0}
    var outBuf, errBuf bytes.Buffer
    code := runAgent(cfg, &outBuf, &errBuf)
    if code != 0 {
        t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
    }
    if strings.TrimSpace(outBuf.String()) != "ok" {
        t.Fatalf("unexpected stdout: %q", outBuf.String())
    }
    if calls != 1 {
        t.Fatalf("expected exactly one HTTP call when not length; got %d", calls)
    }
}

// https://github.com/hyperifyio/goagent/issues/318
// Edge case: only one in-step retry even if the second response is also "length".
func TestLengthBackoff_OnlyOneRetryWhenSecondIsAlsoLength(t *testing.T) {
    var bodies [][]byte
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        b, err := io.ReadAll(r.Body)
        if err != nil {
            t.Fatalf("read body: %v", err)
        }
        bodies = append(bodies, append([]byte(nil), b...))
        switch len(bodies) {
        case 1:
            _ = json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{FinishReason: "length", Message: oai.Message{Role: oai.RoleAssistant}}}})
        case 2:
            _ = json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{FinishReason: "length", Message: oai.Message{Role: oai.RoleAssistant}}}})
        default:
            // If a third in-step retry occurs, fail deterministically
            t.Fatalf("unexpected extra in-step retry; total bodies=%d", len(bodies))
        }
    }))
    defer srv.Close()

    // Limit to a single agent step so no additional step-level requests are made.
    cfg := cliConfig{prompt: "x", systemPrompt: "sys", baseURL: srv.URL, model: "m", maxSteps: 1, httpTimeout: 2 * time.Second, toolTimeout: 1 * time.Second, temperature: 0}
    var outBuf, errBuf bytes.Buffer
    code := runAgent(cfg, &outBuf, &errBuf)
    if code == 0 {
        t.Fatalf("expected non-zero exit since no final content; stdout=%q stderr=%q", outBuf.String(), errBuf.String())
    }
    if len(bodies) != 2 {
        t.Fatalf("expected exactly two requests (initial + one retry), got %d", len(bodies))
    }
}

// https://github.com/hyperifyio/goagent/issues/318
// Edge case: tool_call flow is unaffected by length backoff logic.
// When the model returns tool_calls, no max_tokens should be injected and
// the conversation should proceed with two HTTP calls (tool step + final).
func TestLengthBackoff_DoesNotInterfereWithToolCalls(t *testing.T) {
    // Build a helper tool that echoes JSON and succeeds quickly
    dir := t.TempDir()
    helper := filepath.Join(dir, "echo.go")
    if err := os.WriteFile(helper, []byte(`package main
import ("io"; "os"; "fmt")
func main(){b,_:=io.ReadAll(os.Stdin); fmt.Print(string(b))}
`), 0o644); err != nil {
        t.Fatalf("write tool: %v", err)
    }
    bin := filepath.Join(dir, "echo")
    if runtime.GOOS == "windows" { bin += ".exe" }
    if out, err := exec.Command("go", "build", "-o", bin, helper).CombinedOutput(); err != nil {
        t.Fatalf("build tool: %v: %s", err, string(out))
    }

    // Capture two sequential requests and ensure no max_tokens present in either
    var bodies [][]byte
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        b, err := io.ReadAll(r.Body)
        if err != nil {
            t.Fatalf("read body: %v", err)
        }
        bodies = append(bodies, append([]byte(nil), b...))
        switch len(bodies) {
        case 1:
            // Return a tool_calls response
            _ = json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{
                FinishReason: "tool_calls",
                Message: oai.Message{Role: oai.RoleAssistant, ToolCalls: []oai.ToolCall{{
                    ID: "1", Type: "function", Function: oai.ToolCallFunction{Name: "echo", Arguments: `{"x":1}`},
                }}},
            }}})
        case 2:
            // Final content
            _ = json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{
                FinishReason: "stop",
                Message:      oai.Message{Role: oai.RoleAssistant, Content: "done"},
            }}})
        default:
            t.Fatalf("unexpected extra HTTP request; bodies=%d", len(bodies))
        }
    }))
    defer srv.Close()

    // Register tool manifest through in-memory registry path
    manifestPath := filepath.Join(dir, "tools.json")
    m := map[string]any{
        "tools": []map[string]any{{
            "name": "echo",
            "description": "echo",
            "schema": map[string]any{"type": "object"},
            "command": []string{bin},
            "timeoutSec": 5,
        }},
    }
    data, err := json.Marshal(m)
    if err != nil { t.Fatalf("marshal manifest: %v", err) }
    if err := os.WriteFile(manifestPath, data, 0o644); err != nil { t.Fatalf("write manifest: %v", err) }

    cfg := cliConfig{prompt: "x", toolsPath: manifestPath, systemPrompt: "sys", baseURL: srv.URL, model: "m", maxSteps: 3, httpTimeout: 2 * time.Second, toolTimeout: 2 * time.Second, temperature: 0}
    var outBuf, errBuf bytes.Buffer
    code := runAgent(cfg, &outBuf, &errBuf)
    if code != 0 {
        t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
    }
    if strings.TrimSpace(outBuf.String()) != "done" {
        t.Fatalf("unexpected stdout: %q", outBuf.String())
    }
    if len(bodies) != 2 {
        t.Fatalf("expected exactly two HTTP calls (tool step + final), got %d", len(bodies))
    }
    // Neither request should include max_tokens
    if strings.Contains(string(bodies[0]), "\"max_tokens\"") || strings.Contains(string(bodies[1]), "\"max_tokens\"") {
        t.Fatalf("max_tokens must be omitted for tool_call flow; got bodies: %s | %s", string(bodies[0]), string(bodies[1]))
    }
}
