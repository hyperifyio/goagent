//nolint:errcheck // Integration tests may ignore some error returns in setup/teardown and JSON encoders.
package main

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
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hyperifyio/goagent/internal/oai"
	testutil "github.com/hyperifyio/goagent/tools/testutil"
)

// copyFile copies a file from src to dst with 0755 mode and checks errors.
func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open %s: %v", src, err)
	}
	defer func() {
		if cerr := in.Close(); cerr != nil {
			t.Fatalf("close in: %v", cerr)
		}
	}()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	defer func() {
		if cerr := out.Close(); cerr != nil {
			t.Fatalf("close out: %v", cerr)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy %s -> %s: %v", src, dst, err)
	}
}

// newTwoStepServer returns a server that first requests tool calls then returns final content.
func newTwoStepServer(t *testing.T, targetRelPath, contentB64, model string) *httptest.Server {
	t.Helper()
	var step int
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Fatalf("encode resp step1: %v", err)
			}
		case 2:
			// Final message
			resp := oai.ChatCompletionsResponse{
				ID:      "cmpl-2",
				Object:  "chat.completion",
				Created: time.Now().Unix(),
				Model:   model,
				Choices: []oai.ChatCompletionsResponseChoice{{
					Index:        0,
					FinishReason: "stop",
					Message:      oai.Message{Role: oai.RoleAssistant, Content: "ok"},
				}},
			}
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				t.Fatalf("encode resp step2: %v", err)
			}
		default:
			t.Fatalf("unexpected extra request step=%d", step)
		}
	}))
}

// Ensures pre-stage honors a nested -prep-tools manifest path and executes the referenced tool.
// The server verifies that the second request (main stage) includes the tool output produced
// during pre-stage, proving nested manifest resolution and execution.
func TestPrep_Integration_NestedManifestResolution(t *testing.T) {
	tmp := t.TempDir()
	// Create nested manifest directory with canonical tools/bin layout
	nested := filepath.Join(tmp, "sub", "manifest")
	binDir := filepath.Join(nested, "tools", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir tools/bin: %v", err)
	}

	// Build a tiny tool that echoes a known JSON to stdout
	src := filepath.Join(tmp, "prep_ok.go")
	if err := os.WriteFile(src, []byte(`package main
import ("encoding/json"; "io"; "os")
func main(){_,_ = io.ReadAll(os.Stdin); _ = json.NewEncoder(os.Stdout).Encode(map[string]any{"from":"prep","ok":true})}
`), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	toolPath := filepath.Join(binDir, "prep_ok")
	if runtime.GOOS == "windows" {
		toolPath += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", toolPath, src).CombinedOutput(); err != nil {
		t.Fatalf("build tool: %v: %s", err, string(out))
	}

	// Write a manifest that references the tool with a relative ./tools/bin path
	manPath := filepath.Join(nested, "tools.json")
	manifest := map[string]any{
		"tools": []map[string]any{{
			"name":        "prep_ok",
			"description": "emit ok json",
			"schema":      map[string]any{"type": "object", "additionalProperties": false},
			"command":     []string{"./tools/bin/prep_ok"},
			"timeoutSec":  5,
		}},
	}
	b, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(manPath, b, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	// Fake server: first response triggers pre-stage tool call; second validates tool output present
	step := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req oai.ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		step++
		switch step {
		case 1:
			// Pre-stage: request tool_calls to our external tool
			_ = json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{
				FinishReason: "tool_calls",
				Message:      oai.Message{Role: oai.RoleAssistant, ToolCalls: []oai.ToolCall{{ID: "t1", Type: "function", Function: oai.ToolCallFunction{Name: "prep_ok", Arguments: "{}"}}}},
			}}})
		case 2:
			// Main stage: verify tool output is present in messages
			var saw bool
			for _, m := range req.Messages {
				if m.Role == oai.RoleTool && m.Name == "prep_ok" && bytes.Contains([]byte(m.Content), []byte("\"ok\":true")) {
					saw = true
					break
				}
			}
			if !saw {
				t.Fatalf("expected prep_ok tool output in main request messages")
			}
			_ = json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{
				Message: oai.Message{Role: oai.RoleAssistant, Content: "done"},
			}}})
		default:
			t.Fatalf("unexpected extra request step=%d", step)
		}
	}))
	defer srv.Close()

	// Run the agent end-to-end with pre-stage external tools enabled and nested -prep-tools manifest
	var outBuf, errBuf bytes.Buffer
	code := cliMain([]string{
		"-prompt", "x",
		"-base-url", srv.URL,
		"-model", "m",
		"-prep-tools-allow-external",
		"-prep-tools", manPath,
	}, &outBuf, &errBuf)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
	}
	if got := outBuf.String(); got != "done\n" {
		t.Fatalf("unexpected stdout: %q", got)
	}
}

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
	copyFile(t, srcWrite, fsWriteBin)
	copyFile(t, srcRead, fsReadBin)

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
	b, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(toolsPath, b, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	// Change working directory to the temp dir so relative ./tools/bin/* resolve
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWD); err != nil {
			t.Errorf("cleanup chdir back: %v", err)
		}
	})

	// Prepare the file path and content for the tool calls (relative to current working directory)
	targetRelPath := "tmp_tools_it_demo.txt"
	// Ensure cleanup
	t.Cleanup(func() {
		if err := os.Remove(targetRelPath); err != nil && !os.IsNotExist(err) {
			t.Errorf("cleanup remove: %v", err)
		}
	})
	content := []byte("hello world")
	contentB64 := base64.StdEncoding.EncodeToString(content)

	// Fake server: first response asserts tools advertised and returns two tool calls;
	// second response returns the final assistant message
	srv := newTwoStepServer(t, targetRelPath, contentB64, "test")
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
		httpTimeout:  10 * time.Second,
		toolTimeout:  10 * time.Second,
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

