package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// https://github.com/hyperifyio/goagent/issues/1
func TestLoadManifest_OK(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "tools.json")
	data := map[string]any{
		"tools": []map[string]any{
			{
				"name":        "hello",
				"description": "says hello",
				"schema":      map[string]any{"type": "object"},
				"command":     []string{"echo", "{}"},
			},
		},
	}
	b, _ := json.Marshal(data)
	if err := os.WriteFile(file, b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	reg, tools, err := LoadManifest(file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reg) != 1 || len(tools) != 1 {
		t.Fatalf("unexpected sizes: reg=%d tools=%d", len(reg), len(tools))
	}
}

func TestLoadManifest_DuplicateName(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "tools.json")
	data := `{"tools":[{"name":"x","command":["echo","{}"]},{"name":"x","command":["echo","{}"]}]}`
	if err := os.WriteFile(file, []byte(data), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, _, err := LoadManifest(file)
	if err == nil {
		t.Fatalf("expected error for duplicate names")
	}
}
