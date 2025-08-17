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
