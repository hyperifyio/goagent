package main

import (
	"testing"
	"time"
)

func TestParseDurationFlexible(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"500ms", 500 * time.Millisecond, false},
		{"2s", 2 * time.Second, false},
		{"30", 30 * time.Second, false},
		{"  15  ", 15 * time.Second, false},
		{"-1", 0, true},
		{"", 0, true},
		{"abc", 0, true},
	}
	for _, tc := range cases {
		got, err := parseDurationFlexible(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("parseDurationFlexible(%q) expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseDurationFlexible(%q) unexpected error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("parseDurationFlexible(%q) = %v; want %v", tc.in, got, tc.want)
		}
	}
}
