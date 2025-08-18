package oai

import (
	"os"
	"strings"
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
