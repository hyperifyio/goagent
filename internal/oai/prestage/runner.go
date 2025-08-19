package prestage

import (
	"context"
	"time"

	"github.com/hyperifyio/goagent/internal/oai"
)

// Runner sends the pre-stage prompt to the model and returns the raw response.
// It is intentionally minimal and only wires the resolved prompt as the user message.
type Runner struct {
	Client *oai.Client
	Model  string
	// Optional knobs; callers may set appropriate values based on CLI flags.
	Temperature *float64
	TopP        *float64
	Timeout     time.Duration
	// When true, request JSON mode (response_format {type:"json_object"}) if supported.
	JSONMode bool
}

// Run executes a single non-streaming chat completion for the pre-stage using
// the provided resolved prompt text. The user message content will be exactly
// the provided prompt. The system message is omitted by default.
func (r *Runner) Run(ctx context.Context, prompt string) (oai.ChatCompletionsResponse, error) {
	req := oai.ChatCompletionsRequest{
		Model: r.Model,
		Messages: []oai.Message{
			{Role: oai.RoleUser, Content: prompt},
		},
		TopP:        r.TopP,
		Temperature: r.Temperature,
	}
	// Capability-based omissions for sampling knobs are handled by the client for temperature.
	// Enforce one‑knob rule here: if TopP is set, do not send Temperature at all.
	if r.TopP != nil {
		req.Temperature = nil
	}
	// Opt into JSON mode when requested; callers decide based on capability map.
	if r.JSONMode {
		req.ResponseFormat = &oai.ResponseFormat{Type: "json_object"}
	}
	// Tag audit with stage label "prep" for observability
	ctx = oai.WithAuditStage(ctx, "prep")
	return r.Client.CreateChatCompletion(ctx, req)
}
