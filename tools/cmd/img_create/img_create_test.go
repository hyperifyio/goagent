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
    "strings"
    "testing"
    
    "github.com/hyperifyio/goagent/tools/testutil"
)

func buildTool(t *testing.T) string {
    // Build this package into a temp binary
    bin := filepath.Join(t.TempDir(), "img_create-test-bin")
    cmd := exec.Command("go", "build", "-o", bin, ".")
    cmd.Dir = "."
    if out, err := cmd.CombinedOutput(); err != nil {
        t.Fatalf("build: %v\n%s", err, string(out))
    }
    return bin
}

func runTool(t *testing.T, bin string, in any, env map[string]string) (stdout, stderr string, code int) {
    data, _ := json.Marshal(in)
    cmd := exec.Command(bin)
    cmd.Stdin = bytes.NewReader(data)
    var outBuf, errBuf bytes.Buffer
    cmd.Stdout = &outBuf
    cmd.Stderr = &errBuf
    if env != nil {
        e := os.Environ()
        for k, v := range env {
            e = append(e, k+"="+v)
        }
        cmd.Env = e
    }
    err := cmd.Run()
    if err != nil {
        if ee, ok := err.(*exec.ExitError); ok {
            code = ee.ExitCode()
        } else {
            code = 1
        }
    }
    return outBuf.String(), errBuf.String(), code
}

func TestMissingPrompt(t *testing.T) {
    bin := buildTool(t)
    _, stderr, code := runTool(t, bin, map[string]any{}, nil)
    if code == 0 {
        t.Fatalf("expected non-zero exit")
    }
    if !strings.Contains(stderr, "prompt is required") {
        t.Fatalf("expected prompt error, got %q", stderr)
    }
}

func TestHappyPath_SaveOnePNG(t *testing.T) {
    // 1x1 transparent PNG
    png1x1 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO9cFmgAAAAASUVORK5CYII="
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost || r.URL.Path != "/v1/images/generations" {
            t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
        }
        var req struct{
            Model string `json:"model"`
            Prompt string `json:"prompt"`
            N int `json:"n"`
            Size string `json:"size"`
            RespFmt string `json:"response_format"`
        }
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            t.Fatalf("bad json: %v", err)
        }
        if req.Model != "gpt-image-1" || req.Prompt != "tiny-pixel" || req.N != 1 || req.Size != "1024x1024" || req.RespFmt != "b64_json" {
            t.Fatalf("unexpected payload: %+v", req)
        }
        _ = json.NewEncoder(w).Encode(map[string]any{
            "data": []map[string]any{{"b64_json": png1x1}},
            "model": "gpt-image-1",
        })
    }))
    defer srv.Close()

    bin := buildTool(t)
    outDir := testutil.MakeRepoRelTempDir(t, "imgcreate-out-")
    stdout, stderr, code := runTool(t, bin, map[string]any{
        "prompt": "tiny-pixel",
        "save": map[string]any{"dir": outDir, "basename": "img", "ext": "png"},
    }, map[string]string{
        "OAI_IMAGE_BASE_URL": srv.URL,
        "OAI_API_KEY":        "test-123",
    })
    if code != 0 {
        t.Fatalf("unexpected failure: %s", stderr)
    }
    var obj struct{
        Saved []struct{ Path string `json:"path"`; Bytes int `json:"bytes"`; Sha256 string `json:"sha256"` } `json:"saved"`
        N int `json:"n"`
        Size string `json:"size"`
        Model string `json:"model"`
    }
    if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &obj); err != nil {
        t.Fatalf("bad stdout json: %v; raw=%q", err, stdout)
    }
    if obj.N != 1 || len(obj.Saved) != 1 {
        t.Fatalf("unexpected saved count: %+v", obj)
    }
    // Verify file exists and bytes match decoded b64
    got, err := os.ReadFile(obj.Saved[0].Path)
    if err != nil {
        t.Fatalf("read saved file: %v", err)
    }
    want, _ := base64.StdEncoding.DecodeString(png1x1)
    if len(got) != len(want) {
        t.Fatalf("bytes mismatch: got %d want %d", len(got), len(want))
    }
}
