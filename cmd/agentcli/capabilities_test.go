package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// https://github.com/hyperifyio/goagent/issues/1
func TestPrintCapabilities_NoToolsPath(t *testing.T) {
	cfg := cliConfig{toolsPath: "", capabilities: true}
	var outBuf, errBuf bytes.Buffer
	code := printCapabilities(cfg, &outBuf, &errBuf)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, errBuf.String())
	}
	got := outBuf.String()
	if !strings.Contains(got, "No tools enabled") {
		t.Fatalf("unexpected stdout: %q", got)
	}
}

// https://github.com/hyperifyio/goagent/issues/1
func TestPrintCapabilities_WithManifest(t *testing.T) {
	dir := t.TempDir()
	toolsPath := filepath.Join(dir, "tools.json")
    manifest := map[string]any{
		"tools": []map[string]any{
			{"name": "btool", "description": "b desc", "schema": map[string]any{"type": "object"}, "command": []string{"/bin/true"}},
			{"name": "atool", "description": "a desc", "schema": map[string]any{"type": "object"}, "command": []string{"/bin/true"}},
            {"name": "img_create", "description": "Generate images", "schema": map[string]any{"type": "object"}, "command": []string{"/bin/true"}},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(toolsPath, data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	cfg := cliConfig{toolsPath: toolsPath, capabilities: true}
	var outBuf, errBuf bytes.Buffer
	code := printCapabilities(cfg, &outBuf, &errBuf)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, errBuf.String())
	}
	got := outBuf.String()
    // Should include warning and sorted tool names (atool before btool)
	if !strings.Contains(got, "Capabilities (enabled tools):") {
		t.Fatalf("capabilities header missing: %q", got)
	}
	aIdx := strings.Index(got, "- atool: a desc")
	bIdx := strings.Index(got, "- btool: b desc")
	if aIdx < 0 || bIdx < 0 || aIdx > bIdx {
		t.Fatalf("tools not listed or not sorted: %q", got)
	}
    // Ensure img_create has explicit network/file warning
    if !strings.Contains(got, "- img_create: Generate images [WARNING: makes outbound network calls and can save files]") {
        t.Fatalf("img_create warning missing or incorrect: %q", got)
    }
}
