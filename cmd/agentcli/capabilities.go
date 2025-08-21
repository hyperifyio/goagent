package main

import (
	"encoding/json"
	"io"
	"os"
)

// printCapabilities prints a minimal JSON summary of tool manifest presence and exits 0.
// The detailed capabilities (including schema listing) are produced at runtime elsewhere;
// this helper focuses on a stable, testable output surface.
func printCapabilities(cfg cliConfig, stdout io.Writer, _ io.Writer) int {
	payload := map[string]any{
		"toolsManifest": map[string]any{
			"path":    cfg.toolsPath,
			"present": func() bool { return cfg.toolsPath != "" && fileExists(cfg.toolsPath) }(),
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		_, _ = io.WriteString(stdout, "{}\n")
		return 0
	}
	_, _ = io.WriteString(stdout, string(b)+"\n")
	return 0
}

func fileExists(p string) bool {
	if p == "" {
		return false
	}
	if _, err := os.Stat(p); err == nil {
		return true
	}
	return false
}
