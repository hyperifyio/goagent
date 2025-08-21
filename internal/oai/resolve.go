package oai

import (
    "strconv"
    "strings"
    "time"
)

// ResolveString resolves a string value with precedence:
// flag > env > inheritFrom > default.
// When inheritFrom is nil, the inherit step is skipped.
// Returns the resolved value and a source label: "flag"|"env"|"inherit"|"default".
func ResolveString(flagValue string, envValue string, inheritFrom *string, def string) (string, string) {
	fv := strings.TrimSpace(flagValue)
	if fv != "" {
		return fv, "flag"
	}
	ev := strings.TrimSpace(envValue)
	if ev != "" {
		return ev, "env"
	}
	if inheritFrom != nil {
		return strings.TrimSpace(*inheritFrom), "inherit"
	}
	return def, "default"
}

// ResolveInt resides in config_helpers.go

// ResolveBool resolves a bool with precedence:
// flag (when flagSet) > env (parseable) > inheritFrom > default.
// Returns the resolved value and a source label.
func ResolveBool(flagSet bool, flagValue bool, envValue string, inheritFrom *bool, def bool) (bool, string) {
	if flagSet {
		return flagValue, "flag"
	}
	ev := strings.TrimSpace(envValue)
	if ev != "" {
		if b, err := strconv.ParseBool(ev); err == nil {
			return b, "env"
		}
		// fall through on parse error
	}
	if inheritFrom != nil {
		return *inheritFrom, "inherit"
	}
	return def, "default"
}

// ResolveDuration resides in config_helpers.go

// parseDurationFlexible mirrors the CLI parser behavior: accepts standard Go
// duration strings (e.g., "750ms", "3s") and plain integer seconds (e.g., "30").
func parseDurationFlexible(raw string) (time.Duration, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, strconv.ErrSyntax
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	// Accept plain integer seconds
	allDigits := true
	for _, r := range s {
		if r < '0' || r > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, err
		}
		if n < 0 {
			// Allow zero; negative is invalid
			return 0, strconv.ErrRange
		}
		return time.Duration(n) * time.Second, nil
	}
	return 0, strconv.ErrSyntax
}

// ResolvePrepPrompt determines the effective pre-stage prompt text and its source.
// It applies the following deterministic order:
//  1. If one or more explicit prompt strings are provided via flags (prepPrompts),
//     join them using JoinPrompts and return ("override", text).
//  2. Else if one or more prompts were loaded from files (prepFilesJoined), use that
//     joined text and return ("override", text).
//  3. Otherwise, return the embedded default via DefaultPrepPrompt() with source
//     label "default".
//
// Callers are expected to pre-join file contents in the order observed when flags
// were parsed to produce prepFilesJoined.
func ResolvePrepPrompt(prepPrompts []string, prepFilesJoined string) (source string, text string) {
	if len(prepPrompts) > 0 {
		return "override", JoinPrompts(prepPrompts)
	}
	if strings.TrimSpace(prepFilesJoined) != "" {
		return "override", strings.TrimRightFunc(prepFilesJoined, func(r rune) bool { return r == '\n' || r == '\r' || r == '\t' || r == ' ' })
	}
	return "default", DefaultPrepPrompt()
}
