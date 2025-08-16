package oai

import "strings"

// SupportsTemperature reports whether the given model id accepts the
// temperature parameter. Defaults to true for forward compatibility.
// Known exceptions are listed explicitly below with brief rationale.
func SupportsTemperature(modelID string) bool {
    id := strings.ToLower(strings.TrimSpace(modelID))
    if id == "" {
        return true
    }
    // Known exceptions: OpenAI "o*" reasoning models ignore or reject sampling knobs.
    // We treat these as not supporting temperature to avoid 400s and no-op params.
    if strings.HasPrefix(id, "o3") || strings.HasPrefix(id, "o4") {
        return false
    }
    // Otherwise allow by default (e.g., GPT-5 variants, oss-gpt-*).
    return true
}
