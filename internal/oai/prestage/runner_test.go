package prestage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hyperifyio/goagent/internal/oai"
)

func TestRunner_SendsResolvedPromptAsUserMessage(t *testing.T) {
	// Arrange a fake OpenAI-compatible server that asserts first user message equals prompt
	const wantPrompt = "PREP_PROMPT_TEXT"
	var gotReq oai.ChatCompletionsRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&gotReq); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Minimal valid response
		if err := json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Model: gotReq.Model}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}))
	defer server.Close()

	client := oai.NewClient(server.URL, "", 2*time.Second)
	runner := &Runner{Client: client, Model: "gpt-test-1"}

	// Act
	_, err := runner.Run(context.Background(), wantPrompt)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Assert
	if len(gotReq.Messages) == 0 || gotReq.Messages[0].Role != oai.RoleUser || gotReq.Messages[0].Content != wantPrompt {
		t.Fatalf("unexpected first message: %+v", gotReq.Messages)
	}
}

func TestRunner_OneKnobRuleAndJSONMode(t *testing.T) {
	// Arrange a fake server that captures request
	var gotReq oai.ChatCompletionsRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&gotReq); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Model: gotReq.Model}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}))
	defer server.Close()

	client := oai.NewClient(server.URL, "", 2*time.Second)
	temp := 0.5
	topP := 0.9
	runner := &Runner{Client: client, Model: "gpt-test-1", Temperature: &temp, TopP: &topP, JSONMode: true}

	_, err := runner.Run(context.Background(), "x")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// When TopP is set, temperature must be omitted per one‑knob rule
	if gotReq.TopP == nil || *gotReq.TopP != 0.9 {
		t.Fatalf("expected top_p 0.9, got %+v", gotReq.TopP)
	}
	if gotReq.Temperature != nil {
		t.Fatalf("expected temperature omitted when top_p set; got %v", *gotReq.Temperature)
	}
	// JSON mode should be requested
	if gotReq.ResponseFormat == nil || gotReq.ResponseFormat.Type != "json_object" {
		t.Fatalf("expected response_format json_object, got %+v", gotReq.ResponseFormat)
	}
}
