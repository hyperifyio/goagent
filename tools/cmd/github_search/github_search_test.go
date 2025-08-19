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

func runTool(t *testing.T, bin string, env []string, input any) (string, string, error) {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), runErr
}

func TestGithubSearch_RepositoriesSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/repositories" {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("q") == "" {
			http.Error(w, "missing q", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.Header().Set("X-RateLimit-Reset", "12345")
		if _, err := w.Write([]byte(`{"items":[{"full_name":"foo/bar","html_url":"https://github.com/foo/bar","description":"desc","stargazers_count":42}]}`)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}))
	defer srv.Close()

	bin := testutil.BuildTool(t, "github_search")
	env := append(os.Environ(), "GITHUB_BASE_URL="+srv.URL, "GITHUB_ALLOW_LOCAL=1")
	outStr, errStr, err := runTool(t, bin, env, map[string]any{"q": "golang", "type": "repositories"})
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	if !strings.Contains(outStr, "\"full_name\":\"foo/bar\"") {
		t.Fatalf("missing repo in output: %s", outStr)
	}
	if !strings.Contains(outStr, "\"rate\":{") || !strings.Contains(outStr, "\"remaining\":4999") {
		t.Fatalf("missing rate info: %s", outStr)
	}
}

func TestGithubSearch_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "12345")
		if _, err := w.Write([]byte(`{"items":[]}`)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}))
	defer srv.Close()

	bin := testutil.BuildTool(t, "github_search")
	env := append(os.Environ(), "GITHUB_BASE_URL="+srv.URL, "GITHUB_ALLOW_LOCAL=1")
	outStr, errStr, err := runTool(t, bin, env, map[string]any{"q": "x", "type": "code"})
	if err == nil {
		t.Fatalf("expected error due to rate limiting, got ok: %s", outStr)
	}
	if !strings.Contains(errStr, "RATE_LIMITED") || !strings.Contains(errStr, "use GITHUB_TOKEN") {
		t.Fatalf("expected RATE_LIMITED with hint, got: %s", errStr)
	}
}

func TestGithubSearch_Retry5xxThenSuccess(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := atomic.AddInt32(&calls, 1)
		if c == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Remaining", "10")
		if _, err := w.Write([]byte(`{"items":[{"full_name":"a/b","html_url":"https://github.com/a/b"}]}`)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}))
	defer srv.Close()

	bin := testutil.BuildTool(t, "github_search")
	env := append(os.Environ(), "GITHUB_BASE_URL="+srv.URL, "GITHUB_ALLOW_LOCAL=1")
	outStr, errStr, err := runTool(t, bin, env, map[string]any{"q": "q", "type": "repositories"})
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Fatalf("expected at least 2 calls, got %d", calls)
	}
	if !strings.Contains(outStr, "\"full_name\":\"a/b\"") {
		t.Fatalf("missing repo: %s", outStr)
	}
}
