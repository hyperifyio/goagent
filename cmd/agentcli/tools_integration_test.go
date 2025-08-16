package main

import (
    "bytes"
    "encoding/base64"
    "encoding/json"
    "io"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/hyperifyio/goagent/internal/oai"
    testutil "github.com/hyperifyio/goagent/tools/testutil"
)

// https://github.com/hyperifyio/goagent/issues/89
func TestRunAgent_AdvertisesSchemas_AndExecutesFsWriteThenRead(t *testing.T) {
    // Build required tool binaries into a temp dir under canonical layout tools/bin
	tmp := t.TempDir()
	binDir := filepath.Join(tmp, "tools", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir tools/bin: %v", err)
	}
	fsWriteBin := filepath.Join(binDir, "fs_write_file")
	fsReadBin := filepath.Join(binDir, "fs_read_file")
    // Use the canonical test helper to build tool binaries
    srcWrite := testutil.BuildTool(t, "fs_write_file")
    srcRead := testutil.BuildTool(t, "fs_read_file")
    // Copy built binaries into the expected temp location with canonical names
    copyTo := func(src, dst string) {
        in, err := os.Open(src)
        if err != nil {
            t.Fatalf("open %s: %v", src, err)
        }
        defer in.Close()
        out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
        if err != nil {
            t.Fatalf("create %s: %v", dst, err)
        }
        defer out.Close()
        if _, err := io.Copy(out, in); err != nil {
            t.Fatalf("copy %s -> %s: %v", src, dst, err)
        }
    }
    copyTo(srcWrite, fsWriteBin)
    copyTo(srcRead, fsReadBin)

	// Create a tools manifest referencing the built binaries
	toolsPath := filepath.Join(tmp, "tools.json")
	manifest := map[string]any{
		"tools": []map[string]any{
			{
				"name":        "fs_write_file",
				"description": "Atomically write a file (base64)",
				"schema": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"path", "contentBase64"},
					"properties": map[string]any{
						"path":            map[string]any{"type": "string"},
						"contentBase64":   map[string]any{"type": "string"},
						"createModeOctal": map[string]any{"type": "string"},
					},
				},
				// Use relative canonical path so manifest validation enforces ./tools/bin prefix
				"command":    []string{"./tools/bin/fs_write_file"},
				"timeoutSec": 5,
			},
			{
				"name":        "fs_read_file",
				"description": "Read a file (base64)",
				"schema": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"path"},
					"properties": map[string]any{
						"path":        map[string]any{"type": "string"},
						"offsetBytes": map[string]any{"type": "integer"},
						"maxBytes":    map[string]any{"type": "integer"},
					},
				},
				// Use relative canonical path so manifest validation enforces ./tools/bin prefix
				"command":    []string{"./tools/bin/fs_read_file"},
				"timeoutSec": 5,
			},
		},
	}
	b, _ := json.Marshal(manifest)
	if err := os.WriteFile(toolsPath, b, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	// Change working directory to the temp dir so relative ./tools/bin/* resolve
	oldWD, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	// Prepare the file path and content for the tool calls (relative to current working directory)
	targetRelPath := "tmp_tools_it_demo.txt"
	// Ensure cleanup
	t.Cleanup(func() { _ = os.Remove(targetRelPath) })
	content := []byte("hello world")
	contentB64 := base64.StdEncoding.EncodeToString(content)

	// Fake server: first response asserts tools advertised and returns two tool calls;
	// second response returns the final assistant message
	var step int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var req oai.ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("bad json: %v", err)
		}
		step++
		switch step {
		case 1:
			// Verify schemas are advertised for both tools
			have := map[string]bool{}
			for _, tl := range req.Tools {
				have[tl.Function.Name] = true
				if len(bytes.TrimSpace(tl.Function.Parameters)) == 0 {
					t.Fatalf("tool %q missing schema parameters", tl.Function.Name)
				}
			}
			if !have["fs_write_file"] || !have["fs_read_file"] {
				t.Fatalf("advertised tools missing: %v", have)
			}
			// Respond with two tool calls: write then read
			resp := oai.ChatCompletionsResponse{
				ID:      "cmpl-1",
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []oai.ChatCompletionsResponseChoice{{
					Index:        0,
					FinishReason: "tool_calls",
					Message: oai.Message{
						Role: oai.RoleAssistant,
						ToolCalls: []oai.ToolCall{
							{ID: "1", Type: "function", Function: oai.ToolCallFunction{Name: "fs_write_file", Arguments: `{"path":"` + targetRelPath + `","contentBase64":"` + contentB64 + `"}`}},
							{ID: "2", Type: "function", Function: oai.ToolCallFunction{Name: "fs_read_file", Arguments: `{"path":"` + targetRelPath + `"}`}},
						},
					},
				}},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case 2:
			// Final message
			resp := oai.ChatCompletionsResponse{
				ID:      "cmpl-2",
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   req.Model,
				Choices: []oai.ChatCompletionsResponseChoice{{
					Index:        0,
					FinishReason: "stop",
					Message:      oai.Message{Role: oai.RoleAssistant, Content: "ok"},
				}},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			t.Fatalf("unexpected extra request step=%d", step)
		}
	}))
	defer srv.Close()

	cfg := cliConfig{
		prompt:       "write and read a file",
		toolsPath:    toolsPath,
		systemPrompt: "sys",
		baseURL:      srv.URL,
		apiKey:       "",
		model:        "test",
		maxSteps:     4,
		timeout:      10 * time.Second,
		temperature:  0,
		debug:        false,
	}

	var outBuf, errBuf bytes.Buffer
	code := runAgent(cfg, &outBuf, &errBuf)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%s", code, errBuf.String())
	}
	if got := outBuf.String(); got != "ok\n" {
		t.Fatalf("unexpected stdout: %q", got)
	}
	// Verify the file was created with expected content
	got, err := os.ReadFile(targetRelPath)
	if err != nil {
		t.Fatalf("read created file: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("file content mismatch: got %q want %q", string(got), string(content))
	}
}
