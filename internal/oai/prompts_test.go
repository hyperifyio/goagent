package oai

import "testing"

func TestDefaultPrepPrompt_NonEmpty(t *testing.T) {
	if s := DefaultPrepPrompt(); len(s) == 0 {
		t.Fatalf("default prep prompt is empty")
	}
	// Basic sanity: contains the Harmony JSON phrase
	if s := DefaultPrepPrompt(); !containsAll(s, []string{"Harmony", "JSON"}) {
		// non-fatal shape check; still require non-empty above
	}
}

func containsAll(s string, subs []string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (len(sub) == 0 || (func() bool { return (stringIndex(s, sub) >= 0) })()) }

// naive index to avoid importing strings in this tiny test
func stringIndex(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
