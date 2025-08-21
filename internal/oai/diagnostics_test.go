package oai

import (
	"context"
	"errors"
	"testing"
)

func TestClassifyHTTPCause(t *testing.T) {
	if got := classifyHTTPCause(context.Background(), nil); got != "success" {
		t.Fatalf("expected success, got %s", got)
	}
	if got := classifyHTTPCause(context.Background(), context.DeadlineExceeded); got != "context_deadline" {
		t.Fatalf("expected context_deadline, got %s", got)
	}
	if got := classifyHTTPCause(context.Background(), errors.New("server closed connection")); got != "server_closed" {
		t.Fatalf("expected server_closed, got %s", got)
	}
	if got := classifyHTTPCause(context.Background(), errors.New("timeout while waiting")); got != "timeout" {
		t.Fatalf("expected timeout, got %s", got)
	}
	if got := classifyHTTPCause(context.Background(), errors.New("other")); got != "error" {
		t.Fatalf("expected error, got %s", got)
	}
}

func TestUserHintForCause(t *testing.T) {
	if got := userHintForCause(context.Background(), nil); got != "" {
		t.Fatalf("expected empty hint for nil error, got %q", got)
	}
	if got := userHintForCause(context.Background(), context.DeadlineExceeded); got == "" {
		t.Fatalf("expected hint for deadline exceeded")
	}
	if got := userHintForCause(context.Background(), errors.New("request timeout")); got == "" {
		t.Fatalf("expected hint for timeout error")
	}
}
