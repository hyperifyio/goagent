package oai

import "testing"

// Table-driven tests for SupportsTemperature across true/false outcomes.
func TestSupportsTemperature(t *testing.T) {
    tests := []struct {
        name   string
        model  string
        expect bool
    }{
        {name: "empty => default true", model: "", expect: true},
        {name: "oss gpt variant => true", model: "oss-gpt-20b", expect: true},
        {name: "gpt-5 family => true", model: "gpt-5.0-pro", expect: true},
        {name: "o3 reasoning => false", model: "o3-mini", expect: false},
        {name: "o4 reasoning => false", model: "o4-heavy", expect: false},
        {name: "case-insensitive match", model: "O3-MINI", expect: false},
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            got := SupportsTemperature(tc.model)
            if got != tc.expect {
                t.Fatalf("SupportsTemperature(%q)=%v want %v", tc.model, got, tc.expect)
            }
        })
    }
}
