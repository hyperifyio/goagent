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
