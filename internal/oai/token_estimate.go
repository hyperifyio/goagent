package oai

import (
	"math"
)

// EstimateTokens returns a rough, deterministic token estimate for a set of
// chat messages. It intentionally uses a simple heuristic that avoids any
// external dependencies and is stable across platforms.
//
// Heuristic:
//   - Assume ~4 characters per token on average
//   - Add a small fixed overhead per message to account for roles/formatting
//   - Include optional fields (name, tool_call_id) and a coarse cost for tool calls
func EstimateTokens(messages []Message) int {
	const averageCharsPerToken = 4.0
	const perMessageOverheadTokens = 4
	const perToolCallOverheadTokens = 8

	total := 0
	for _, msg := range messages {
		// Content cost
		if msg.Content != "" {
			total += int(math.Ceil(float64(len(msg.Content)) / averageCharsPerToken))
		}
		// Optional name and tool call id fields
		if msg.Name != "" {
			total += int(math.Ceil(float64(len(msg.Name)) / averageCharsPerToken))
		}
		if msg.ToolCallID != "" {
			total += int(math.Ceil(float64(len(msg.ToolCallID)) / averageCharsPerToken))
		}
		// Tool calls (coarse)
		if len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				// Per-call overhead plus name/arguments length approximated to tokens
				total += perToolCallOverheadTokens
				if tc.Function.Name != "" {
					total += int(math.Ceil(float64(len(tc.Function.Name)) / averageCharsPerToken))
				}
				if tc.Function.Arguments != "" {
					total += int(math.Ceil(float64(len(tc.Function.Arguments)) / averageCharsPerToken))
				}
			}
		}
		// Per-message structural overhead
		total += perMessageOverheadTokens
	}

	// Ensure non-negative and at least one token per message in extreme edge cases
	if total < len(messages) {
		total = len(messages)
	}
	return total
}
