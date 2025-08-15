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
    var out, err bytes.Buffer
    code := printCapabilities(cfg, &out, &err)
    if code != 0 {
        t.Fatalf("expected exit code 0, got %d; stderr=%q", code, err.String())
    }
    got := out.String()
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
        },
    }
    data, _ := json.Marshal(manifest)
    if err := os.WriteFile(toolsPath, data, 0o644); err != nil {
        t.Fatalf("write manifest: %v", err)
    }

    cfg := cliConfig{toolsPath: toolsPath, capabilities: true}
    var out, err bytes.Buffer
    code := printCapabilities(cfg, &out, &err)
    if code != 0 {
        t.Fatalf("expected exit code 0, got %d; stderr=%q", code, err.String())
    }
    got := out.String()
    // Should include warning and sorted tool names (atool before btool)
    if !strings.Contains(got, "Capabilities (enabled tools):") {
        t.Fatalf("capabilities header missing: %q", got)
    }
    aIdx := strings.Index(got, "- atool: a desc")
    bIdx := strings.Index(got, "- btool: b desc")
    if aIdx < 0 || bIdx < 0 || aIdx > bIdx {
        t.Fatalf("tools not listed or not sorted: %q", got)
    }
}
