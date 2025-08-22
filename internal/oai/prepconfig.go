package oai

// PrepConfig holds resolved configuration for the pre-stage flow.
// Currently it includes only the prepared prompt text.
type PrepConfig struct {
    // Prompt is the finalized pre-stage prompt after applying overrides.
    // When multiple prompt sources are provided, they are concatenated using
    // JoinPrompts and stored here.
    Prompt string
}

// NewPrepConfig constructs a PrepConfig with Prompt set to the normalized
// concatenation of the provided parts.
func NewPrepConfig(parts []string) PrepConfig {
    return PrepConfig{Prompt: JoinPrompts(parts)}
}
