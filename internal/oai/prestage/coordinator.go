package prestage

import (
	"context"
	"strings"

	"github.com/hyperifyio/goagent/internal/oai"
	"github.com/hyperifyio/goagent/internal/state"
)

// PrestageRunner abstracts the runner so tests can stub it.
// The concrete Runner in runner.go implements this interface.
type PrestageRunner interface {
	Run(ctx context.Context, prompt string) (oai.ChatCompletionsResponse, error)
}

// Coordinator wires restore-before-prep behavior with override precedence.
// If a state bundle is available and refinement is not requested and there are
// no explicit overrides, Coordinator will reuse the persisted prompts and
// settings without invoking the runner.
type Coordinator struct {
	// Optional state directory. When empty, restore is disabled.
	StateDir string
	// Optional scope key; when set, restored bundle must match this key.
	ScopeKey string
	// When true, forces a new pre-stage run instead of restoring.
	Refine bool
	// Overrides from CLI: explicit prompt strings and pre-joined file contents.
	PrepPrompts     []string
	PrepFilesJoined string

	// Runner used when a live pre-stage call is required.
	Runner PrestageRunner

	// Warnf, when set, is used to emit a single-line warning message.
	// It is called at most once per Execute() invocation.
	Warnf func(format string, args ...any)
}

// Outcome captures the result of Execute.
type Outcome struct {
	// UsedRestore indicates a persisted bundle was reused and Runner was not called.
	UsedRestore bool
	// Restored is the bundle that was reused when UsedRestore is true.
	Restored *state.StateBundle
	// PromptUsed is the prompt text sent to Runner when UsedRestore is false.
	PromptUsed string
	// Response is the model response when Runner was called.
	Response oai.ChatCompletionsResponse
}

// Execute performs restore-before-prep logic.
// Precedence for the effective pre-stage prompt source:
//  1. explicit prompt overrides (PrepPrompts)
//  2. file-based overrides (PrepFilesJoined)
//  3. restored bundle (when available, ScopeKey matches, and Refine==false)
//  4. embedded default
//
// Note: This function does not save state; persistence is handled elsewhere.
func (c *Coordinator) Execute(ctx context.Context) (Outcome, error) {
	var out Outcome

	// If overrides are present, they take precedence and force a live call.
	overrideSource, overrideText := oai.ResolvePrepPrompt(c.PrepPrompts, c.PrepFilesJoined)
	overridesProvided := overrideSource == "override" && strings.TrimSpace(overrideText) != ""

	// If refine is requested but explicit overrides are provided, warn and proceed.
	if overridesProvided && c.Refine && c.Warnf != nil {
		c.Warnf("pre-stage: explicit overrides provided while -state-refine is set; proceeding with overrides")
	}

	if !overridesProvided && !c.Refine && strings.TrimSpace(c.StateDir) != "" {
		if b, err := state.LoadLatestStateBundle(c.StateDir); err == nil && b != nil {
			if strings.TrimSpace(c.ScopeKey) == "" || b.ScopeKey == c.ScopeKey {
				out.UsedRestore = true
				out.Restored = b
				return out, nil
			}
			// scope mismatch → ignore and fall through
		}
		// On any load error, fall through to live run
	}

	// Determine the prompt to use for a live pre-stage run
	_, prompt := oai.ResolvePrepPrompt(c.PrepPrompts, c.PrepFilesJoined)

	out.PromptUsed = prompt
	if c.Runner != nil {
		resp, err := c.Runner.Run(ctx, prompt)
		if err != nil {
			return out, err
		}
		out.Response = resp
	}
	return out, nil
}
