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

// Allow local SSRF for tests targeting httptest.Server
func TestMain(m *testing.M) {
	if err := os.Setenv("CITATION_PACK_ALLOW_LOCAL", "1"); err != nil {
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

func TestCitationPack_NoArchive(t *testing.T) {
	bin := testutil.BuildTool(t, "citation_pack")
	env := os.Environ()
	in := map[string]any{
		"doc": map[string]any{
			"title": "Example",
			"url":   "http://example.com/article",
		},
	}
	outStr, errStr, err := runTool(t, bin, env, in)
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	if !strings.Contains(outStr, "\"url\":\"http://example.com/article\"") {
		t.Fatalf("unexpected url: %s", outStr)
	}
	if !strings.Contains(outStr, "\"host\":\"example.com\"") {
		t.Fatalf("missing host: %s", outStr)
	}
	if strings.Contains(outStr, "archive_url") {
		t.Fatalf("unexpected archive_url: %s", outStr)
	}
}

func TestCitationPack_WaybackLookup_Success(t *testing.T) {
	// Mock Wayback API
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/available" {
			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write([]byte(`{"archived_snapshots":{"closest":{"available":true,"url":"http://web.archive.org/web/20200101000000/http://example.com/article","timestamp":"20200101000000"}}}`)); err != nil {
				t.Fatalf("write: %v", err)
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	bin := testutil.BuildTool(t, "citation_pack")
	env := append(os.Environ(), "WAYBACK_BASE_URL="+srv.URL, "CITATION_PACK_ALLOW_LOCAL=1")
	in := map[string]any{
		"doc": map[string]any{
			"url": "http://example.com/article",
		},
		"archive": map[string]any{
			"wayback": true,
		},
	}
	outStr, errStr, err := runTool(t, bin, env, in)
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	if !strings.Contains(outStr, "\"archive_url\":\"http://web.archive.org/web/20200101000000/http://example.com/article\"") {
		t.Fatalf("expected archive_url, got: %s", outStr)
	}
}

func TestCitationPack_SSRFBlocked_BaseURL(t *testing.T) {
	bin := testutil.BuildTool(t, "citation_pack")
	env := []string{"WAYBACK_BASE_URL=http://127.0.0.1:9"}
	in := map[string]any{
		"doc": map[string]any{
			"url": "http://example.com",
		},
		"archive": map[string]any{
			"wayback": true,
		},
	}
	_, errStr, err := runTool(t, bin, env, in)
	if err == nil {
		t.Fatalf("expected error, got ok")
	}
	if !strings.Contains(errStr, "SSRF blocked") {
		t.Fatalf("expected SSRF blocked, got: %s", errStr)
	}
}
