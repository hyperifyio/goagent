package oai

import (
	"context"
	"errors"
	"net"
	"strings"
)

// isRetryableError returns true for transient network/timeouts.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	// Context deadline exceeded from client timeout
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) {
		if ne.Timeout() { // ne.Temporary is deprecated; avoid
			return true
		}
	}
    // *url.Error often wraps retryable errors; fall back to string contains
    // for common timeout phrasing used by standard library and proxies.
    s := strings.ToLower(err.Error())
    return strings.Contains(s, "timeout") || strings.Contains(s, "timed out")
}
