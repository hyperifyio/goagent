//nolint:errcheck // In tests, many helper writes/encodes ignore errors intentionally; functional behavior is asserted elsewhere.
package oai

import (
	"context"
	"encoding/json"
	"errors"
	mathrand "math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// https://github.com/hyperifyio/goagent/issues/1
func TestCreateChatCompletion_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var req ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("bad json: %v", err)
		}
		resp := ChatCompletionsResponse{
			ID:      "cmpl-1",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []ChatCompletionsResponseChoice{{
				Index:        0,
				FinishReason: "stop",
				Message:      Message{Role: RoleAssistant, Content: "hello"},
			}},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			panic(err)
		}
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "", 2*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := c.CreateChatCompletion(ctx, ChatCompletionsRequest{Model: "test", Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Choices) != 1 || out.Choices[0].Message.Content != "hello" {
		t.Fatalf("unexpected response: %+v", out)
	}
}

// https://github.com/hyperifyio/goagent/issues/1
func TestCreateChatCompletion_HTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		if _, err := w.Write([]byte(`{"error":"bad request"}`)); err != nil {
			panic(err)
		}
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "", 2*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := c.CreateChatCompletion(ctx, ChatCompletionsRequest{Model: "x", Messages: []Message{}})
	if err == nil {
		t.Fatalf("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "400") || !strings.Contains(got, "bad request") {
		t.Fatalf("expected status code and body in error, got: %v", got)
	}
}

// https://github.com/hyperifyio/goagent/issues/1
// Ensure encoder omits temperature when SupportsTemperature == false and includes when true.
func TestCreateChatCompletion_TemperatureOmissionAndInclusion(t *testing.T) {
	t.Run("OmitWhenUnsupported", func(t *testing.T) {
		// Model id that does not support temperature per capabilities
		req := ChatCompletionsRequest{Model: "o3-mini", Messages: []Message{{Role: RoleUser, Content: "x"}}}
		b, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		got := string(b)
		if strings.Contains(got, "temperature") {
			t.Fatalf("expected no temperature field, got: %s", got)
		}
	})

	t.Run("IncludeWhenSupported", func(t *testing.T) {
		temp := 0.7
		req := ChatCompletionsRequest{Model: "oss-gpt-20b", Messages: []Message{{Role: RoleUser, Content: "x"}}, Temperature: &temp}
		b, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		got := string(b)
		if !strings.Contains(got, "\"temperature\":0.7") {
			t.Fatalf("expected temperature field, got: %s", got)
		}
	})
}

// Ensure client strips temperature for unsupported models right before HTTP.
func TestCreateChatCompletion_TemperatureStrippedWhenUnsupported(t *testing.T) {
	// Spin up a server that captures incoming request JSON
	var seenTemp *float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var req ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		seenTemp = req.Temperature
		// Respond with minimal valid JSON
		resp := ChatCompletionsResponse{Choices: []ChatCompletionsResponseChoice{{Message: Message{Role: RoleAssistant, Content: "ok"}}}}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClientWithRetry(srv.URL, "", 2*time.Second, RetryPolicy{MaxRetries: 0})

	// Case: unsupported model with temperature set -> should be stripped
	temp := 0.9
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := c.CreateChatCompletion(ctx, ChatCompletionsRequest{Model: "o3-mini", Messages: []Message{{Role: RoleUser, Content: "x"}}, Temperature: &temp})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if seenTemp != nil {
		t.Fatalf("expected temperature to be omitted; got %v", *seenTemp)
	}
}

// Ensure client preserves temperature for supported models.
func TestCreateChatCompletion_TemperaturePreservedWhenSupported(t *testing.T) {
	var seenTemp *float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		seenTemp = req.Temperature
		resp := ChatCompletionsResponse{Choices: []ChatCompletionsResponseChoice{{Message: Message{Role: RoleAssistant, Content: "ok"}}}}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClientWithRetry(srv.URL, "", 2*time.Second, RetryPolicy{MaxRetries: 0})
	temp := 0.7
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := c.CreateChatCompletion(ctx, ChatCompletionsRequest{Model: "oss-gpt-20b", Messages: []Message{{Role: RoleUser, Content: "x"}}, Temperature: &temp})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if seenTemp == nil || *seenTemp != 0.7 {
		if seenTemp == nil {
			t.Fatalf("expected temperature to be present")
		}
		t.Fatalf("expected temperature 0.7, got %v", *seenTemp)
	}
}

