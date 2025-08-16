package main

import (
    "bytes"
    "encoding/base64"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "os/exec"
    "path/filepath"
    "runtime"
    "testing"
    "time"

    "github.com/hyperifyio/goagent/internal/oai"
)

// https://github.com/hyperifyio/goagent/issues/89
func TestRunAgent_AdvertisesSchemas_AndExecutesFsWriteThenRead(t *testing.T) {
    // Resolve repository root so we can build tools with correct paths regardless of package CWD
    _, thisFile, _, ok := runtime.Caller(0)
    if !ok {
        t.Fatalf("runtime.Caller failed")
    }
    repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))

    // Build required tool binaries into a temp dir
    tmp := t.TempDir()
    fsWrite := filepath.Join(tmp, "fs_write_file")
    fsRead := filepath.Join(tmp, "fs_read_file")
    if out, err := exec.Command("go", "build", "-o", fsWrite, filepath.Join(repoRoot, "tools", "cmd", "fs_write_file")).CombinedOutput(); err != nil {
        t.Fatalf("build fs_write_file: %v: %s", err, string(out))
    }
    if out, err := exec.Command("go", "build", "-o", fsRead, filepath.Join(repoRoot, "tools", "cmd", "fs_read_file")).CombinedOutput(); err != nil {
        t.Fatalf("build fs_read_file: %v: %s", err, string(out))
    }

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
                "command":    []string{fsWrite},
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
                "command":    []string{fsRead},
                "timeoutSec": 5,
            },
        },
    }
    b, _ := json.Marshal(manifest)
    if err := os.WriteFile(toolsPath, b, 0o644); err != nil {
        t.Fatalf("write manifest: %v", err)
    }

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
