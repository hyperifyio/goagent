package main_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
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

func TestCrossrefSearch_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/works" {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("User-Agent"); !strings.Contains(got, "agentcli-crossref/0.1") {
			http.Error(w, "bad ua", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("mailto") != "dev@example.com" {
			http.Error(w, "missing mailto", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"items":[{"title":["A title"],"DOI":"10.1/x","issued":{"date-parts":[[2024,7,2]]},"container-title":["J Testing"],"short-title":["Short"]}]}}`)) //nolint:errcheck
	}))
	defer srv.Close()

	bin := testutil.BuildTool(t, "crossref_search")
	env := append(os.Environ(), "CROSSREF_BASE_URL="+srv.URL, "CROSSREF_ALLOW_LOCAL=1", "CROSSREF_MAILTO=dev@example.com")
	outStr, errStr, err := runTool(t, bin, env, map[string]any{"q": "golang", "rows": 5})
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	if !strings.Contains(outStr, "\"results\":[") {
		t.Fatalf("missing results: %s", outStr)
	}
	if !strings.Contains(outStr, "A title") || !strings.Contains(outStr, "10.1/x") || !strings.Contains(outStr, "2024-07-02") || !strings.Contains(outStr, "J Testing") || !strings.Contains(outStr, "Short") {
		t.Fatalf("missing mapped fields: %s", outStr)
	}
}

func TestCrossrefSearch_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Header().Set("Retry-After", "10")
		_, _ = w.Write([]byte("{}")) //nolint:errcheck
	}))
	defer srv.Close()

	bin := testutil.BuildTool(t, "crossref_search")
	env := append(os.Environ(), "CROSSREF_BASE_URL="+srv.URL, "CROSSREF_ALLOW_LOCAL=1", "CROSSREF_MAILTO=dev@example.com")
	outStr, errStr, err := runTool(t, bin, env, map[string]any{"q": "rate"})
	if err == nil {
		t.Fatalf("expected error, got ok: %s", outStr)
	}
	if !strings.Contains(errStr, "RATE_LIMITED") {
		t.Fatalf("expected RATE_LIMITED error, got: %s", errStr)
	}
}

func TestCrossrefSearch_RequiresMailto(t *testing.T) {
	bin := testutil.BuildTool(t, "crossref_search")
	env := append(os.Environ(), "CROSSREF_ALLOW_LOCAL=1")
	outStr, errStr, err := runTool(t, bin, env, map[string]any{"q": "x"})
	if err == nil {
		t.Fatalf("expected error, got ok: %s", outStr)
	}
	if !strings.Contains(errStr, "CROSSREF_MAILTO is required") {
		t.Fatalf("expected mailto required error, got: %s", errStr)
	}
}
