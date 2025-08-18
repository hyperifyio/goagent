package oai

import (
    "encoding/json"
    "fmt"
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
    // Stream requests server-sent events (SSE) streaming mode when true.
    // When enabled, the server responds with text/event-stream and emits
    // incremental deltas under choices[].delta.
    Stream bool `json:"stream,omitempty"`
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

// NormalizeHarmonyMessages returns a copy of messages with roles trimmed and
// lowercased, and assistant channel tokens normalized to a safe subset.
// Valid roles are: system, developer, user, assistant, tool. Any other role
// results in an error. Channels are optional; when present on assistant
// messages they are lowercased, non-ASCII characters are removed, and the
// result is truncated to 32 characters. Unknown channel names are allowed and
// simply pass through after normalization; they may not be auto-printed unless
// explicitly routed by the CLI.
func NormalizeHarmonyMessages(in []Message) ([]Message, error) {
    out := make([]Message, 0, len(in))
    for _, m := range in {
        nm := m
        nm.Role = strings.ToLower(strings.TrimSpace(nm.Role))
        switch nm.Role {
        case RoleSystem, RoleDeveloper, RoleUser, RoleAssistant, RoleTool:
            // ok
        default:
            return nil, fmt.Errorf("invalid role: %q", m.Role)
        }
        // Normalize channel only for assistant messages
        if nm.Role == RoleAssistant {
            ch := strings.ToLower(strings.TrimSpace(nm.Channel))
            if ch != "" {
                ch = normalizeAssistantChannel(ch)
            }
            nm.Channel = ch
        } else {
            // Other roles should not carry a channel
            nm.Channel = ""
        }
        out = append(out, nm)
    }
    return out, nil
}

// normalizeAssistantChannel makes channel tokens safe: lowercased, ASCII-only
// subset [a-z0-9_-], and max length 32. Characters outside the allowed set are
// dropped. If the result is empty after filtering, the empty string is
// returned, which the CLI treats as an unchannelled assistant message.
func normalizeAssistantChannel(in string) string {
    const maxLen = 32
    // Filter to allowed characters
    b := make([]byte, 0, len(in))
    for i := 0; i < len(in); i++ {
        c := in[i]
        if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
            b = append(b, c)
        }
        if len(b) >= maxLen {
            break
        }
    }
    return string(b)
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

// StreamChunk models an SSE delta event payload for streaming responses.
// Only a subset of fields are needed for CLI streaming.
type StreamChunk struct {
    ID      string `json:"id"`
    Object  string `json:"object"`
    Model   string `json:"model"`
    Choices []struct {
        Index int `json:"index"`
        Delta struct {
            Role    string `json:"role"`
            Channel string `json:"channel"`
            Content string `json:"content"`
        } `json:"delta"`
        FinishReason string `json:"finish_reason"`
    } `json:"choices"`
}
