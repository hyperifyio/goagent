package oai

import (
    "context"
    "errors"
    "net"
    "testing"
)

// netError is a small helper implementing net.Error to simulate timeouts.
type netError struct{ timeout bool }

func (e netError) Error() string   { return "simulated" }
func (e netError) Timeout() bool   { return e.timeout }
func (e netError) Temporary() bool { return false }

func TestIsRetryableError_NetTimeoutAndContext(t *testing.T) {
    if !isRetryableError(context.DeadlineExceeded) {
        t.Fatalf("context deadline should be retryable")
    }
    if !isRetryableError(context.Canceled) {
        t.Fatalf("context canceled should be retryable")
    }
    var ne net.Error = netError{timeout: true}
    if !isRetryableError(ne) {
        t.Fatalf("net timeout should be retryable")
    }
    if isRetryableError(errors.New("permanent failure")) {
        t.Fatalf("generic error should not be retryable")
    }
}

func TestIsRetryableError_StringTimeoutFallback(t *testing.T) {
    // String contains fallback for wrapped url.Error style
    if !isRetryableError(errors.New("request timed out while awaiting headers")) {
        t.Fatalf("string timeout detection should be retryable")
    }
    // Ensure non-timeout strings are not treated as retryable
    if isRetryableError(errors.New("unrelated error")) {
        t.Fatalf("non-timeout string should not be retryable")
    }
}
