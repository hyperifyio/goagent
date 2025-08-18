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

// rcOutput intentionally omitted; tests validate via substring checks.

// Allow local httptest origins during tests
func TestMain(m *testing.M) {
	if err := os.Setenv("ROBOTS_CHECK_ALLOW_LOCAL", "1"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func runRobots(t *testing.T, bin string, input any) (string, string, error) {
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

// UA-specific rules must take precedence over wildcard.
func TestRobotsCheck_UAPrecedence_DenySpecific(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		// RFC 9309: specific UA group applies when matched
		// Specific denies /private, while wildcard allows all
		if _, err := w.Write([]byte("User-agent: agentcli\nDisallow: /private\n\nUser-agent: *\nAllow: /\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	bin := testutil.BuildTool(t, "robots_check")
	outStr, errStr, err := runRobots(t, bin, map[string]any{
		"url":        srv.URL + "/private/page",
		"user_agent": "agentcli",
	})
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	if !strings.Contains(outStr, "\"allowed\":false") {
		t.Fatalf("expected allowed=false, got: %s", outStr)
	}
	if !strings.Contains(outStr, "group_rules") {
		t.Fatalf("expected group_rules in output")
	}
}

// Crawl-delay should be exposed in milliseconds when present in matched group.
func TestRobotsCheck_CrawlDelay_Parsed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		if _, err := w.Write([]byte("User-agent: agentcli\nCrawl-delay: 2\nAllow: /\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	bin := testutil.BuildTool(t, "robots_check")
	outStr, errStr, err := runRobots(t, bin, map[string]any{
		"url":        srv.URL + "/anything",
		"user_agent": "agentcli",
	})
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	if !strings.Contains(outStr, "\"allowed\":true") {
		t.Fatalf("expected allowed=true, got: %s", outStr)
	}
	if !strings.Contains(outStr, "\"crawl_delay_ms\":2000") {
		t.Fatalf("expected crawl_delay_ms=2000, got: %s", outStr)
	}
}

// Wildcard-only group should apply when UA does not match a specific group.
func TestRobotsCheck_WildcardFallback_DenyAll(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		if _, err := w.Write([]byte("User-agent: *\nDisallow: /\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	bin := testutil.BuildTool(t, "robots_check")
	outStr, errStr, err := runRobots(t, bin, map[string]any{
		"url":        srv.URL + "/blocked",
		"user_agent": "otherbot",
	})
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	if !strings.Contains(outStr, "\"allowed\":false") {
		t.Fatalf("expected allowed=false, got: %s", outStr)
	}
}
