package oai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestChatCompletionsRequest_MaxTokens_OmitsWhenZero(t *testing.T) {
	req := ChatCompletionsRequest{
		Model:    "m",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		// MaxTokens is zero by default
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if strings.Contains(s, "\"max_tokens\":") {
		t.Fatalf("expected max_tokens to be omitted when zero, got: %s", s)
	}
}

func TestChatCompletionsRequest_MaxTokens_IncludedWhenSet(t *testing.T) {
	req := ChatCompletionsRequest{
		Model:     "m",
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens: 123,
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "\"max_tokens\":") {
		t.Fatalf("expected max_tokens key present, got: %s", s)
	}
	if !strings.Contains(s, "\"max_tokens\":123") {
		t.Fatalf("expected max_tokens=123, got: %s", s)
	}
}
