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

// no exported test-only types

func TestMain(m *testing.M) {
	if err := os.Setenv("OPENALEX_ALLOW_LOCAL", "1"); err != nil {
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
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), runErr
}

func TestOpenAlexSearch_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/works" {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"display_name":"A title","doi":"10.1/x","publication_year":2024,"authorships":[{"a":1}],"cited_by_count":3,"open_access":{"oa_url":"https://oa"}}]}`)) //nolint:errcheck
	}))
	defer srv.Close()

	bin := testutil.BuildTool(t, "openalex_search")
	env := append(os.Environ(), "OPENALEX_BASE_URL="+srv.URL, "OPENALEX_ALLOW_LOCAL=1")
	outStr, errStr, err := runTool(t, bin, env, map[string]any{"q": "golang", "per_page": 5})
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	if !strings.Contains(outStr, "\"results\":[") {
		t.Fatalf("missing results: %s", outStr)
	}
	if !strings.Contains(outStr, "A title") {
		t.Fatalf("missing mapped title: %s", outStr)
	}
}

func TestOpenAlexSearch_SSRFBlocked(t *testing.T) {
	bin := testutil.BuildTool(t, "openalex_search")
	env := []string{"OPENALEX_BASE_URL=http://127.0.0.1:9"}
	outStr, errStr, err := runTool(t, bin, env, map[string]any{"q": "x"})
	if err == nil {
		t.Fatalf("expected error, got ok: %s", outStr)
	}
	if !strings.Contains(errStr, "SSRF blocked") {
		t.Fatalf("expected SSRF blocked error, got: %s", errStr)
	}
}
