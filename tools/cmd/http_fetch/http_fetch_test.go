package main_test

import (
    "bytes"
    "encoding/base64"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os/exec"
    "strings"
    "testing"

    testutil "github.com/hyperifyio/goagent/tools/testutil"
)

type fetchOutput struct {
    Status     int               `json:"status"`
    Headers    map[string]string `json:"headers"`
    BodyBase64 string            `json:"body_base64,omitempty"`
    Truncated  bool              `json:"truncated"`
}

func runFetch(t *testing.T, bin string, input any) (fetchOutput, string) {
    t.Helper()
    data, err := json.Marshal(input)
    if err != nil {
        t.Fatalf("marshal input: %v", err)
    }
    cmd := exec.Command(bin)
    cmd.Stdin = bytes.NewReader(data)
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr
    if err := cmd.Run(); err != nil {
        t.Fatalf("http_fetch failed to run: %v, stderr=%s", err, stderr.String())
    }
    out := strings.TrimSpace(stdout.String())
    var parsed fetchOutput
    if err := json.Unmarshal([]byte(out), &parsed); err != nil {
        t.Fatalf("failed to parse http_fetch output JSON: %v; raw=%q", err, out)
    }
    return parsed, stderr.String()
}

// TestHttpFetch_Get200_Basic verifies a simple GET returns status, headers, and base64 body without truncation.
func TestHttpFetch_Get200_Basic(t *testing.T) {
    // Arrange a test server that returns plain text
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodGet {
            http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
            return
        }
        w.Header().Set("Content-Type", "text/plain; charset=utf-8")
        w.Header().Set("ETag", "\"abc123\"")
        _, _ = w.Write([]byte("hello world"))
    }))
    defer srv.Close()

    bin := testutil.BuildTool(t, "http_fetch")

    out, _ := runFetch(t, bin, map[string]any{
        "url":        srv.URL,
        "max_bytes":  1 << 20, // 1 MiB cap
        "timeout_ms": 2000,
        "decompress": true,
    })

    if out.Status != 200 {
        t.Fatalf("expected status 200, got %d", out.Status)
    }
    if out.Truncated {
        t.Fatalf("expected truncated=false")
    }
    if ct := out.Headers["Content-Type"]; !strings.HasPrefix(ct, "text/plain") {
        t.Fatalf("unexpected content-type: %q", ct)
    }
    body, err := base64.StdEncoding.DecodeString(out.BodyBase64)
    if err != nil {
        t.Fatalf("body_base64 not valid base64: %v", err)
    }
    if string(body) != "hello world" {
        t.Fatalf("unexpected body: %q", string(body))
    }
}
