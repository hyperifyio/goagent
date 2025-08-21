package oai

import "testing"

func TestMapProfileToTemperature_SupportedModel(t *testing.T) {
	temp, ok := MapProfileToTemperature("gpt-5", PromptProfile("deterministic"))
	if !ok {
		t.Fatalf("expected ok for supported model")
	}
	if temp != 0.1 {
		t.Fatalf("deterministic => 0.1, got %v", temp)
	}

	temp, ok = MapProfileToTemperature("gpt-5", PromptProfile("creative"))
	if !ok || temp != 1.0 {
		t.Fatalf("creative => 1.0, got %v ok=%v", temp, ok)
	}
}

func TestMapProfileToTemperature_UnsupportedModel(t *testing.T) {
	if _, ok := MapProfileToTemperature("o3-mini", PromptProfile("deterministic")); ok {
		t.Fatalf("expected ok=false for unsupported model")
	}
}

func TestMapProfileToTemperature_UnknownProfile(t *testing.T) {
	if _, ok := MapProfileToTemperature("gpt-5", PromptProfile("unknown")); ok {
		t.Fatalf("expected ok=false for unknown profile")
	}
}
