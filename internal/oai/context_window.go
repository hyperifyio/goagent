package oai

import "strings"

// DefaultContextWindow provides a conservative default for modern models.
const DefaultContextWindow = 128000

// modelToContextWindow holds known model context window sizes.
// Keys should be lower-case exact model identifiers.
var modelToContextWindow = map[string]int{
	"oss-gpt-20b": 8192,
}

// ContextWindowForModel returns the total token window for a given model.
// When the model is unknown or empty, it returns DefaultContextWindow.
func ContextWindowForModel(model string) int {
	m := strings.TrimSpace(strings.ToLower(model))
	if m == "" {
		return DefaultContextWindow
	}
	if w, ok := modelToContextWindow[m]; ok {
		return w
	}
	return DefaultContextWindow
}

// ClampCompletionCap bounds a desired completion cap to the remaining context
// window after accounting for the estimated tokens of the prompt messages. It
// ensures a minimum of 1 token and subtracts a small safety margin.
//
// The clamp rule is: max(1, window - EstimateTokens(messages) - 32), then
// bounded above by the requested cap.
func ClampCompletionCap(messages []Message, requestedCap int, window int) int {
	// Remaining space after considering prompt tokens and a small margin.
	remaining := window - EstimateTokens(messages) - 32
	if remaining < 1 {
		remaining = 1
	}
	if requestedCap <= 0 {
		// If caller provides non-positive cap, treat as wanting the maximum safe amount.
		return remaining
	}
	if requestedCap > remaining {
		return remaining
	}
	return requestedCap
}
