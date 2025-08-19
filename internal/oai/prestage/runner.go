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
	// Tag audit with stage label "prep" for observability
	ctx = oai.WithAuditStage(ctx, "prep")
	return r.Client.CreateChatCompletion(ctx, req)
}
