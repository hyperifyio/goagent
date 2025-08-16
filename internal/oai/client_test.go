package oai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
