package main

import (
    "bytes"
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

// https://github.com/hyperifyio/goagent/issues/1
func TestRunAgent_ToolConversationLoop(t *testing.T) {
    // Fake tool: echo stdin to stdout
    dir := t.TempDir()
    helper := filepath.Join(dir, "echo.go")
    if err := os.WriteFile(helper, []byte(`package main
import ("io"; "os"; "fmt")
func main(){b,_:=io.ReadAll(os.Stdin); fmt.Print(string(b))}
`), 0o644); err != nil {
        t.Fatalf("write tool: %v", err)
    }
    bin := filepath.Join(dir, "echo")
    if runtime.GOOS == "windows" { bin += ".exe" }
    if out, err := exec.Command("go", "build", "-o", bin, helper).CombinedOutput(); err != nil {
        t.Fatalf("build tool: %v: %s", err, string(out))
    }

    // Create tools.json referencing the echo tool
    toolsPath := filepath.Join(dir, "tools.json")
    manifest := map[string]any{
        "tools": []map[string]any{{
            "name": "echo",
            "description": "echo back input",
            "schema": map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []string{"text"}},
            "command": []string{bin},
            "timeoutSec": 5,
        }},
    }
    b, _ := json.Marshal(manifest)
    if err := os.WriteFile(toolsPath, b, 0o644); err != nil {
        t.Fatalf("write manifest: %v", err)
    }

    // Fake server with two-step responses
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
            // Respond with a tool call to echo
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
                        ToolCalls: []oai.ToolCall{{
                            ID:   "1",
                            Type: "function",
                            Function: oai.ToolCallFunction{
                                Name:      "echo",
                                Arguments: `{"text":"hi"}`,
                            },
                        }},
                    },
                }},
            }
            _ = json.NewEncoder(w).Encode(resp)
        case 2:
            resp := oai.ChatCompletionsResponse{
                ID:      "cmpl-2",
                Object:  "chat.completion",
                Created: time.Now().Unix(),
                Model:   req.Model,
                Choices: []oai.ChatCompletionsResponseChoice{{
                    Index:        0,
                    FinishReason: "stop",
                    Message: oai.Message{Role: oai.RoleAssistant, Content: "done"},
                }},
            }
            _ = json.NewEncoder(w).Encode(resp)
        default:
            t.Fatalf("unexpected extra request step=%d", step)
        }
    }))
    defer srv.Close()

    cfg := cliConfig{
        prompt:       "test",
        toolsPath:    toolsPath,
        systemPrompt: "sys",
        baseURL:      srv.URL,
        apiKey:       "",
        model:        "test",
        maxSteps:     4,
        timeout:      5 * time.Second,
        temperature:  0,
        debug:        false,
    }

    var outBuf, errBuf bytes.Buffer
    code := runAgent(cfg, &outBuf, &errBuf)
    if code != 0 {
        t.Fatalf("expected exit code 0, got %d; stderr=%s", code, errBuf.String())
    }
    if got := outBuf.String(); got != "done\n" {
        t.Fatalf("unexpected stdout: %q", got)
    }
}
