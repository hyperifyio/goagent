package tools

import (
	"os"
	"regexp"
	"strings"
)

// redactSensitiveStrings applies redactSensitiveString to each element and returns a new slice.
func redactSensitiveStrings(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = redactSensitiveString(v)
	}
	return out
}

// redactSensitiveString masks occurrences of configured sensitive patterns and known secret env values.
// Patterns are sourced from GOAGENT_REDACT (comma/semicolon-separated substrings or regexes).
// Additionally, values of well-known secret env vars (OAI_API_KEY, OPENAI_API_KEY) are masked if present.
func redactSensitiveString(s string) string {
	if s == "" {
		return s
	}
	// Collect patterns
	patterns := gatherRedactionPatterns()
	// Apply regex replacements first
	for _, rx := range patterns.regexps {
		s = rx.ReplaceAllString(s, "***REDACTED***")
	}
	// Apply literal value masking
	for _, lit := range patterns.literals {
		if lit == "" {
			continue
		}
		s = strings.ReplaceAll(s, lit, "***REDACTED***")
	}
	return s
}

type redactionPatterns struct {
	regexps  []*regexp.Regexp
	literals []string
}

// gatherRedactionPatterns builds redaction patterns from environment.
// GOAGENT_REDACT may contain comma/semicolon separated regex patterns or literals.
// Known secret env values are added as literal masks.
func gatherRedactionPatterns() redactionPatterns {
	var pats redactionPatterns
	// Configurable patterns
	cfg := os.Getenv("GOAGENT_REDACT")
	if cfg != "" {
		// split by comma or semicolon
		fields := strings.FieldsFunc(cfg, func(r rune) bool { return r == ',' || r == ';' })
		for _, f := range fields {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			// Try to compile as regex; if it fails, treat as literal
			if rx, err := regexp.Compile(f); err == nil {
				pats.regexps = append(pats.regexps, rx)
			} else {
				pats.literals = append(pats.literals, f)
			}
		}
	}
	// Known secret env values (mask exact substrings)
	for _, key := range []string{"OAI_API_KEY", "OPENAI_API_KEY"} {
		if v := os.Getenv(key); v != "" {
			pats.literals = append(pats.literals, v)
		}
	}
	return pats
}
