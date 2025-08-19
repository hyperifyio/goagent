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
	if err := os.Setenv("WAYBACK_ALLOW_LOCAL", "1"); err != nil {
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

func TestWaybackLookup_SuccessLookup(t *testing.T) {
	// Mock Wayback API
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/available":
			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write([]byte(`{"archived_snapshots":{"closest":{"available":true,"url":"http://web.archive.org/web/20200101000000/http://example.com/","timestamp":"20200101000000"}}}`)); err != nil {
				t.Fatalf("write: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	bin := testutil.BuildTool(t, "wayback_lookup")
	env := append(os.Environ(), "WAYBACK_BASE_URL="+srv.URL, "WAYBACK_ALLOW_LOCAL=1")
	outStr, errStr, err := runTool(t, bin, env, map[string]any{"url": "http://example.com"})
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	if !strings.Contains(outStr, "\"closest_url\":") || !strings.Contains(outStr, "web.archive.org/web/20200101000000") {
		t.Fatalf("unexpected output: %s", outStr)
	}
	if !strings.Contains(outStr, "\"timestamp\":\"20200101000000\"") {
		t.Fatalf("missing timestamp: %s", outStr)
	}
}

func TestWaybackLookup_SaveTrue(t *testing.T) {
	var saveCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/available":
			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write([]byte(`{"archived_snapshots":{"closest":{"available":false}}}`)); err != nil {
				t.Fatalf("write: %v", err)
			}
		case r.URL.Path == "/save/":
			saveCalled = true
			w.WriteHeader(200)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	bin := testutil.BuildTool(t, "wayback_lookup")
	env := append(os.Environ(), "WAYBACK_BASE_URL="+srv.URL, "WAYBACK_ALLOW_LOCAL=1")
	outStr, errStr, err := runTool(t, bin, env, map[string]any{"url": "http://example.com", "save": true})
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	if !saveCalled {
		t.Fatalf("expected /save/ to be called")
	}
	if !strings.Contains(outStr, "\"saved\":true") {
		t.Fatalf("expected saved=true in output: %s", outStr)
	}
}

func TestWaybackLookup_SSRFBlocked(t *testing.T) {
	bin := testutil.BuildTool(t, "wayback_lookup")
	// Force no local bypass and set private base URL
	env := []string{"WAYBACK_BASE_URL=http://127.0.0.1:9"}
	_, errStr, err := runTool(t, bin, env, map[string]any{"url": "http://example.com"})
	if err == nil {
		t.Fatalf("expected error, got ok")
	}
	if !strings.Contains(errStr, "SSRF blocked") {
		t.Fatalf("expected SSRF blocked error, got: %s", errStr)
	}
}

func TestWaybackLookup_BadBaseURL(t *testing.T) {
	bin := testutil.BuildTool(t, "wayback_lookup")
	env := []string{"WAYBACK_BASE_URL=:bad"}
	_, errStr, err := runTool(t, bin, env, map[string]any{"url": "http://example.com"})
	if err == nil {
		t.Fatalf("expected error for bad base url")
	}
	if !strings.Contains(errStr, "WAYBACK_BASE_URL") {
		t.Fatalf("expected base url error, got: %s", errStr)
	}
}
