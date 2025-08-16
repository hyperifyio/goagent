package examples

import (
    "bytes"
    "encoding/base64"
    "encoding/json"
    "io"
    "net/http"
    "net/http/httptest"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"
    "time"

    "github.com/hyperifyio/goagent/internal/oai"
    testutil "github.com/hyperifyio/goagent/tools/testutil"
)

// findRepoRoot walks up from the current directory to locate go.mod.
func findRepoRoot(t *testing.T) string {
    t.Helper()
    dir, err := os.Getwd()
    if err != nil || dir == "" {
        t.Fatalf("getwd: %v", err)
    }
    for {
        if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
            return dir
        }
        parent := filepath.Dir(dir)
        if parent == dir {
            t.Fatalf("go.mod not found from %s upward", dir)
        }
        dir = parent
    }
}

// TestWorkedExample_ToolCalls_TemperatureOne_Sequencing builds the CLI and two tools,
// runs against a fake server that returns two tool_calls, and verifies:
// - default temperature propagates as 1.0
// - tool messages are appended with matching tool_call_id
// - debug transcript includes request/response dumps (used as a transcript example)
func TestWorkedExample_ToolCalls_TemperatureOne_Sequencing(t *testing.T) {
    _ = findRepoRoot(t)

    // Build agent CLI binary from repo root for correctness
    tmp := t.TempDir()
    agentBin := filepath.Join(tmp, "agentcli")
    cmdBuild := exec.Command("go", "build", "-o", agentBin, "./cmd/agentcli")
    cmdBuild.Dir = findRepoRoot(t)
    if out, err := cmdBuild.CombinedOutput(); err != nil {
        t.Fatalf("build agentcli: %v: %s", err, string(out))
    }

    // Build required tool binaries using canonical helper, then copy under ./tools/bin
    toolsDir := filepath.Join(tmp, "tools", "bin")
    if err := os.MkdirAll(toolsDir, 0o755); err != nil {
        t.Fatalf("mkdir tools/bin: %v", err)
    }
    srcWrite := testutil.BuildTool(t, "fs_write_file")
    srcRead := testutil.BuildTool(t, "fs_read_file")
    mustCopy := func(src, dst string) {
        in, err := os.Open(src)
        if err != nil { t.Fatalf("open %s: %v", src, err) }
        defer in.Close()
        out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
        if err != nil { t.Fatalf("create %s: %v", dst, err) }
        if _, err := io.Copy(out, in); err != nil { t.Fatalf("copy %s -> %s: %v", src, dst, err) }
        if err := out.Close(); err != nil { t.Fatalf("close out: %v", err) }
    }
    mustCopy(srcWrite, filepath.Join(toolsDir, "fs_write_file"))
    mustCopy(srcRead, filepath.Join(toolsDir, "fs_read_file"))

    // Create tools.json manifest that references ./tools/bin/*
    manifestPath := filepath.Join(tmp, "tools.json")
    man := map[string]any{
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
                "command":    []string{"./tools/bin/fs_read_file"},
                "timeoutSec": 5,
            },
        },
    }
    if b, err := json.Marshal(man); err != nil {
        t.Fatalf("marshal manifest: %v", err)
    } else if err := os.WriteFile(manifestPath, b, 0o644); err != nil {
        t.Fatalf("write manifest: %v", err)
    }

    // Prepare target file and content for tool calls (relative to tmp dir)
    targetRel := "worked_example.txt"
    content := []byte("hello example")
    contentB64 := base64.StdEncoding.EncodeToString(content)

    // Fake server with two steps
    var step int
    var sawToolIDs map[string]bool
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
            t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
        }
        var req oai.ChatCompletionsRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            t.Fatalf("decode: %v", err)
        }
        step++
        switch step {
        case 1:
            if req.Temperature == nil || *req.Temperature != 1.0 {
                if req.Temperature == nil {
                    t.Fatalf("temperature missing in request; want 1.0")
                }
                t.Fatalf("temperature got %v want 1.0", *req.Temperature)
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
                            {ID: "1", Type: "function", Function: oai.ToolCallFunction{Name: "fs_write_file", Arguments: `{"path":"` + targetRel + `","contentBase64":"` + contentB64 + `"}`}},
                            {ID: "2", Type: "function", Function: oai.ToolCallFunction{Name: "fs_read_file", Arguments: `{"path":"` + targetRel + `"}`}},
                        },
                    },
                }},
            }
            if err := json.NewEncoder(w).Encode(resp); err != nil {
                t.Fatalf("encode step1: %v", err)
            }
        case 2:
            // Verify that tool messages with matching ids were appended
            sawToolIDs = map[string]bool{"1": false, "2": false}
            for _, m := range req.Messages {
                if m.Role == oai.RoleTool {
                    if _, ok := sawToolIDs[m.ToolCallID]; ok {
                        sawToolIDs[m.ToolCallID] = true
                    }
                    if strings.TrimSpace(m.Content) == "" {
                        t.Fatalf("tool message content empty for id %s", m.ToolCallID)
                    }
                }
            }
            // Final assistant message
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
            if err := json.NewEncoder(w).Encode(resp); err != nil {
                t.Fatalf("encode step2: %v", err)
            }
        default:
            t.Fatalf("unexpected extra request step=%d", step)
        }
    }))
    defer srv.Close()

    // Run the agent binary with -debug to emit transcript-style dumps
    var stdout, stderr bytes.Buffer
    cmd := exec.Command(agentBin,
        "-prompt", "write and read a file",
        "-tools", manifestPath,
        "-base-url", srv.URL,
        "-model", "test",
        "-max-steps", "4",
        "-http-timeout", "5s",
        "-tool-timeout", "5s",
        "-debug",
    )
    cmd.Dir = tmp // so relative file path lands here
    cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr
    if err := cmd.Run(); err != nil {
        t.Fatalf("agent run error: %v; stdout=%s stderr=%s", err, stdout.String(), stderr.String())
    }

    if got := strings.TrimSpace(stdout.String()); got != "ok" {
        t.Fatalf("unexpected stdout: %q", got)
    }
    // Ensure both tool ids were seen by server in step 2
    if sawToolIDs == nil || !sawToolIDs["1"] || !sawToolIDs["2"] {
        t.Fatalf("server did not observe both tool messages: %+v", sawToolIDs)
    }
    // Transcript excerpts should be present in stderr
    tr := stderr.String()
    if !strings.Contains(tr, "--- chat.request step=1 ---") || !strings.Contains(tr, "\"temperature\": 1") {
        t.Fatalf("missing debug request dump with temperature; stderr=\n%s", tr)
    }
    if !strings.Contains(tr, "--- chat.response step=1 ---") || !strings.Contains(tr, "tool_calls") {
        t.Fatalf("missing debug response dump with tool_calls; stderr=\n%s", tr)
    }
    // Verify the file exists with expected content as a final sanity check
    data, err := os.ReadFile(filepath.Join(tmp, targetRel))
    if err != nil {
        t.Fatalf("read created file: %v", err)
    }
    if string(data) != string(content) {
        t.Fatalf("file content mismatch: got %q want %q", string(data), string(content))
    }
}
