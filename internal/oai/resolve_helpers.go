package oai

import (
    "strconv"
    "strings"
)

// ResolveString resolves a string value with precedence:
// flag > env > inheritFrom > default. Trims whitespace from flag and env.
// Returns the resolved value and a source label: "flag" | "env" | "inherit" | "default".
func ResolveString(flagValue string, envValue string, inheritFrom *string, def string) (string, string) {
    if v := strings.TrimSpace(flagValue); v != "" {
        return v, "flag"
    }
    if v := strings.TrimSpace(envValue); v != "" {
        return v, "env"
    }
    if inheritFrom != nil {
        return strings.TrimSpace(*inheritFrom), "inherit"
    }
    return def, "default"
}

// ResolveBool resolves a bool with precedence:
// flag (when flagSet) > env (parseable) > inheritFrom > default.
// Returns the resolved value and a source label.
func ResolveBool(flagSet bool, flagValue bool, envValue string, inheritFrom *bool, def bool) (bool, string) {
    if flagSet {
        return flagValue, "flag"
    }
    if s := strings.TrimSpace(envValue); s != "" {
        if b, err := strconv.ParseBool(s); err == nil {
            return b, "env"
        }
    }
    if inheritFrom != nil {
        return *inheritFrom, "inherit"
    }
    return def, "default"
}
