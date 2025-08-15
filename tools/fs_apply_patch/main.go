package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type applyPatchInput struct {
	UnifiedDiff string `json:"unifiedDiff"`
}

func main() {
	// Read all stdin (expected small JSON)
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "NOT_IMPLEMENTED: read error")
		os.Exit(1)
	}
	var in applyPatchInput
	if err := json.Unmarshal(data, &in); err != nil {
		fmt.Fprintln(os.Stderr, "NOT_IMPLEMENTED: invalid JSON")
		os.Exit(1)
	}
	if in.UnifiedDiff == "" {
		fmt.Fprintln(os.Stderr, "NOT_IMPLEMENTED: missing unifiedDiff")
		os.Exit(1)
	}
	// Stub implementation: advertise not implemented
	fmt.Fprintln(os.Stderr, "NOT_IMPLEMENTED: fs_apply_patch stub")
	os.Exit(1)
}
