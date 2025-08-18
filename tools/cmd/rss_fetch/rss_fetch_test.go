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

// Allow local SSRF for tests
func TestMain(m *testing.M) {
	if err := os.Setenv("RSS_FETCH_ALLOW_LOCAL", "1"); err != nil {
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

func TestRSSFetch_RSS(t *testing.T) {
	rss := `<?xml version="1.0"?><rss version="2.0"><channel><title>t</title><link>https://ex/</link><item><title>A</title><link>https://ex/a</link><pubDate>Mon, 02 Jan 2006 15:04:05 MST</pubDate><description>da</description></item></channel></rss>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		if _, err := w.Write([]byte(rss)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}))
	defer srv.Close()

	bin := testutil.BuildTool(t, "rss_fetch")
	env := append(os.Environ(), "RSS_FETCH_ALLOW_LOCAL=1")
	outStr, errStr, err := runTool(t, bin, env, map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	if !strings.Contains(outStr, "\"feed\":") || !strings.Contains(outStr, "\"items\":") {
		t.Fatalf("unexpected output: %s", outStr)
	}
	if !strings.Contains(outStr, "https://ex/a") {
		t.Fatalf("missing item url: %s", outStr)
	}
}

func TestRSSFetch_Atom(t *testing.T) {
	atom := `<?xml version="1.0" encoding="utf-8"?><feed xmlns="http://www.w3.org/2005/Atom"><title>Example</title><link href="https://ex/"/><entry><title>B</title><link href="https://ex/b"/><updated>2006-01-02T15:04:05Z</updated><summary>sb</summary></entry></feed>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		if _, err := w.Write([]byte(atom)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}))
	defer srv.Close()

	bin := testutil.BuildTool(t, "rss_fetch")
	env := append(os.Environ(), "RSS_FETCH_ALLOW_LOCAL=1")
	outStr, errStr, err := runTool(t, bin, env, map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	if !strings.Contains(outStr, "\"feed\":") || !strings.Contains(outStr, "\"items\":") {
		t.Fatalf("unexpected output: %s", outStr)
	}
	if !strings.Contains(outStr, "https://ex/b") {
		t.Fatalf("missing item url: %s", outStr)
	}
}

func TestRSSFetch_304NotModified(t *testing.T) {
	etag := "W/\"abc\""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-Modified-Since") != "" || r.Header.Get("If-None-Match") != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "application/rss+xml")
		if _, err := w.Write([]byte("<rss/>")); err != nil {
			t.Fatalf("write: %v", err)
		}
	}))
	defer srv.Close()

	bin := testutil.BuildTool(t, "rss_fetch")
	env := append(os.Environ(), "RSS_FETCH_ALLOW_LOCAL=1")
	// First fetch
	_, errStr, err := runTool(t, bin, env, map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	// Second fetch with If-Modified-Since triggers 304
	outStr2, _, err2 := runTool(t, bin, env, map[string]any{"url": srv.URL, "if_modified_since": "Mon, 02 Jan 2006 15:04:05 GMT"})
	if err2 != nil {
		t.Fatalf("second run error: %v", err2)
	}
	if !strings.Contains(outStr2, "\"items\":[]") {
		t.Fatalf("expected empty items on 304: %s", outStr2)
	}
}

func TestRSSFetch_SSRFBlocked(t *testing.T) {
	bin := testutil.BuildTool(t, "rss_fetch")
	env := []string{"RSS_FETCH_ALLOW_LOCAL=0"}
	_, errStr, err := runTool(t, bin, env, map[string]any{"url": "http://127.0.0.1:9"})
	if err == nil {
		t.Fatalf("expected error but got ok")
	}
	if !strings.Contains(errStr, "SSRF") {
		t.Fatalf("expected SSRF error, got: %s", errStr)
	}
}

func TestRSSFetch_BadInput(t *testing.T) {
	bin := testutil.BuildTool(t, "rss_fetch")
	env := append(os.Environ(), "RSS_FETCH_ALLOW_LOCAL=1")
	_, errStr, err := runTool(t, bin, env, map[string]any{"url": ":bad"})
	if err == nil {
		t.Fatalf("expected error for bad url")
	}
	if !strings.Contains(errStr, "http/https") {
		t.Fatalf("unexpected stderr: %s", errStr)
	}
}
