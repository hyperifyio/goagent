package oai

import (
	"encoding/json"
	"strings"
)

// Message roles
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
    // RoleDeveloper is a Harmony role used to convey developer guidance
    // that is distinct from system and user prompts. Messages with this
    // role are prepended ahead of user messages and may be merged from
    // multiple sources (CLI flags and pre-stage refinement).
    RoleDeveloper = "developer"
)

// Message represents an OpenAI-compatible chat message.
// Tool results are conveyed via RoleTool with ToolCallID and Content.
type Message struct {
	Role       string `json:"role"`
	Content    string `json:"content,omitempty"`
	Name       string `json:"name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
    // Channel allows assistants to tag messages with a semantic channel such as
    // "final", "critic", or "confidence". Unknown or empty channels are
    // treated as normal assistant messages by the CLI unless routed explicitly.
    Channel    string `json:"channel,omitempty"`
	// The OpenAI-compatible schema also allows "tool_calls" on assistant messages.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall mirrors the OpenAI tool call structure.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool describes a function tool as per OpenAI API.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ChatCompletionsRequest is the payload for POST /v1/chat/completions
// Compatible with OpenAI API.
type ChatCompletionsRequest struct {
	Model      string    `json:"model"`
	Messages   []Message `json:"messages"`
	Tools      []Tool    `json:"tools,omitempty"`
	ToolChoice string    `json:"tool_choice,omitempty"`
	// TopP enables nucleus sampling when provided. One‑knob rule ensures either
	// top_p or temperature is set, but never both.
	TopP        *float64 `json:"top_p,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
    // MaxTokens limits the number of tokens generated for the completion.
    // Omitted when zero to preserve backward compatibility.
    MaxTokens int `json:"max_tokens,omitempty"`
}

// includesTemperature reports whether the request currently has a temperature set.
func includesTemperature(req ChatCompletionsRequest) bool { return req.Temperature != nil }

// mentionsUnsupportedTemperature detects common API error messages indicating
// that the temperature parameter is invalid or unsupported for the model.
func mentionsUnsupportedTemperature(body string) bool {
	s := strings.ToLower(body)
	if s == "" {
		return false
	}
	return (strings.Contains(s, "unsupported") && strings.Contains(s, "temperature")) ||
		(strings.Contains(s, "invalid") && strings.Contains(s, "temperature"))
}

// ChatCompletionsResponse represents the response for chat completions.
type ChatCompletionsResponse struct {
	ID      string                          `json:"id"`
	Object  string                          `json:"object"`
	Created int64                           `json:"created"`
	Model   string                          `json:"model"`
	Choices []ChatCompletionsResponseChoice `json:"choices"`
}

type ChatCompletionsResponseChoice struct {
	Index        int     `json:"index"`
	FinishReason string  `json:"finish_reason"`
	Message      Message `json:"message"`
}
