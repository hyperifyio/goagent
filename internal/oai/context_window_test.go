package oai

import "testing"

func TestContextWindowForModel_Known(t *testing.T) {
	if got := ContextWindowForModel("oss-gpt-20b"); got != 8192 {
		t.Fatalf("expected 8192 for oss-gpt-20b, got %d", got)
	}
}

func TestContextWindowForModel_DefaultOnUnknown(t *testing.T) {
	if got := ContextWindowForModel("unknown-model"); got != DefaultContextWindow {
		t.Fatalf("expected default %d for unknown, got %d", DefaultContextWindow, got)
	}
}

func TestContextWindowForModel_CaseInsensitivityAndTrim(t *testing.T) {
	if got := ContextWindowForModel("  OSS-GPT-20B  "); got != 8192 {
		t.Fatalf("expected 8192 for oss-gpt-20b with varied case/whitespace, got %d", got)
	}
}

func TestClampCompletionCap_WithinRemaining(t *testing.T) {
	window := 1000
	msgs := []Message{{Role: RoleUser, Content: "hello"}}
	// EstimateTokens("hello") ~ ceil(5/4)=2 + overhead 4 = 6; remaining ~ 1000-6-32=962
	cap := ClampCompletionCap(msgs, 100, window)
	if cap != 100 {
		t.Fatalf("expected cap to remain 100, got %d", cap)
	}
}

func TestClampCompletionCap_ClampsDownToRemaining(t *testing.T) {
	window := 200
	// Make a long prompt to force small remaining
	long := make([]byte, 600)
	for i := range long {
		long[i] = 'a'
	}
	msgs := []Message{{Role: RoleUser, Content: string(long)}}
	// Rough estimate ~ ceil(600/4)=150 + overhead 4 = 154; remaining ~ 200-154-32=14
	cap := ClampCompletionCap(msgs, 100, window)
	if cap != 14 {
		t.Fatalf("expected clamped cap 14, got %d", cap)
	}
}

func TestClampCompletionCap_NonPositiveRequestedUsesRemaining(t *testing.T) {
	window := 128
	msgs := []Message{{Role: RoleUser, Content: "hi"}}
	cap := ClampCompletionCap(msgs, 0, window)
	if cap <= 0 {
		t.Fatalf("expected positive cap, got %d", cap)
	}
}

func TestClampCompletionCap_MinimumOne(t *testing.T) {
	// Construct messages that nearly exhaust the window
	window := 64
	long := make([]byte, 1000)
	for i := range long {
		long[i] = 'a'
	}
	msgs := []Message{{Role: RoleUser, Content: string(long)}}
	cap := ClampCompletionCap(msgs, 5, window)
	if cap != 1 {
		t.Fatalf("expected minimum cap of 1, got %d", cap)
	}
}
