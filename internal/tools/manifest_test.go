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
                // absolute path allowed in tests
                "command":     []string{"/bin/echo", "{}"},
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

// https://github.com/hyperifyio/goagent/issues/1
func TestLoadManifest_MissingNameOrCommand(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "tools.json")
	// Missing name
	data := `{"tools":[{"description":"x","command":["echo","{}"]}]}`
	if err := os.WriteFile(file, []byte(data), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := LoadManifest(file); err == nil {
		t.Fatalf("expected error for missing name")
	}

	// Missing command
	data = `{"tools":[{"name":"x"}]}`
	if err := os.WriteFile(file, []byte(data), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := LoadManifest(file); err == nil {
		t.Fatalf("expected error for missing command")
	}
}

// Harden validation: reject relative command[0] that escapes ./tools/bin or contains .. after normalization
func TestLoadManifest_CommandEscapeAndDotDot(t *testing.T) {
    dir := t.TempDir()
    file := filepath.Join(dir, "tools.json")

    cases := []struct {
        name     string
        command0 string
        wantErr  bool
    }{
        {name: "ok-absolute", command0: "/usr/bin/env", wantErr: false},
        // relative simple path must now be under ./tools/bin
        {name: "reject-simple-relative", command0: "echo", wantErr: true},
        {name: "ok-tools-bin", command0: "./tools/bin/fs_read_file", wantErr: false},
        {name: "reject-dotdot-leading", command0: "../tools/bin/get_time", wantErr: true},
        {name: "reject-escape-from-bin", command0: "./tools/bin/../hack", wantErr: true},
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            data := map[string]any{
                "tools": []map[string]any{
                    {
                        "name":    "t",
                        "command": []string{tc.command0},
                    },
                },
            }
            b, _ := json.Marshal(data)
            if err := os.WriteFile(file, b, 0o644); err != nil {
                t.Fatalf("write: %v", err)
            }
            _, _, err := LoadManifest(file)
            if tc.wantErr && err == nil {
                t.Fatalf("expected error for command0=%q", tc.command0)
            }
            if !tc.wantErr && err != nil {
                t.Fatalf("unexpected error for %q: %v", tc.command0, err)
            }
        })
    }
}
