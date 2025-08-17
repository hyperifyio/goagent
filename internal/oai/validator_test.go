package oai

import "testing"

func TestValidateMessageSequence_InvalidStrayTool(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleTool, Name: "echo", ToolCallID: "call_1", Content: "{\"echo\":\"hi\"}"},
	}
	if err := ValidateMessageSequence(msgs); err == nil {
		t.Fatalf("expected error for stray tool message without prior assistant tool_calls")
	}
}

func TestValidateMessageSequence_ValidSequenceSingleTool(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_1", Type: "function", Function: ToolCallFunction{Name: "echo", Arguments: "{\"text\":\"hi\"}"}}}},
		{Role: RoleTool, Name: "echo", ToolCallID: "call_1", Content: "{\"echo\":\"hi\"}"},
	}
	if err := ValidateMessageSequence(msgs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateMessageSequence_InvalidMismatchedID(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_1", Type: "function", Function: ToolCallFunction{Name: "echo", Arguments: "{\"text\":\"hi\"}"}}}},
		{Role: RoleTool, Name: "echo", ToolCallID: "call_2", Content: "{\"echo\":\"hi\"}"},
	}
	if err := ValidateMessageSequence(msgs); err == nil {
		t.Fatalf("expected error for mismatched tool_call_id not present in prior assistant tool_calls")
	}
}
