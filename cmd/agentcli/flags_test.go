//nolint:errcheck // Tests intentionally allow some unchecked errors for pipe/env helpers.
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestParseFlags_SystemAndSystemFile_MutuallyExclusive ensures providing both
// -system (non-default) and -system-file results in exit code 2 from parseFlags.
func TestParseFlags_SystemAndSystemFile_MutuallyExclusive(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"agentcli.test", "-system", "custom", "-system-file", "sys.txt", "-prompt", "p"}

	_, code := parseFlags()
	if code != 2 {
		t.Fatalf("parseFlags exit = %d; want 2 (mutual exclusion)", code)
	}
}

// TestParseFlags_PromptAndPromptFile_MutuallyExclusive ensures providing both
// -prompt and -prompt-file results in exit code 2 from parseFlags.
func TestParseFlags_PromptAndPromptFile_MutuallyExclusive(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"agentcli.test", "-prompt", "inline", "-prompt-file", "p.txt"}

	_, code := parseFlags()
	if code != 2 {
		t.Fatalf("parseFlags exit = %d; want 2 (mutual exclusion)", code)
	}
}

// TestResolveMaybeFile_InlinePreferred returns inline when filePath empty.
func TestResolveMaybeFile_InlinePreferred(t *testing.T) {
	got, err := resolveMaybeFile("inline", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "inline" {
		t.Fatalf("got %q; want %q", got, "inline")
	}
}

// TestResolveMaybeFile_File reads content from a real file.
func TestResolveMaybeFile_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.txt")
	if err := os.WriteFile(path, []byte("from-file"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	got, err := resolveMaybeFile("inline-ignored", path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "from-file" {
		t.Fatalf("got %q; want %q", got, "from-file")
	}
}

// TestResolveMaybeFile_STDIN reads content when filePath is "-".
func TestResolveMaybeFile_STDIN(t *testing.T) {
	// Save and restore os.Stdin
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	r := bytes.NewBufferString("from-stdin")
	// Create a pipe and write contents so io.ReadAll can consume it as Stdin
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	// Write and close writer
	if _, err := pw.Write(r.Bytes()); err != nil {
		t.Fatalf("write to pipe: %v", err)
	}
	_ = pw.Close()
	os.Stdin = pr

	got, err := resolveMaybeFile("ignored", "-")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(got) != "from-stdin" {
		t.Fatalf("got %q; want %q", got, "from-stdin")
	}
}

// TestResolveDeveloperMessages_Order ensures files are read first (in order),
// followed by inline -developer values (in order).
func TestResolveDeveloperMessages_Order(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "dev1.txt")
	f2 := filepath.Join(dir, "dev2.txt")
	if err := os.WriteFile(f1, []byte("file-1"), 0o644); err != nil {
		t.Fatalf("write f1: %v", err)
	}
	if err := os.WriteFile(f2, []byte("file-2"), 0o644); err != nil {
		t.Fatalf("write f2: %v", err)
	}

	devs, err := resolveDeveloperMessages([]string{"inline-1", "inline-2"}, []string{f1, f2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"file-1", "file-2", "inline-1", "inline-2"}
	if len(devs) != len(want) {
		t.Fatalf("len(devs)=%d; want %d", len(devs), len(want))
	}
	for i := range want {
		if strings.TrimSpace(devs[i]) != want[i] {
			t.Fatalf("devs[%d]=%q; want %q", i, devs[i], want[i])
		}
	}
}

// TestResolveDeveloperMessages_STDIN ensures a "-" entry is read from stdin.
func TestResolveDeveloperMessages_STDIN(t *testing.T) {
	// Save and restore os.Stdin
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	// Prepare stdin data
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := pw.Write([]byte("dev-stdin")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = pw.Close()
	os.Stdin = pr

	devs, err := resolveDeveloperMessages([]string{"inline"}, []string{"-"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(devs) != 2 {
		t.Fatalf("len(devs)=%d; want 2", len(devs))
	}
	if strings.TrimSpace(devs[0]) != "dev-stdin" {
		t.Fatalf("first dev from stdin = %q; want %q", devs[0], "dev-stdin")
	}
	if strings.TrimSpace(devs[1]) != "inline" {
		t.Fatalf("second dev inline = %q; want %q", devs[1], "inline")
	}
}

// TestHelpContainsRoleFlags ensures help output mentions the role flags, as a smoke test.
func TestHelpContainsRoleFlags(t *testing.T) {
	var b strings.Builder
	printUsage(&b)
	help := b.String()
	for _, token := range []string{"-developer", "-developer-file", "-prompt-file", "-system-file"} {
		if !strings.Contains(help, token) {
			t.Fatalf("help missing %s token; help=\n%s", token, help)
		}
	}
}

// Avoid unused imports on some platforms
var _ = runtime.GOOS

// TestHTTPRetryPrecedence verifies precedence and defaults for -http-retries and -http-retry-backoff.
func TestHTTPRetryPrecedence(t *testing.T) {
    // Save and restore env
    unset := func(k string) func() { v, ok := os.LookupEnv(k); if ok { return func(){ _ = os.Setenv(k, v) } }; _ = os.Unsetenv(k); return func(){} }
    restore1 := unset("OAI_HTTP_RETRIES")
    defer restore1()
    restore2 := unset("OAI_HTTP_RETRY_BACKOFF")
    defer restore2()

    t.Run("defaults when neither flags nor env", func(t *testing.T) {
        orig := os.Args; defer func(){ os.Args = orig }()
        os.Args = []string{"agentcli.test", "-prompt", "p"}
        cfg, code := parseFlags()
        if code != 0 { t.Fatalf("parseFlags exit=%d; want 0", code) }
        if cfg.httpRetries != 2 { t.Fatalf("httpRetries=%d; want 2", cfg.httpRetries) }
        if cfg.httpBackoff.String() != "500ms" { t.Fatalf("httpBackoff=%s; want 500ms", cfg.httpBackoff) }
    })

    t.Run("env applies when flags unset", func(t *testing.T) {
        _ = os.Setenv("OAI_HTTP_RETRIES", "5")
        _ = os.Setenv("OAI_HTTP_RETRY_BACKOFF", "750ms")
        orig := os.Args; defer func(){ os.Args = orig }()
        os.Args = []string{"agentcli.test", "-prompt", "p"}
        cfg, code := parseFlags()
        if code != 0 { t.Fatalf("parseFlags exit=%d; want 0", code) }
        if cfg.httpRetries != 5 { t.Fatalf("httpRetries=%d; want 5", cfg.httpRetries) }
        if cfg.httpBackoff.String() != "750ms" { t.Fatalf("httpBackoff=%s; want 750ms", cfg.httpBackoff) }
        _ = os.Unsetenv("OAI_HTTP_RETRIES")
        _ = os.Unsetenv("OAI_HTTP_RETRY_BACKOFF")
    })

    t.Run("flags override env", func(t *testing.T) {
        _ = os.Setenv("OAI_HTTP_RETRIES", "7")
        _ = os.Setenv("OAI_HTTP_RETRY_BACKOFF", "900ms")
        orig := os.Args; defer func(){ os.Args = orig }()
        os.Args = []string{"agentcli.test", "-prompt", "p", "-http-retries", "3", "-http-retry-backoff", "1s"}
        cfg, code := parseFlags()
        if code != 0 { t.Fatalf("parseFlags exit=%d; want 0", code) }
        if cfg.httpRetries != 3 { t.Fatalf("httpRetries=%d; want 3", cfg.httpRetries) }
        if cfg.httpBackoff.String() != "1s" { t.Fatalf("httpBackoff=%s; want 1s", cfg.httpBackoff) }
        _ = os.Unsetenv("OAI_HTTP_RETRIES")
        _ = os.Unsetenv("OAI_HTTP_RETRY_BACKOFF")
    })

    t.Run("explicit zero via flags retains zero", func(t *testing.T) {
        orig := os.Args; defer func(){ os.Args = orig }()
        os.Args = []string{"agentcli.test", "-prompt", "p", "-http-retries", "0", "-http-retry-backoff", "0"}
        cfg, code := parseFlags()
        if code != 0 { t.Fatalf("parseFlags exit=%d; want 0", code) }
        if cfg.httpRetries != 0 { t.Fatalf("httpRetries=%d; want 0", cfg.httpRetries) }
        if cfg.httpBackoff != 0 { t.Fatalf("httpBackoff=%s; want 0", cfg.httpBackoff) }
    })
}
