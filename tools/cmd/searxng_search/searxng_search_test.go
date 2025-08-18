package main_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"

	testutil "github.com/hyperifyio/goagent/tools/testutil"
)

// no exported types required here

// TestMain allows local SSRF to reach httptest.Server
func TestMain(m *testing.M) {
	if err := os.Setenv("SEARXNG_ALLOW_LOCAL", "1"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func runTool(t *testing.T, bin string, env []string, input any) (string, string, error) {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(data)
	// Use provided env exactly; caller can pass os.Environ()-derived slice when needed.
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), runErr
}

func TestSearxngSearch_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" || r.URL.Query().Get("format") != "json" {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"query":"golang","results":[{"title":"Go","url":"https://golang.org","content":"The Go Programming Language","engine":"duckduckgo"}]}`)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}))
	defer srv.Close()

	bin := testutil.BuildTool(t, "searxng_search")
	env := append(os.Environ(), "SEARXNG_BASE_URL="+srv.URL, "SEARXNG_ALLOW_LOCAL=1")
	outStr, errStr, err := runTool(t, bin, env, map[string]any{"q": "golang"})
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	if !strings.Contains(outStr, "\"query\":\"golang\"") {
		t.Fatalf("missing query in output: %s", outStr)
	}
	if !strings.Contains(outStr, "golang.org") {
		t.Fatalf("missing result url: %s", outStr)
	}
}

func TestSearxngSearch_Retry429(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := atomic.AddInt32(&calls, 1)
		if c == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"query":"q","results":[{"title":"A","url":"https://a","content":"s","engine":"e"}]}`)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}))
	defer srv.Close()

	bin := testutil.BuildTool(t, "searxng_search")
	env := append(os.Environ(), "SEARXNG_BASE_URL="+srv.URL, "SEARXNG_ALLOW_LOCAL=1")
	outStr, errStr, err := runTool(t, bin, env, map[string]any{"q": "q"})
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Fatalf("expected at least 2 calls, got %d", calls)
	}
	if !strings.Contains(outStr, "\"results\":[") {
		t.Fatalf("missing results: %s", outStr)
	}
}

func TestSearxngSearch_Retry5xxThenSuccess(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := atomic.AddInt32(&calls, 1)
		if c == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"query":"q","results":[{"title":"B","url":"https://b","content":"s","engine":"e"}]}`)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}))
	defer srv.Close()

	bin := testutil.BuildTool(t, "searxng_search")
	env := append(os.Environ(), "SEARXNG_BASE_URL="+srv.URL, "SEARXNG_ALLOW_LOCAL=1")
	outStr, errStr, err := runTool(t, bin, env, map[string]any{"q": "q"})
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Fatalf("expected at least 2 calls, got %d", calls)
	}
	if !strings.Contains(outStr, "\"results\":[") {
		t.Fatalf("missing results: %s", outStr)
	}
}

func TestSearxngSearch_SSRFBlocked(t *testing.T) {
	bin := testutil.BuildTool(t, "searxng_search")
	// Force no local bypass and set private base URL
	env := []string{"SEARXNG_BASE_URL=http://127.0.0.1:9"}
	outStr, errStr, err := runTool(t, bin, env, map[string]any{"q": "x"})
	if err == nil {
		t.Fatalf("expected error, got ok: %s", outStr)
	}
	if !strings.Contains(errStr, "SSRF blocked") {
		t.Fatalf("expected SSRF blocked error, got: %s", errStr)
	}
}

func TestSearxngSearch_BadBaseURL(t *testing.T) {
	bin := testutil.BuildTool(t, "searxng_search")
	env := []string{"SEARXNG_BASE_URL=:bad"}
	_, errStr, err := runTool(t, bin, env, map[string]any{"q": "x"})
	if err == nil {
		t.Fatalf("expected error for bad base url")
	}
	if !strings.Contains(errStr, "SEARXNG_BASE_URL") {
		t.Fatalf("expected base url error, got: %s", errStr)
	}
}