// Parameter-recovery retry: when the server responds 400 mentioning invalid/unsupported
// temperature, the client should remove temperature and retry once before any normal retries.
func TestCreateChatCompletion_ParameterRecovery_InvalidTemperature(t *testing.T) {
	attempts := 0
	var firstReqHadTemp bool
	var secondReqHadTemp bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		var req ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if attempts == 1 {
			firstReqHadTemp = req.Temperature != nil
			// Simulate OpenAI-style 400 error indicating unsupported temperature
			w.WriteHeader(http.StatusBadRequest)
			if _, err := w.Write([]byte(`{"error":{"message":"parameter 'temperature' is unsupported for this model"}}`)); err != nil {
				t.Fatalf("write: %v", err)
			}
			return
		}
		secondReqHadTemp = req.Temperature != nil
		// On retry, succeed
		resp := ChatCompletionsResponse{Choices: []ChatCompletionsResponseChoice{{Message: Message{Role: RoleAssistant, Content: "ok"}}}}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}))
	defer srv.Close()

	// No normal retries; parameter-recovery should still allow exactly one retry
	c := NewClientWithRetry(srv.URL, "", 2*time.Second, RetryPolicy{MaxRetries: 0})
	temp := 0.5
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := c.CreateChatCompletion(ctx, ChatCompletionsRequest{Model: "oss-gpt-20b", Messages: []Message{{Role: RoleUser, Content: "x"}}, Temperature: &temp})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out.Choices[0].Message.Content != "ok" {
		t.Fatalf("unexpected content: %+v", out)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts (1st 400, 2nd success), got %d", attempts)
	}
	if !firstReqHadTemp {
		t.Fatalf("expected temperature set on first request")
	}
	if secondReqHadTemp {
		t.Fatalf("expected temperature to be removed on retry after 400")
	}
}

// https://github.com/hyperifyio/goagent/issues/216
func TestCreateChatCompletion_RetryTimeoutThenSuccess(t *testing.T) {
	attempts := 0
	var firstIdem string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		// Assert Idempotency-Key header is present and stable across attempts
		idem := r.Header.Get("Idempotency-Key")
		if idem == "" {
			t.Fatalf("missing Idempotency-Key header")
		}
		if firstIdem == "" {
			firstIdem = idem
		} else if firstIdem != idem {
			t.Fatalf("Idempotency-Key changed across retries: %q != %q", firstIdem, idem)
		}
		if attempts == 1 {
			// Simulate a slow server to trigger client timeout
			time.Sleep(500 * time.Millisecond)
		}
		var req ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("bad json: %v", err)
		}
		resp := ChatCompletionsResponse{
			ID:      "cmpl-1",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []ChatCompletionsResponseChoice{{Index: 0, FinishReason: "stop", Message: Message{Role: RoleAssistant, Content: "ok"}}},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			panic(err)
		}
	}))
	defer ts.Close()

	// Small HTTP timeout to trigger quickly; allow 1 retry
	c := NewClientWithRetry(ts.URL, "", 200*time.Millisecond, RetryPolicy{MaxRetries: 1, Backoff: 1 * time.Millisecond})
	// Context slightly larger than two attempts to avoid overall ctx deadline
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	out, err := c.CreateChatCompletion(ctx, ChatCompletionsRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Choices[0].Message.Content != "ok" {
		t.Fatalf("unexpected content: %+v", out)
	}
	if attempts < 2 {
		t.Fatalf("expected at least 2 attempts, got %d", attempts)
	}

	// Verify audit log contains http_attempt and http_timing entries
	// Audit logs are centralized under the repository root's .goagent/audit
	root := findRepoRoot(t)
	auditDir := filepath.Join(root, ".goagent", "audit")
	// Allow a brief moment for file flush on slow FS
	time.Sleep(10 * time.Millisecond)
	entries, err := os.ReadDir(auditDir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("expected audit file in %s: %v", auditDir, err)
	}
	// Read the latest file and ensure it has at least two http_attempt lines
	latest := filepath.Join(auditDir, entries[len(entries)-1].Name())
	b, rerr := os.ReadFile(latest)
	if rerr != nil {
		t.Fatalf("read audit: %v", rerr)
	}
	content := string(b)
	if !strings.Contains(content, "\"event\":\"http_attempt\"") {
		t.Fatalf("expected http_attempt audit entries, got: %s", content)
	}
	if !strings.Contains(content, "\"event\":\"http_timing\"") {
		t.Fatalf("expected http_timing audit entries, got: %s", content)
	}
}

func TestIsRetryableError_ContextDeadline(t *testing.T) {
	if !isRetryableError(context.DeadlineExceeded) {
		t.Fatal("expected context deadline to be retryable")
	}
	if isRetryableError(errors.New("permanent failure")) {
		t.Fatal("unexpected retryable for generic error")
	}
}

