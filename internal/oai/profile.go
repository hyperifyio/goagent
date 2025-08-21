package oai

import "strings"

// PromptProfile enumerates supported prompt style presets.
// Valid values (case-insensitive): deterministic | general | creative | reasoning.
type PromptProfile string

const (
	ProfileDeterministic PromptProfile = "deterministic"
	ProfileGeneral       PromptProfile = "general"
	ProfileCreative      PromptProfile = "creative"
	ProfileReasoning     PromptProfile = "reasoning"
)

// MapProfileToTemperature returns the target temperature for a given profile
// and whether the temperature field should be included for the specified model.
//
// Rules:
// - deterministic => temperature 0.1 (when supported)
// - general | creative | reasoning => temperature 1.0 (when supported)
// - if the model does not support temperature, omit the field (false)
func MapProfileToTemperature(model string, profile PromptProfile) (float64, bool) {
	// Decide the desired temperature by profile (case-insensitive)
	p := strings.ToLower(string(profile))
	var desired float64
	switch p {
	case string(ProfileDeterministic):
		desired = 0.1
	case string(ProfileGeneral), string(ProfileCreative), string(ProfileReasoning):
		fallthrough
	default:
		desired = 1.0
	}
	// Respect model capability: omit temperature when unsupported
	if !SupportsTemperature(model) {
		return 0, false
	}
	// Clamp to allowed range to avoid surprises
	return clampTemperature(desired), true
}
