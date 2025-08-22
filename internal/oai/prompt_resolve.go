package oai

import (
	"strings"
	"unicode"
)

// JoinPrompts concatenates parts with two newlines and trims trailing whitespace.
func JoinPrompts(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	trimmed := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed = append(trimmed, trimRightNLTab(p))
	}
	joined := strings.Join(trimmed, "\n\n")
	return strings.TrimRightFunc(joined, unicode.IsSpace)
}

func trimRightNLTab(s string) string {
	return strings.TrimRightFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r' || r == '\t'
	})
}

// ResolvePrepPrompt selects the effective pre-stage prompt text and its source.
// Order: explicit prompt strings (override) > joined file contents (override) > embedded default.
func ResolvePrepPrompt(prepPrompts []string, prepFilesJoined string) (source string, text string) {
	if len(prepPrompts) > 0 {
		return "override", JoinPrompts(prepPrompts)
	}
	if s := strings.TrimSpace(prepFilesJoined); s != "" {
		// trim trailing whitespace but preserve internal spacing
		return "override", strings.TrimRightFunc(s, func(r rune) bool { return r == '\n' || r == '\r' || r == '\t' || r == ' ' })
	}
	return "default", DefaultPrepPrompt()
}
