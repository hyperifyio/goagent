package oai

import (
	"os"
	"strings"
	"unicode"
)

// ImageConfig holds resolved configuration for the Images API endpoint.
type ImageConfig struct {
	BaseURL string
	APIKey  string
}

// ResolveImageConfig determines the effective image configuration using the precedence:
// flag > env > inheritFrom > fallback. The env variables are OAI_IMAGE_BASE_URL and
// OAI_IMAGE_API_KEY. When API key is not provided via flag or env, it inherits from
// the provided mainAPIKey; if still empty, it falls back to OPENAI_API_KEY if present.
// The returned sources describe where each field came from: "flag" | "env" |
// "inherit" | "env:OPENAI_API_KEY" | "empty".
func ResolveImageConfig(flagBaseURL, flagAPIKey, mainBaseURL, mainAPIKey string) (cfg ImageConfig, baseSource, keySource string) {
	// Base URL resolution
	if s := strings.TrimSpace(flagBaseURL); s != "" {
		cfg.BaseURL = s
		baseSource = "flag"
	} else if s := strings.TrimSpace(os.Getenv("OAI_IMAGE_BASE_URL")); s != "" {
		cfg.BaseURL = s
		baseSource = "env"
	} else {
		cfg.BaseURL = strings.TrimSpace(mainBaseURL)
		baseSource = "inherit"
	}

	// API key resolution
	if s := strings.TrimSpace(flagAPIKey); s != "" {
		cfg.APIKey = s
		keySource = "flag"
	} else if s := strings.TrimSpace(os.Getenv("OAI_IMAGE_API_KEY")); s != "" {
		cfg.APIKey = s
		keySource = "env"
	} else if s := strings.TrimSpace(mainAPIKey); s != "" {
		cfg.APIKey = s
		keySource = "inherit"
	} else if s := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); s != "" {
		cfg.APIKey = s
		keySource = "env:OPENAI_API_KEY"
	} else {
		cfg.APIKey = ""
		keySource = "empty"
	}
	return
}

// MaskAPIKeyLast4 returns a redacted representation of an API key showing only
// the last 4 characters. Empty input returns an empty string.
func MaskAPIKeyLast4(key string) string {
	k := strings.TrimSpace(key)
	if k == "" {
		return ""
	}
	if len(k) <= 4 {
		return "****" + k
	}
	return "****" + k[len(k)-4:]
}

// PrepConfig holds resolved configuration for the pre-stage flow.
// Currently it includes only the prepared prompt text.
type PrepConfig struct {
	// Prompt is the finalized pre-stage prompt after applying overrides.
	// When multiple prompt sources are provided, they are concatenated using
	// JoinPrompts and stored here.
	Prompt string
}

// JoinPrompts concatenates the given parts in-order using two newline
// separators ("\n\n") and trims trailing whitespace from the final string.
// It preserves leading whitespace and internal whitespace within parts.
func JoinPrompts(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	// Trim trailing newlines, carriage returns, and tabs from each part, but
	// preserve trailing spaces to avoid eating intentional spacing before the
	// separator.
	trimmed := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed = append(trimmed, trimRightNLTab(p))
	}
	joined := strings.Join(trimmed, "\n\n")
	// Finally, trim any trailing whitespace from the full string
	return strings.TrimRightFunc(joined, unicode.IsSpace)
}

// trimRightNLTab removes trailing newlines, carriage returns, and tabs.
func trimRightNLTab(s string) string {
	return strings.TrimRightFunc(s, func(r rune) bool {
		return r == '\n' || r == '\r' || r == '\t'
	})
}

// NewPrepConfig constructs a PrepConfig with Prompt set to the normalized
// concatenation of the provided parts.
func NewPrepConfig(parts []string) PrepConfig {
	return PrepConfig{Prompt: JoinPrompts(parts)}
}
