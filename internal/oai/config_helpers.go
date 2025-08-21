package oai

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// PromptProfile denotes a pre-stage prompt style selector.
// Expected values include "deterministic", "general", "creative", "reasoning".
// The concrete semantics are applied by higher layers; this type is used
// primarily for configuration plumbing and printing.
type PromptProfile string

// ImageConfig bundles resolved Image API connection settings.
type ImageConfig struct {
	BaseURL string
	APIKey  string
}

// ResolveImageConfig resolves Image API BaseURL and API Key using the following precedence:
// - If explicit imageBaseURL/imageAPIKey are provided (non-empty), use them (source: "flag").
// - Else if environment variables are set, prefer OAI_IMAGE_BASE_URL and OAI_IMAGE_API_KEY (source: "env").
//   For the API key, also allow OPENAI_API_KEY as a fallback environment variable.
// - Else inherit from baseURL/apiKey (source: "inherit"). When the inherited key is empty, the source is "empty".
func ResolveImageConfig(imageBaseURL, imageAPIKey, baseURL, apiKey string) (ImageConfig, string, string) {
	var cfg ImageConfig
	var baseSrc, keySrc string

	// Base URL
	if strings.TrimSpace(imageBaseURL) != "" {
		cfg.BaseURL = strings.TrimSpace(imageBaseURL)
		baseSrc = "flag"
	} else if v := strings.TrimSpace(os.Getenv("OAI_IMAGE_BASE_URL")); v != "" {
		cfg.BaseURL = v
		baseSrc = "env"
	} else {
		cfg.BaseURL = strings.TrimSpace(baseURL)
		if cfg.BaseURL != "" {
			baseSrc = "inherit"
		} else {
			baseSrc = "empty"
		}
	}

	// API key
	if strings.TrimSpace(imageAPIKey) != "" {
		cfg.APIKey = strings.TrimSpace(imageAPIKey)
		keySrc = "flag"
	} else if v := strings.TrimSpace(os.Getenv("OAI_IMAGE_API_KEY")); v != "" {
		cfg.APIKey = v
		keySrc = "env"
	} else if v := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); v != "" {
		// Compatibility: allow OPENAI_API_KEY for images too
		cfg.APIKey = v
		keySrc = "env:OPENAI_API_KEY"
	} else {
		cfg.APIKey = strings.TrimSpace(apiKey)
		if cfg.APIKey != "" {
			keySrc = "inherit"
		} else {
			keySrc = "empty"
		}
	}

	return cfg, baseSrc, keySrc
}

// MaskAPIKeyLast4 returns a redacted representation of a secret showing only the last 4 characters.
// Empty input returns an empty string. Inputs with length <= 4 return "****".
func MaskAPIKeyLast4(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(s)-4) + s[len(s)-4:]
}

// ResolveInt resolves an integer from flag/env/inherit/default sources and returns the value and source label.
// - If flagSet is true, flagVal is returned with source "flag".
// - Else if envStr parses as a non-negative integer, it is returned with source "env".
// - Else if inherit is non-nil, *inherit is returned with source "inherit".
// - Else defaultVal is returned with source "default".
func ResolveInt(flagSet bool, flagVal int, envStr string, inherit *int, defaultVal int) (int, string) {
	if flagSet {
		return flagVal, "flag"
	}
	if v := strings.TrimSpace(envStr); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n, "env"
		}
	}
	if inherit != nil {
		return *inherit, "inherit"
	}
	return defaultVal, "default"
}

// ResolveDuration resolves a time.Duration from flag/env/inherit/default sources and returns the value and source label.
// - If flagSet is true and flagVal > 0, flagVal is returned with source "flag".
// - Else if envStr parses via time.ParseDuration() or as an integer seconds value, return that with source "env".
// - Else if inherit is non-nil, *inherit is returned with source "inherit".
// - Else defaultVal is returned with source "default".
func ResolveDuration(flagSet bool, flagVal time.Duration, envStr string, inherit *time.Duration, defaultVal time.Duration) (time.Duration, string) {
	if flagSet && flagVal > 0 {
		return flagVal, "flag"
	}
	if v := strings.TrimSpace(envStr); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d, "env"
		}
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			if n > 0 {
				return time.Duration(n) * time.Second, "env"
			}
		}
	}
	if inherit != nil {
		return *inherit, "inherit"
	}
	return defaultVal, "default"
}

// MapProfileToTemperature maps a pre-stage prompt profile to an effective
// temperature for the given model. When the target model does not support
// temperature, the second return value is false and the caller should omit
// the field entirely.
//
// Profile mapping (see docs/reference/cli-reference.md):
// - deterministic => 0.1
// - general | creative | reasoning => 1.0
// - unknown/empty => (0, false)
func MapProfileToTemperature(model string, profile PromptProfile) (float64, bool) {
    p := strings.ToLower(strings.TrimSpace(string(profile)))
    if p == "" {
        return 0, false
    }
    var temp float64
    switch p {
    case "deterministic":
        temp = 0.1
    case "general", "creative", "reasoning":
        temp = 1.0
    default:
        return 0, false
    }
    if !SupportsTemperature(model) {
        return 0, false
    }
    return clampTemperature(temp), true
}
