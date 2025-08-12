package oai

import "encoding/json"

// Message roles
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message represents an OpenAI-compatible chat message.
// Tool results are conveyed via RoleTool with ToolCallID and Content.
type Message struct {
	Role       string `json:"role"`
	Content    string `json:"content,omitempty"`
	Name       string `json:"name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	// The OpenAI-compatible schema also allows "tool_calls" on assistant messages.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall mirrors the OpenAI tool call structure.
type ToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
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
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []Tool    `json:"tools,omitempty"`
	ToolChoice  string    `json:"tool_choice,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
}

// ChatCompletionsResponse represents the response for chat completions.
type ChatCompletionsResponse struct {
	ID      string                        `json:"id"`
	Object  string                        `json:"object"`
	Created int64                         `json:"created"`
	Model   string                        `json:"model"`
	Choices []ChatCompletionsResponseChoice `json:"choices"`
}

type ChatCompletionsResponseChoice struct {
	Index        int     `json:"index"`
	FinishReason string  `json:"finish_reason"`
	Message      Message `json:"message"`
}
