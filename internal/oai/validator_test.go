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

func TestValidatePrestageHarmony_AllowsSystemAndDeveloperOnly(t *testing.T) {
	msgs := []Message{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleDeveloper, Content: "dev guidance"},
	}
	if err := ValidatePrestageHarmony(msgs); err != nil {
		t.Fatalf("expected ok, got error: %v", err)
	}
}

func TestValidatePrestageHarmony_RejectsAssistantUserToolRoles(t *testing.T) {
	cases := []struct {
		name string
		msgs []Message
	}{
		{"assistant", []Message{{Role: RoleAssistant, Content: "nope"}}},
		{"user", []Message{{Role: RoleUser, Content: "nope"}}},
		{"tool", []Message{{Role: RoleTool, ToolCallID: "x"}}},
	}
	for _, tc := range cases {
		if err := ValidatePrestageHarmony(tc.msgs); err == nil {
			t.Fatalf("%s: expected error, got nil", tc.name)
		}
	}
}

func TestValidatePrestageHarmony_RejectsToolCallsAndToolCallID(t *testing.T) {
	msgsWithToolCalls := []Message{{Role: RoleSystem, ToolCalls: []ToolCall{{ID: "1", Type: "function", Function: ToolCallFunction{Name: "x"}}}}}
	if err := ValidatePrestageHarmony(msgsWithToolCalls); err == nil {
		t.Fatalf("expected error for tool_calls, got nil")
	}
	msgsWithToolCallID := []Message{{Role: RoleDeveloper, ToolCallID: "abc"}}
	if err := ValidatePrestageHarmony(msgsWithToolCallID); err == nil {
		t.Fatalf("expected error for tool_call_id, got nil")
	}
}