// https://github.com/hyperifyio/goagent/issues/216
func TestCreateChatCompletion_RetryAfter_HeaderSeconds(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0") // zero should fallback to backoff, but we will return 429 to test path
			w.WriteHeader(http.StatusTooManyRequests)
			if _, err := w.Write([]byte(`{"error":"rate limited"}`)); err != nil {
				panic(err)
			}
			return
		}
		var req ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("bad json: %v", err)
		}
		resp := ChatCompletionsResponse{Choices: []ChatCompletionsResponseChoice{{Message: Message{Role: RoleAssistant, Content: "ok"}}}}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			panic(err)
		}
	}))
	defer ts.Close()

	c := NewClientWithRetry(ts.URL, "", 1*time.Second, RetryPolicy{MaxRetries: 2, Backoff: 1 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := c.CreateChatCompletion(ctx, ChatCompletionsRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Choices[0].Message.Content != "ok" {
		t.Fatalf("unexpected content: %+v", out)
	}
	if attempts < 2 {
		t.Fatalf("expected retry, got attempts=%d", attempts)
	}
}

// https://github.com/hyperifyio/goagent/issues/216
func TestRetryAfter_HTTPDate(t *testing.T) {
	// Validate header parsing helper directly
	now := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	date := now.Add(2 * time.Second).UTC().Format(http.TimeFormat)
	if d, ok := retryAfterDuration(date, now); !ok || d < 1900*time.Millisecond || d > 2100*time.Millisecond {
		t.Fatalf("unexpected duration: %v ok=%v", d, ok)
	}
}

// https://github.com/hyperifyio/goagent/issues/216
func TestBackoffWithJitter_GrowthAndBounds(t *testing.T) {
	base := 100 * time.Millisecond
	jf := 0.5 // +/-50%
	r := mathrand.New(mathrand.NewSource(1))
	d0 := backoffWithJitter(base, 0, jf, r)
	if d0 < 50*time.Millisecond || d0 > 150*time.Millisecond {
		t.Fatalf("attempt0 out of bounds: %v", d0)
	}
	d1 := backoffWithJitter(base, 1, jf, r)
	// attempt 1 base is 200ms; with jitter bounds are [100ms, 300ms]
	if d1 < 100*time.Millisecond || d1 > 300*time.Millisecond {
		t.Fatalf("attempt1 out of bounds: %v", d1)
	}
	// ensure min growth relative to min bound
	if d1 <= 75*time.Millisecond { // strictly greater than a conservative lower threshold
		t.Fatalf("expected growth, d1=%v", d1)
	}
	// cap check at high attempts should not exceed 2s +/- jitter
	dN := backoffWithJitter(base, 10, jf, r)
	if dN < 1*time.Second || dN > 3*time.Second {
		t.Fatalf("cap bounds unexpected: %v", dN)
	}
}

// Verify jittered backoff is used for 429 without Retry-After.
func TestCreateChatCompletion_Retry429_UsesJitteredBackoff(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		var req ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("bad json: %v", err)
		}
		_ = json.NewEncoder(w).Encode(ChatCompletionsResponse{Choices: []ChatCompletionsResponseChoice{{Message: Message{Role: RoleAssistant, Content: "ok"}}}})
	}))
	defer ts.Close()

	// Intercept sleeps
	var slept []time.Duration
	oldSleep := sleepFunc
	sleepFunc = func(d time.Duration) { slept = append(slept, d) }
	defer func() { sleepFunc = oldSleep }()

	// Deterministic jitter
	r := mathrand.New(mathrand.NewSource(42))
	c := NewClientWithRetry(ts.URL, "", 1*time.Second, RetryPolicy{MaxRetries: 1, Backoff: 100 * time.Millisecond, JitterFraction: 0.5, Rand: r})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := c.CreateChatCompletion(ctx, ChatCompletionsRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Choices[0].Message.Content != "ok" {
		t.Fatalf("unexpected content: %+v", out)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if len(slept) != 1 {
		t.Fatalf("expected one sleep, got %d", len(slept))
	}
	if slept[0] < 50*time.Millisecond || slept[0] > 150*time.Millisecond {
		t.Fatalf("sleep not jittered within bounds: %v", slept[0])
	}
}

// Verify jittered backoff is used for client timeouts on first attempt.
func TestCreateChatCompletion_RetryTimeout_UsesJitteredBackoff(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			time.Sleep(120 * time.Millisecond)
		}
		var req ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("bad json: %v", err)
		}
		_ = json.NewEncoder(w).Encode(ChatCompletionsResponse{Choices: []ChatCompletionsResponseChoice{{Message: Message{Role: RoleAssistant, Content: "ok"}}}})
	}))
	defer ts.Close()

	var slept []time.Duration
	oldSleep := sleepFunc
	sleepFunc = func(d time.Duration) { slept = append(slept, d) }
	defer func() { sleepFunc = oldSleep }()

	r := mathrand.New(mathrand.NewSource(7))
	c := NewClientWithRetry(ts.URL, "", 100*time.Millisecond, RetryPolicy{MaxRetries: 1, Backoff: 100 * time.Millisecond, JitterFraction: 0.25, Rand: r})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := c.CreateChatCompletion(ctx, ChatCompletionsRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Choices[0].Message.Content != "ok" {
		t.Fatalf("unexpected content: %+v", out)
	}
	if attempts < 2 {
		t.Fatalf("expected retry, attempts=%d", attempts)
	}
	if len(slept) != 1 {
		t.Fatalf("expected one sleep, got %d", len(slept))
	}
	// base=100ms, jitter 25% => [75ms,125ms]
	if slept[0] < 75*time.Millisecond || slept[0] > 125*time.Millisecond {
		t.Fatalf("sleep not within jitter bounds: %v", slept[0])
	}
}
