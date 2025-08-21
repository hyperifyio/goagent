package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPrintCapabilities_PresentAndAbsent(t *testing.T) {
	var out bytes.Buffer
	cfg := cliConfig{}
	// Absent path
	cfg.toolsPath = filepath.Join(t.TempDir(), "missing.json")
	printCapabilities(cfg, &out, &out)
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil { t.Fatalf("json: %v", err) }
	tm := m["toolsManifest"].(map[string]any)
	if tm["present"].(bool) {
		t.Fatalf("expected present=false for nonexistent path")
	}
	// Present path
	out.Reset()
	d := t.TempDir()
	p := filepath.Join(d, "tools.json")
	if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil { t.Fatalf("prep: %v", err) }
	cfg.toolsPath = p
	printCapabilities(cfg, &out, &out)
	if err := json.Unmarshal(out.Bytes(), &m); err != nil { t.Fatalf("json: %v", err) }
	tm = m["toolsManifest"].(map[string]any)
	if !tm["present"].(bool) {
		t.Fatalf("expected present=true for existing path")
	}
}
