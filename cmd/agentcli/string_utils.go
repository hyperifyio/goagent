package main

import "strings"

func oneLine(s string) string {
	// Collapse newlines and tabs
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	// Collapse repeated spaces
	return strings.Join(strings.Fields(s), " ")
}

// nonEmptyOr returns a when non-empty, otherwise b.
func nonEmptyOr(a, b string) string {
	if strings.TrimSpace(a) == "" {
		return b
	}
	return a
}
