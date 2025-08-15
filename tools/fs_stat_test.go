package main

// https://github.com/hyperifyio/goagent/issues/1

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
	"os/exec"
)

type fsStatOutput struct {
	Exists    bool   `json:"exists"`
	Type      string `json:"type"`
	SizeBytes int64  `json:"sizeBytes"`
	ModeOctal string `json:"modeOctal"`
	ModTime   string `json:"modTime"`
	SHA256    string `json:"sha256,omitempty"`
}

// buildFsStatTool builds ./tools/fs_stat into a temporary binary.
func buildFsStatTool(t *testing.T) string {
					t.Helper()
					tmpDir := t.TempDir()
					binPath := filepath.Join(tmpDir, "fs-stat")
					cmd := exec.Command("go", "build", "-o", binPath, "./fs_stat")
					cmd.Dir = "."
					out, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("failed to build fs_stat tool: %v\n%s", err, string(out))
					}
					return binPath
}

// runFsStat runs the built fs_stat tool with the given JSON input and decodes stdout.
func runFsStat(t *testing.T, bin string, input any) (fsStatOutput, string, int) {
					t.Helper()
					data, err := json.Marshal(input)
					if err != nil {
						t.Fatalf("marshal input: %v", err)
					}
					cmd := exec.Command(bin)
					cmd.Dir = "."
					cmd.Stdin = bytes.NewReader(data)
					var stdout, stderr bytes.Buffer
					cmd.Stdout = &stdout
					cmd.Stderr = &stderr
					err = cmd.Run()
					code := 0
					if err != nil {
						if ee, ok := err.(*exec.ExitError); ok {
							code = ee.ExitCode()
						} else {
							code = 1
						}
					}
					var out fsStatOutput
					_ = json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &out)
					return out, stderr.String(), code
}

// TestFsStat_File expresses the minimal contract: for an existing regular file,
// the tool exits 0 and reports exists=true, type="file", and sizeBytes.
func TestFsStat_File(t *testing.T) {
	bin := buildFsStatTool(t)

	content := []byte("hello-fsstat")
	path := makeRepoRelTempFile(t, "fsstat-file-", content)

	out, stderr, code := runFsStat(t, bin, map[string]any{
		"path": path,
	})
	if code != 0 {
		t.Fatalf("expected success, got exit=%d stderr=%q", code, stderr)
	}
	if !out.Exists {
		t.Fatalf("expected exists=true, got false")
	}
	if out.Type != "file" {
		t.Fatalf("expected type=file, got %q", out.Type)
	}
	if out.SizeBytes != int64(len(content)) {
		t.Fatalf("sizeBytes mismatch: got %d want %d", out.SizeBytes, len(content))
	}
}

// TestFsStat_MissingPath verifies that a non-existent path is handled
// gracefully: exit code 0 and exists=false in the JSON output.
func TestFsStat_MissingPath(t *testing.T) {
    bin := buildFsStatTool(t)

    // Use a path name that is very unlikely to exist under repo root.
    missing := filepath.Join("fsstat-missing-", "no-such-file.bin")

    out, stderr, code := runFsStat(t, bin, map[string]any{
        "path": missing,
    })
    if code != 0 {
        t.Fatalf("expected success (exit 0) for missing path, got exit=%d stderr=%q", code, stderr)
    }
    if out.Exists {
        t.Fatalf("expected exists=false for missing path")
    }
}
