//nolint:errcheck // Tests intentionally ignore some error returns for brevity; behavior validated via assertions.
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	oai "github.com/hyperifyio/goagent/internal/oai"
)

// TestGPT5_MockSmoke_DefaultTemperature asserts that when targeting a GPT-5
// model with no sampling flags, the request includes temperature 1.0 by default.
// It uses a mock OpenAI-compatible endpoint to capture the request payload.
func TestGPT5_MockSmoke_DefaultTemperature(t *testing.T) {
	var seen oai.ChatCompletionsRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode: %v", err)
		}
		_ = json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{Message: oai.Message{Role: oai.RoleAssistant, Content: "ok"}}}})
	}))
	defer srv.Close()

	var outBuf, errBuf bytes.Buffer
	code := cliMain([]string{
		"-prompt", "Say ok",
		"-base-url", srv.URL,
		"-model", "gpt-5",
		"-max-steps", "1",
	}, &outBuf, &errBuf)
	if code != 0 {
		t.Fatalf("cli exit=%d stderr=%s", code, errBuf.String())
	}
	if seen.Temperature == nil || *seen.Temperature != 1.0 {
		if seen.Temperature == nil {
			t.Fatalf("expected temperature in request; want 1.0")
		}
		t.Fatalf("temperature got %v want 1.0", *seen.Temperature)
	}
}

// TestGPT5_MockSmoke_ReasoningControlsIndependence simulates toggling vendor-specific
// reasoning controls via environment variables and verifies temperature remains 1.0.
// The agent currently ignores these envs by design; this test protects independence.
func TestGPT5_MockSmoke_ReasoningControlsIndependence(t *testing.T) {
	// Save and restore environment variables used in this test
	save := func(k string) (string, bool) { v, ok := os.LookupEnv(k); return v, ok }
	restore := func(k, v string, ok bool) {
		if ok {
			_ = os.Setenv(k, v)
		} else {
			_ = os.Unsetenv(k)
		}
	}

	// First run: baseline
	var baseline oai.ChatCompletionsRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req oai.ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		// On first call record baseline; on second call record override
		if baseline.Model == "" {
			baseline = req
		} else {
			// Overwrite baseline for clarity in failure messages
			baseline = req
		}
		_ = json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{Message: oai.Message{Role: oai.RoleAssistant, Content: "ok"}}}})
	}))
	defer srv.Close()

	var out1, err1 bytes.Buffer
	code := cliMain([]string{"-prompt", "x", "-base-url", srv.URL, "-model", "gpt-5", "-max-steps", "1"}, &out1, &err1)
	if code != 0 {
		t.Fatalf("baseline exit=%d err=%s", code, err1.String())
	}
	if baseline.Temperature == nil || *baseline.Temperature != 1.0 {
		t.Fatalf("baseline temperature got %v want 1.0", ptrToString(baseline.Temperature))
	}

	// Second run: toggle hypothetical reasoning controls via env vars
	v0, ok0 := save("GPT5_VERBOSITY")
	v1, ok1 := save("GPT5_REASONING_EFFORT")
	defer restore("GPT5_VERBOSITY", v0, ok0)
	defer restore("GPT5_REASONING_EFFORT", v1, ok1)
	_ = os.Setenv("GPT5_VERBOSITY", "high")
	_ = os.Setenv("GPT5_REASONING_EFFORT", "medium")

	var seen oai.ChatCompletionsRequest
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatalf("decode: %v", err)
		}
		_ = json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{Message: oai.Message{Role: oai.RoleAssistant, Content: "ok"}}}})
	}))
	defer srv2.Close()

	var out2, err2 bytes.Buffer
	code = cliMain([]string{"-prompt", "x", "-base-url", srv2.URL, "-model", "gpt-5", "-max-steps", "1"}, &out2, &err2)
	if code != 0 {
		t.Fatalf("env-toggle exit=%d err=%s", code, err2.String())
	}
	if seen.Temperature == nil || *seen.Temperature != 1.0 {
		t.Fatalf("with reasoning envs, temperature got %v want 1.0", ptrToString(seen.Temperature))
	}
}

func ptrToString(p *float64) string {
	if p == nil {
		return "<nil>"
	}
	// avoid fmt import; simple formatting
	s := strings.TrimRight(strings.TrimRight(jsonNumber(*p), "0"), ".")
	if s == "" {
		return "0"
	}
	return s
}

// jsonNumber renders a float with JSON rules for test messages.
func jsonNumber(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}
