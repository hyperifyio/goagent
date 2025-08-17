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
