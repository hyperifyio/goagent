package main

import (
	"strconv"
	"strings"
	"time"
)

// parseDurationFlexible accepts either standard Go duration strings
// (e.g., "750ms", "3s") or plain integer seconds (e.g., "30").
// Returns a non-positive duration only on parse errors to keep callers explicit.
func parseDurationFlexible(raw string) (time.Duration, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, strconv.ErrSyntax
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	// Accept plain integer seconds
	allDigits := true
	for _, r := range s {
		if r < '0' || r > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, err
		}
		if n < 0 {
			// Allow zero; negative is invalid
			return 0, strconv.ErrRange
		}
		return time.Duration(n) * time.Second, nil
	}
	return 0, strconv.ErrSyntax
}
