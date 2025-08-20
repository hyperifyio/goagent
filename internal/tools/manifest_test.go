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
				"command": []string{"/bin/echo", "{}"},
				// envPassthrough should be normalized, deduplicated
				"envPassthrough": []string{"oai_api_key", "OAI_API_KEY", " Path ", "TZ"},
			},
		},
	}
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
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
	spec := reg["hello"]
	want := []string{"OAI_API_KEY", "PATH", "TZ"}
	if len(spec.EnvPassthrough) != len(want) {
		t.Fatalf("envPassthrough len: got %d want %d", len(spec.EnvPassthrough), len(want))
	}
	for i := range want {
		if spec.EnvPassthrough[i] != want[i] {
			t.Fatalf("envPassthrough[%d]: got %q want %q", i, spec.EnvPassthrough[i], want[i])
		}
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
		// Windows-style backslashes that normalize to an escape must be rejected
		{name: "reject-windows-backslash-escape", command0: ".\\tools\\bin\\..\\hack", wantErr: true},
		// Windows-style acceptable path under tools/bin should be accepted after normalization
		{name: "ok-windows-backslash-tools-bin", command0: ".\\tools\\bin\\fs_read_file", wantErr: false},
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
			b, err2 := json.Marshal(data)
			if err2 != nil {
				t.Fatalf("marshal: %v", err2)
			}
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

func TestLoadManifest_InvalidEnvPassthrough(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "tools.json")
	// Invalid names: leading digit and dash inside
	data := map[string]any{
		"tools": []map[string]any{
			{
				"name":           "t",
				"command":        []string{"/bin/true"},
				"envPassthrough": []string{"1BAD", "GOOD", "OAI-API-KEY"},
			},
		},
	}
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(file, b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := LoadManifest(file); err == nil {
		t.Fatalf("expected error for invalid envPassthrough entries")
	}
}

// Relative command paths must resolve against the manifest directory, not process CWD.
// The loader should rewrite command[0] to an absolute path rooted at the manifest's folder.
func TestLoadManifest_ResolvesRelativeAgainstManifestDir(t *testing.T) {
	// Create nested manifest directory
	base := t.TempDir()
	nested := filepath.Join(base, "configs", "sub")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	// Create a fake tools/bin tree relative to the manifest
	binDir := filepath.Join(nested, "tools", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	// Create a small executable file to represent the tool binary
	toolPath := filepath.Join(binDir, "hello_tool")
	if err := os.WriteFile(toolPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write tool bin: %v", err)
	}
	// Write manifest that references ./tools/bin/hello_tool relative to the manifest dir
	manPath := filepath.Join(nested, "tools.json")
	data := map[string]any{
		"tools": []map[string]any{
			{
				"name":    "hello",
				"command": []string{"./tools/bin/hello_tool"},
			},
		},
	}
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(manPath, b, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	// Change working directory to a different location to ensure CWD is not used for resolution
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	other := filepath.Join(base, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatalf("mkdir other: %v", err)
	}
	if err := os.Chdir(other); err != nil {
		t.Fatalf("chdir other: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Logf("chdir restore: %v", err)
		}
	})

	reg, _, err := LoadManifest(manPath)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	spec, ok := reg["hello"]
	if !ok {
		t.Fatalf("missing tool in registry")
	}
	if len(spec.Command) == 0 {
		t.Fatalf("empty command")
	}
	got := spec.Command[0]
	if !filepath.IsAbs(got) {
		t.Fatalf("command[0] not absolute: %q", got)
	}
	// It should point to the tool under the manifest's directory, not under CWD
	if got != toolPath {
		t.Fatalf("resolved path mismatch:\n got: %s\nwant: %s", got, toolPath)
	}
}
