package state

import (
	"testing"
)

func TestValidate_OK(t *testing.T) {
	b := &StateBundle{
		Version:     "1",
		CreatedAt:   "2025-08-19T00:00:00Z",
		ToolVersion: "dev",
		ModelID:     "gpt-5",
		BaseURL:     "https://api.openai.example/v1",
		ToolsetHash: "abc123",
		ScopeKey:    "scope",
		Prompts:     map[string]string{"system": "s"},
		SourceHash:  "deadbeef",
	}
	if err := b.Validate(); err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
}

func TestValidate_Errors(t *testing.T) {
	cases := []struct {
		name string
		b    StateBundle
	}{
		{"bad version", StateBundle{Version: "2", CreatedAt: "2025-08-19T00:00:00Z", ModelID: "m", BaseURL: "u", ScopeKey: "s"}},
		{"bad ts", StateBundle{Version: "1", CreatedAt: "not-time", ModelID: "m", BaseURL: "u", ScopeKey: "s"}},
		{"no model", StateBundle{Version: "1", CreatedAt: "2025-08-19T00:00:00Z", BaseURL: "u", ScopeKey: "s"}},
		{"no base", StateBundle{Version: "1", CreatedAt: "2025-08-19T00:00:00Z", ModelID: "m", ScopeKey: "s"}},
		{"no scope", StateBundle{Version: "1", CreatedAt: "2025-08-19T00:00:00Z", ModelID: "m", BaseURL: "u"}},
	}
	for _, tc := range cases {
		if err := tc.b.Validate(); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
}

func TestComputeSourceHash_Deterministic(t *testing.T) {
	got1 := ComputeSourceHash("m", "u", "t", "s")
	got2 := ComputeSourceHash("m", "u", "t", "s")
	if got1 != got2 {
		t.Fatalf("hash not deterministic: %s vs %s", got1, got2)
	}
	got3 := ComputeSourceHash("m2", "u", "t", "s")
	if got1 == got3 {
		t.Fatalf("hash should change on input change")
	}
}
