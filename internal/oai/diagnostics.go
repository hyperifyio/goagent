package oai

import (
	"context"
	"errors"
	"strings"
)

// classifyHTTPCause returns a short cause label for audit based on error/context.
func classifyHTTPCause(ctx context.Context, err error) string {
	if err == nil {
		return "success"
	}
	if errors.Is(err, context.DeadlineExceeded) || (ctx != nil && ctx.Err() == context.DeadlineExceeded) {
		return "context_deadline"
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "server closed") || strings.Contains(s, "connection reset") || strings.Contains(s, "broken pipe"):
		return "server_closed"
	case strings.Contains(s, "timeout"):
		return "timeout"
	default:
		return "error"
	}
}

// userHintForCause returns a short actionable hint for common failure causes.
func userHintForCause(ctx context.Context, err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) || (ctx != nil && ctx.Err() == context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout") {
		return "increase -http-timeout or reduce prompt/model latency"
	}
	return ""
}
