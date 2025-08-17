package oai

import "testing"

func TestMapProfileToTemperature_SupportedModel(t *testing.T) {
	model := "oss-gpt-20b" // supports temperature by default
	cases := []struct {
		name     string
		profile  PromptProfile
		wantTemp float64
		wantOK   bool
	}{
		{"deterministic->0.1", ProfileDeterministic, 0.1, true},
		{"general->1.0", ProfileGeneral, 1.0, true},
		{"creative->1.0", ProfileCreative, 1.0, true},
		{"reasoning->1.0", ProfileReasoning, 1.0, true},
		{"unknown-defaults-to-1.0", PromptProfile("weird"), 1.0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := MapProfileToTemperature(model, tc.profile)
			if ok != tc.wantOK {
				t.Fatalf("ok mismatch: got %v want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if got != tc.wantTemp {
				t.Fatalf("temperature mismatch: got %v want %v", got, tc.wantTemp)
			}
		})
	}
}

func TestMapProfileToTemperature_UnsupportedModelOmits(t *testing.T) {
	// Any o4* model is treated as not supporting temperature
	model := "o4-mini"
	if temp, ok := MapProfileToTemperature(model, ProfileDeterministic); ok {
		t.Fatalf("expected omit for unsupported model; got temp=%v ok=%v", temp, ok)
	}
	if temp, ok := MapProfileToTemperature(model, ProfileGeneral); ok {
		t.Fatalf("expected omit for unsupported model; got temp=%v ok=%v", temp, ok)
	}
}
