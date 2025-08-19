package main_test

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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

// TestMain enables local SSRF allowance for most tests that rely on httptest servers.
func TestMain(m *testing.M) {
	if err := os.Setenv("HTTP_FETCH_ALLOW_LOCAL", "1"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
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
		if _, err := w.Write([]byte("hello world")); err != nil {
			t.Fatalf("write: %v", err)
		}
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

// TestHttpFetch_HeadRequest ensures no body is returned and headers are present.
func TestHttpFetch_HeadRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
		w.WriteHeader(204)
	}))
	defer srv.Close()

	bin := testutil.BuildTool(t, "http_fetch")
	out, _ := runFetch(t, bin, map[string]any{
		"url":    srv.URL,
		"method": "HEAD",
	})
	if out.Status != 204 {
		t.Fatalf("expected 204, got %d", out.Status)
	}
	if out.BodyBase64 != "" {
		t.Fatalf("expected empty body for HEAD, got %q", out.BodyBase64)
	}
	if out.Headers["Last-Modified"] == "" {
		t.Fatalf("expected Last-Modified header present")
	}
}

// TestHttpFetch_Redirects_Limited ensures redirects are followed up to 5 and then fail.
func TestHttpFetch_Redirects_Limited(t *testing.T) {
	// Chain of 6 redirects
	mux := http.NewServeMux()
	for i := 0; i < 6; i++ {
		idx := i
		path := fmt.Sprintf("/r%c", 'a'+i)
		next := fmt.Sprintf("/r%c", 'a'+i+1)
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if idx == 5 {
				w.WriteHeader(200)
				if _, err := w.Write([]byte("ok")); err != nil {
					t.Fatalf("write: %v", err)
				}
				return
			}
			http.Redirect(w, r, next, http.StatusFound)
		})
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	bin := testutil.BuildTool(t, "http_fetch")
	// Expect error due to >5 redirects
	cmd := exec.Command(bin)
	in := map[string]any{"url": srv.URL + "/ra", "timeout_ms": 2000}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err == nil {
		t.Fatalf("expected error after too many redirects")
	}
	if !strings.Contains(stderr.String(), "too many redirects") {
		t.Fatalf("expected too many redirects error, got %q", stderr.String())
	}
}

// TestHttpFetch_GzipDecompress checks automatic gzip decoding by default.
func TestHttpFetch_GzipDecompress(t *testing.T) {
	gz := func(s string) []byte {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write([]byte(s)); err != nil {
			t.Fatalf("gzip write: %v", err)
		}
		if err := zw.Close(); err != nil {
			t.Fatalf("gzip close: %v", err)
		}
		return buf.Bytes()
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		if _, err := w.Write(gz("zipper")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}))
	defer srv.Close()

	bin := testutil.BuildTool(t, "http_fetch")

	// Default: decompress=true
	out, _ := runFetch(t, bin, map[string]any{"url": srv.URL})
	body, err := base64.StdEncoding.DecodeString(out.BodyBase64)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	if string(body) != "zipper" {
		t.Fatalf("expected decompressed body, got %q", string(body))
	}

	// With decompress=false, expect raw gzip bytes
	out, _ = runFetch(t, bin, map[string]any{"url": srv.URL, "decompress": false})
	body, err = base64.StdEncoding.DecodeString(out.BodyBase64)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	if string(body) == "zipper" {
		t.Fatalf("expected raw gzip bytes when decompress=false")
	}
}

// TestHttpFetch_Truncation enforces max_bytes cap.
func TestHttpFetch_Truncation(t *testing.T) {
	data := strings.Repeat("A", 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}))
	defer srv.Close()

	bin := testutil.BuildTool(t, "http_fetch")
	out, _ := runFetch(t, bin, map[string]any{"url": srv.URL, "max_bytes": 100})
	if !out.Truncated {
		t.Fatalf("expected truncated=true")
	}
	body, err := base64.StdEncoding.DecodeString(out.BodyBase64)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	if len(body) != 100 {
		t.Fatalf("expected 100 bytes, got %d", len(body))
	}
}

// TestHttpFetch_SSRF_Block_Localhost ensures SSRF guard blocks localhost by default.
func TestHttpFetch_SSRF_Block_Localhost(t *testing.T) {
	bin := testutil.BuildTool(t, "http_fetch")
	cmd := exec.Command(bin)
	in := map[string]any{"url": "http://127.0.0.1:9"}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Ensure guard is active
	// Inherit env but explicitly remove HTTP_FETCH_ALLOW_LOCAL to enforce guard
	var env []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "HTTP_FETCH_ALLOW_LOCAL=") {
			continue
		}
		env = append(env, e)
	}
	cmd.Env = env
	err = cmd.Run()
	if err == nil {
		t.Fatalf("expected SSRF block error")
	}
	if !strings.Contains(stderr.String(), "SSRF blocked") {
		t.Fatalf("expected SSRF blocked error, got %q", stderr.String())
	}
}