// Deterministic end-to-end acceptance: pre-stage returns two read-only tool calls
// and a non-final assistant channel; the agent executes built-in pre-stage tools,
// routes the non-final channel to stderr under -verbose, and the main call completes.
// nolint:gocyclo // End-to-end integration test favors readability over low cyclomatic complexity.
func TestAcceptance_EndToEnd_PrepReadonlyTools_ChannelRouting_AndMainCompletion(t *testing.T) {
	// Work in an isolated temp directory and create a small file for fs.read_file
	tmp := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	if err := os.WriteFile("prestage.txt", []byte("hi"), 0o644); err != nil {
		t.Fatalf("write prestage.txt: %v", err)
	}

	// Two-step mock server: pre-stage -> tool_calls; main -> verify tool outputs present
	step := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var req oai.ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		step++
		switch step {
		case 1:
			// Pre-stage: return two built-in read-only tool calls with a non-final channel
			resp := oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{
				Message: oai.Message{
					Role:    oai.RoleAssistant,
					Channel: "critic",
					Content: "pre-critic",
					ToolCalls: []oai.ToolCall{
						{ID: "t1", Type: "function", Function: oai.ToolCallFunction{Name: "fs.read_file", Arguments: `{"path":"prestage.txt"}`}},
						{ID: "t2", Type: "function", Function: oai.ToolCallFunction{Name: "os.info", Arguments: `{}`}},
					},
				},
			}}}
			_ = json.NewEncoder(w).Encode(resp)
		case 2:
			// Main call: assert pre-stage tool outputs were appended to messages
			var haveRead, haveOS bool
			for _, m := range req.Messages {
				if m.Role == oai.RoleTool && m.Name == "fs.read_file" && strings.Contains(m.Content, `"content":"hi"`) {
					haveRead = true
				}
				if m.Role == oai.RoleTool && m.Name == "os.info" && strings.Contains(m.Content, "goos") {
					haveOS = true
				}
			}
			if !haveRead || !haveOS {
				t.Fatalf("expected pre-stage tool outputs present (fs.read_file=%v os.info=%v)", haveRead, haveOS)
			}
			_ = json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{
				Message: oai.Message{Role: oai.RoleAssistant, Channel: "final", Content: "OK"},
			}}})
		default:
			t.Fatalf("unexpected extra request step=%d", step)
		}
	}))
	defer srv.Close()

	var outBuf, errBuf bytes.Buffer
	code := cliMain([]string{
		"-prompt", "x",
		"-base-url", srv.URL,
		"-model", "m",
		"-max-steps", "1",
		"-verbose",
	}, &outBuf, &errBuf)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
	}
	if got := outBuf.String(); got != "OK\n" {
		t.Fatalf("unexpected stdout: %q", got)
	}
	if !strings.Contains(errBuf.String(), "pre-critic") {
		t.Fatalf("stderr did not contain pre-stage non-final channel content; got=%q", errBuf.String())
	}
}

