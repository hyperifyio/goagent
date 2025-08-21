package oai

import "fmt"

// ValidateMessageSequence enforces that any tool message responds to the most
// recent assistant message that contains tool_calls and that the tool_call_id
// matches one of those ids. It returns a descriptive error when the sequence is
// invalid. This mirrors the API's requirement that tool outputs must respond to
// a prior assistant tool call.
func ValidateMessageSequence(messages []Message) error {
    currentAllowedIDs := map[string]struct{}{}
    hasAllowed := false
    for i, m := range messages {
        switch m.Role {
        case RoleAssistant:
            if len(m.ToolCalls) > 0 {
                currentAllowedIDs = make(map[string]struct{}, len(m.ToolCalls))
                for _, tc := range m.ToolCalls {
                    if tc.ID != "" {
                        currentAllowedIDs[tc.ID] = struct{}{}
                    }
                }
                hasAllowed = true
            }
        case RoleTool:
            if !hasAllowed {
                return fmt.Errorf("invalid message sequence at index %d: found role:\"tool\" without a prior assistant message containing tool_calls; each tool message must respond to an assistant tool call id", i)
            }
            if m.ToolCallID == "" {
                return fmt.Errorf("invalid message sequence at index %d: role:\"tool\" is missing tool_call_id; each tool message must include the id of the assistant tool call it responds to", i)
            }
            if _, ok := currentAllowedIDs[m.ToolCallID]; !ok {
                return fmt.Errorf("invalid message sequence at index %d: role:\"tool\" has tool_call_id %q that does not match any id from the most recent assistant tool_calls", i, m.ToolCallID)
            }
        }
    }
    return nil
}

// ValidatePrestageHarmony enforces the pre-stage output contract for Harmony
// messages. The contract requires that the array contains only roles "system"
// and/or "developer". Messages MUST NOT include role "tool", role
// "assistant", or role "user". Additionally, no message may contain
// tool_calls and no tool message with tool_call_id is allowed at this stage.
// The content field may be empty for system messages but developer messages
// should typically include guidance text; emptiness is permitted to keep the
// validator non-opinionated about content semantics.
func ValidatePrestageHarmony(messages []Message) error {
    for i, m := range messages {
        switch m.Role {
        case RoleSystem, RoleDeveloper:
            // Allowed roles for pre-stage output
        case RoleTool:
            return fmt.Errorf("pre-stage output invalid at index %d: role:\"tool\" is not allowed in pre-stage output", i)
        case RoleAssistant:
            return fmt.Errorf("pre-stage output invalid at index %d: role:\"assistant\" is not allowed in pre-stage output", i)
        case RoleUser:
            return fmt.Errorf("pre-stage output invalid at index %d: role:\"user\" is not allowed in pre-stage output", i)
        default:
            return fmt.Errorf("pre-stage output invalid at index %d: unknown role %q", i, m.Role)
        }
        if len(m.ToolCalls) > 0 {
            return fmt.Errorf("pre-stage output invalid at index %d: tool_calls are not allowed in pre-stage output", i)
        }
        if m.ToolCallID != "" {
            return fmt.Errorf("pre-stage output invalid at index %d: tool_call_id present but no tool calls are allowed in pre-stage output", i)
        }
    }
    return nil
}
