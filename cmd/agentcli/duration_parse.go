package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseDurationFlexible accepts either standard Go duration strings (e.g., "500ms", "2s")
// or plain integers meaning seconds (e.g., "30" -> 30s).
func parseDurationFlexible(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n < 0 {
			return 0, fmt.Errorf("negative duration seconds: %d", n)
		}
		return time.Duration(n) * time.Second, nil
	}
	return 0, fmt.Errorf("invalid duration: %q", s)
}