// End-to-end agent integration for img_create tool.
// Spins an Images API mock expecting POST /v1/images/generations with the canonical body,
// builds/copies the img_create tool under ./tools/bin, and runs the agent against a model
// mock that first requests a tool call to img_create (saving under out/) and then returns
// a final assistant message summarizing the saved path. Asserts one PNG is written and
// stdout equals the final assistant content.
// nolint:gocyclo // End-to-end integration test favors readability over low cyclomatic complexity.
func TestAgent_EndToEnd_ImageCreate_SavesPNG_AndPrintsFinal(t *testing.T) {
	// Use an isolated temp directory so relative save paths land here
	// Do not chdir yet; building the tool needs repo root as CWD
	tmp := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	// 1x1 transparent PNG base64 (same as tool unit test)
	png1x1 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO9cFmgAAAAASUVORK5CYII="

	// Images API mock: verify request body and return one b64 image
	var imagesHits int
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/images/generations" {
			t.Fatalf("unexpected images request: %s %s", r.Method, r.URL.Path)
		}
		imagesHits++
		var req struct {
			Model          string `json:"model"`
			Prompt         string `json:"prompt"`
			N              int    `json:"n"`
			Size           string `json:"size"`
			ResponseFormat string `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("images decode: %v", err)
		}
		if req.Model != "gpt-image-1" || req.Prompt != "tiny-pixel" || req.N != 1 || req.Size != "1024x1024" || req.ResponseFormat != "b64_json" {
			t.Fatalf("unexpected images payload: %+v", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":  []map[string]any{{"b64_json": png1x1}},
			"model": "gpt-image-1",
		})
	}))
	defer imgSrv.Close()

	// Build img_create tool and place it under ./tools/bin in temp repo
	binDir := filepath.Join(tmp, "tools", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir tools/bin: %v", err)
	}
	srcImg := testutil.BuildTool(t, "img_create")
	dstImg := filepath.Join(binDir, "img_create")
	if runtime.GOOS == "windows" {
		dstImg += ".exe"
	}
	copyFile(t, srcImg, dstImg)

	// tools.json manifest with envPassthrough for Images API
	toolsPath := filepath.Join(tmp, "tools.json")
	manifest := map[string]any{
		"tools": []map[string]any{{
			"name":        "img_create",
			"description": "Generate image(s) with OpenAI Images API and save to repo or return base64",
			"schema": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"prompt"},
				"properties": map[string]any{
					"prompt":     map[string]any{"type": "string"},
					"n":          map[string]any{"type": "integer", "minimum": 1, "maximum": 4, "default": 1},
					"size":       map[string]any{"type": "string", "pattern": "^\\d{3,4}x\\d{3,4}$", "default": "1024x1024"},
					"model":      map[string]any{"type": "string", "default": "gpt-image-1"},
					"return_b64": map[string]any{"type": "boolean", "default": false},
					"save": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"required":             []string{"dir"},
						"properties": map[string]any{
							"dir":      map[string]any{"type": "string"},
							"basename": map[string]any{"type": "string", "default": "img"},
							"ext":      map[string]any{"type": "string", "enum": []any{"png"}, "default": "png"},
						},
					},
				},
			},
			"command":        []string{"./tools/bin/img_create"},
			"timeoutSec":     120,
			"envPassthrough": []string{"OAI_API_KEY", "OAI_BASE_URL", "OAI_IMAGE_BASE_URL", "OAI_HTTP_TIMEOUT"},
		}},
	}
	if b, err := json.Marshal(manifest); err != nil {
		t.Fatalf("marshal manifest: %v", err)
	} else if err := os.WriteFile(toolsPath, b, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	// Model server: step1 -> tool_calls: img_create saving under out/; step2 -> final message
	step := 0
	expectedSaved := filepath.ToSlash(filepath.Join("out", "img_001.png"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var req oai.ChatCompletionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		step++
		switch step {
		case 1:
			// Request a single tool call to img_create with deterministic args
			resp := oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{
				FinishReason: "tool_calls",
				Message: oai.Message{Role: oai.RoleAssistant, ToolCalls: []oai.ToolCall{{
					ID:   "ic1",
					Type: "function",
					Function: oai.ToolCallFunction{
						Name:      "img_create",
						Arguments: `{"prompt":"tiny-pixel","n":1,"size":"1024x1024","save":{"dir":"out"}}`,
					},
				}}},
			}}}
			_ = json.NewEncoder(w).Encode(resp)
		case 2:
			// Final assistant message summarizes the saved path
			_ = json.NewEncoder(w).Encode(oai.ChatCompletionsResponse{Choices: []oai.ChatCompletionsResponseChoice{{
				Message: oai.Message{Role: oai.RoleAssistant, Content: "saved " + expectedSaved},
			}}})
		default:
			t.Fatalf("unexpected extra request step=%d", step)
		}
	}))
	defer srv.Close()

	// Ensure the tool sees the Images API base and a dummy API key
	t.Setenv("OAI_IMAGE_BASE_URL", imgSrv.URL)
	t.Setenv("OAI_API_KEY", "test-123")

	// Now chdir so relative outputs (e.g., out/) are created under tmp
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	var outBuf, errBuf bytes.Buffer
	code := cliMain([]string{
		"-prompt", "use img_create to save under out/",
		"-tools", toolsPath,
		"-prep-enabled", "false",
		"-base-url", srv.URL,
		"-model", "m",
		"-max-steps", "4",
		"-http-timeout", "5s",
		"-tool-timeout", "5s",
		"-debug",
	}, &outBuf, &errBuf)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errBuf.String())
	}
	if got := strings.TrimSpace(outBuf.String()); got != "saved "+expectedSaved {
		t.Fatalf("unexpected stdout: %q", got)
	}
	// Assert one PNG exists at the expected path
	if fi, err := os.Stat(expectedSaved); err != nil || fi.IsDir() {
		t.Fatalf("expected saved PNG at %s (imagesHits=%d, stderr=%q)", expectedSaved, imagesHits, errBuf.String())
	}
}
