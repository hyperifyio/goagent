package oai

import _ "embed"

//go:embed assets/prep_default.md
var prepDefaultPrompt string

// DefaultPrepPrompt returns the embedded default pre-stage prompt.
func DefaultPrepPrompt() string { return prepDefaultPrompt }
