package oai

import (
	"context"
	"encoding/json"
	"errors"
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
	auditDir := filepath.Join(".goagent", "audit")
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
			_, _ = w.Write([]byte(`{"error":"rate limited"}`))
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
