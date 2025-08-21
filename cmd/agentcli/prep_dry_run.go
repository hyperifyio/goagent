package main

import (
	"encoding/json"
	"io"
)

// runPrepDryRun emits a minimal refined message array in JSON and exits 0.
// This keeps the CLI behavior deterministic in tests without requiring network calls.
func runPrepDryRun(cfg cliConfig, stdout io.Writer, _ io.Writer) int {
	// Simple seed with system and user messages similar to runAgent pre-flight.
	msgs := []map[string]any{
		{"role": "system", "content": cfg.systemPrompt},
		{"role": "user", "content": cfg.prompt},
	}
	_ = json.NewEncoder(stdout).Encode(msgs)
	return 0
}
