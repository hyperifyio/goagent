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

// TestParseFlags_PrepSystem_Exclusivity ensures -prep-system and -prep-system-file are mutually exclusive.
func TestParseFlags_PrepSystem_Exclusivity(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()
	os.Args = []string{"agentcli.test", "-prompt", "p", "-prep-system", "X", "-prep-system-file", "-"}
	_, code := parseFlags()
	if code != 2 {
		t.Fatalf("parseFlags exit = %d; want 2 (mutual exclusion)", code)
	}
}

// TestPrepSystem_EnvAndFlagPrecedence ensures env is used when flags unset and flag overrides env.
func TestPrepSystem_EnvAndFlagPrecedence(t *testing.T) {
	t.Setenv("OAI_PREP_SYSTEM", "ENV_SYS")
	t.Setenv("OAI_PREP_SYSTEM_FILE", "")
	// When flags unset, env should populate cfg.prepSystem
	orig := os.Args
	defer func() { os.Args = orig }()
	os.Args = []string{"agentcli.test", "-prompt", "p"}
	cfg, code := parseFlags()
	if code != 0 {
		t.Fatalf("parseFlags exit=%d; want 0", code)
	}
	if strings.TrimSpace(cfg.prepSystem) != "ENV_SYS" {
		t.Fatalf("prepSystem=%q; want ENV_SYS", cfg.prepSystem)
	}
	// Flag should override env
	os.Args = []string{"agentcli.test", "-prompt", "p", "-prep-system", "FLAG_SYS"}
	cfg, code = parseFlags()
	if code != 0 {
		t.Fatalf("parseFlags exit=%d; want 0", code)
	}
	if strings.TrimSpace(cfg.prepSystem) != "FLAG_SYS" {
		t.Fatalf("prepSystem=%q; want FLAG_SYS", cfg.prepSystem)
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

// TestImageParamDefaultsAndEnvAndFlags verifies precedence and defaults for image param pass-throughs.
//
//nolint:gocyclo // Intentional multi-branch table-style assertions for env/flag precedence in one test.
func TestImageParamDefaultsAndEnvAndFlags(t *testing.T) {
	// Clear possibly impacting envs
	t.Setenv("OAI_IMAGE_N", "")
	t.Setenv("OAI_IMAGE_SIZE", "")
	t.Setenv("OAI_IMAGE_QUALITY", "")
	t.Setenv("OAI_IMAGE_STYLE", "")
	t.Setenv("OAI_IMAGE_RESPONSE_FORMAT", "")
	t.Setenv("OAI_IMAGE_TRANSPARENT_BACKGROUND", "")

	t.Run("defaults when neither flags nor env", func(t *testing.T) { //nolint:tparallel // serial to avoid env races
		orig := os.Args
		defer func() { os.Args = orig }()
		os.Args = []string{"agentcli.test", "-prompt", "p"}
		cfg, code := parseFlags()
		if code != 0 {
			t.Fatalf("parseFlags exit=%d; want 0", code)
		}
		if cfg.imageN != 1 || cfg.imageSize != "1024x1024" || cfg.imageQuality != "standard" || cfg.imageStyle != "natural" || cfg.imageResponseFormat != "url" || cfg.imageTransparentBackground != false {
			t.Fatalf("defaults mismatch: n=%d size=%s quality=%s style=%s resp=%s transparent=%v", cfg.imageN, cfg.imageSize, cfg.imageQuality, cfg.imageStyle, cfg.imageResponseFormat, cfg.imageTransparentBackground)
		}
	})

	t.Run("env applies when flags unset", func(t *testing.T) { //nolint:tparallel
			t.Setenv("OAI_IMAGE_N", "3")
			t.Setenv("OAI_IMAGE_SIZE", "512x512")
			t.Setenv("OAI_IMAGE_QUALITY", "hd")
			t.Setenv("OAI_IMAGE_STYLE", "vivid")
			t.Setenv("OAI_IMAGE_RESPONSE_FORMAT", "b64_json")
			t.Setenv("OAI_IMAGE_TRANSPARENT_BACKGROUND", "true")
			orig := os.Args
			defer func() { os.Args = orig }()
			os.Args = []string{"agentcli.test", "-prompt", "p"}
			cfg, code := parseFlags()
			if code != 0 {
				t.Fatalf("parseFlags exit=%d; want 0", code)
			}
			if cfg.imageN != 3 || cfg.imageSize != "512x512" || cfg.imageQuality != "hd" || cfg.imageStyle != "vivid" || cfg.imageResponseFormat != "b64_json" || cfg.imageTransparentBackground != true {
				t.Fatalf("env mismatch: n=%d size=%s quality=%s style=%s resp=%s transparent=%v", cfg.imageN, cfg.imageSize, cfg.imageQuality, cfg.imageStyle, cfg.imageResponseFormat, cfg.imageTransparentBackground)
			}
		})

	t.Run("flags override env", func(t *testing.T) { //nolint:tparallel
			t.Setenv("OAI_IMAGE_N", "2")
			t.Setenv("OAI_IMAGE_SIZE", "256x256")
			t.Setenv("OAI_IMAGE_QUALITY", "standard")
			t.Setenv("OAI_IMAGE_STYLE", "natural")
			t.Setenv("OAI_IMAGE_RESPONSE_FORMAT", "url")
			t.Setenv("OAI_IMAGE_TRANSPARENT_BACKGROUND", "false")
			orig := os.Args
			defer func() { os.Args = orig }()
			os.Args = []string{"agentcli.test", "-prompt", "p", "-image-n", "4", "-image-size", "640x640", "-image-quality", "hd", "-image-style", "vivid", "-image-response-format", "b64_json", "-image-transparent-background", "true"}
			cfg, code := parseFlags()
			if code != 0 {
				t.Fatalf("parseFlags exit=%d; want 0", code)
			}
			if cfg.imageN != 4 || cfg.imageSize != "640x640" || cfg.imageQuality != "hd" || cfg.imageStyle != "vivid" || cfg.imageResponseFormat != "b64_json" || cfg.imageTransparentBackground != true {
				t.Fatalf("flags mismatch: n=%d size=%s quality=%s style=%s resp=%s transparent=%v", cfg.imageN, cfg.imageSize, cfg.imageQuality, cfg.imageStyle, cfg.imageResponseFormat, cfg.imageTransparentBackground)
			}
		})
}

// TestImageModelFlagPrecedence verifies precedence flag > env > default for -image-model.
func TestImageModelFlagPrecedence(t *testing.T) {
	t.Run("default when neither flags nor env", func(t *testing.T) { //nolint:tparallel
		t.Setenv("OAI_IMAGE_MODEL", "")
		orig := os.Args
		defer func() { os.Args = orig }()
		os.Args = []string{"agentcli.test", "-prompt", "p"}
		cfg, code := parseFlags()
		if code != 0 {
			t.Fatalf("parseFlags exit=%d; want 0", code)
		}
		if cfg.imageModel != "gpt-image-1" {
			t.Fatalf("imageModel=%q; want gpt-image-1", cfg.imageModel)
		}
	})

	t.Run("env applies when flag unset", func(t *testing.T) { //nolint:tparallel
			t.Setenv("OAI_IMAGE_MODEL", "env-model")
			orig := os.Args
			defer func() { os.Args = orig }()
			os.Args = []string{"agentcli.test", "-prompt", "p"}
			cfg, code := parseFlags()
			if code != 0 {
				t.Fatalf("parseFlags exit=%d; want 0", code)
			}
			if cfg.imageModel != "env-model" {
				t.Fatalf("imageModel=%q; want env-model", cfg.imageModel)
			}
		})

	t.Run("flags override env", func(t *testing.T) { //nolint:tparallel
			t.Setenv("OAI_IMAGE_MODEL", "env-model")
			orig := os.Args
			defer func() { os.Args = orig }()
			os.Args = []string{"agentcli.test", "-prompt", "p", "-image-model", "flag-model"}
			cfg, code := parseFlags()
			if code != 0 {
				t.Fatalf("parseFlags exit=%d; want 0", code)
			}
			if cfg.imageModel != "flag-model" {
				t.Fatalf("imageModel=%q; want flag-model", cfg.imageModel)
			}
		})
}

// TestPrintConfig_IncludesImageParams verifies print-config output reflects resolved image params.
func TestPrintConfig_IncludesImageParams(t *testing.T) {
	orig := os.Args
	defer func() { os.Args = orig }()
	os.Args = []string{"agentcli.test", "-prompt", "p", "-image-n", "2", "-image-size", "512x512", "-image-quality", "hd", "-image-style", "vivid", "-image-response-format", "b64_json", "-image-transparent-background", "true", "-print-config"}
	var out bytes.Buffer
	// parse then print
	cfg, code := parseFlags()
	if code != 0 {
		t.Fatalf("parseFlags exit=%d; want 0", code)
	}
	exit := printResolvedConfig(cfg, &out)
	if exit != 0 {
		t.Fatalf("printResolvedConfig exit=%d; want 0", exit)
	}
	s := out.String()
	for _, token := range []string{
		"\"image\"",
		"\"n\": 2",
		"\"size\": \"512x512\"",
		"\"quality\": \"hd\"",
		"\"style\": \"vivid\"",
		"\"response_format\": \"b64_json\"",
		"\"transparent_background\": true",
	} {
		if !strings.Contains(s, token) {
			t.Fatalf("print-config missing %s in output: %s", token, s)
		}
	}
}

// Avoid unused imports on some platforms
var _ = runtime.GOOS

// TestHTTPRetryPrecedence verifies precedence and defaults for -http-retries and -http-retry-backoff.
func TestHTTPRetryPrecedence(t *testing.T) {
	// Save and restore env using t.Setenv for cleanliness
	t.Setenv("OAI_HTTP_RETRIES", "")
	t.Setenv("OAI_HTTP_RETRY_BACKOFF", "")

	t.Run("defaults when neither flags nor env", func(t *testing.T) {
		orig := os.Args
		defer func() { os.Args = orig }()
		os.Args = []string{"agentcli.test", "-prompt", "p"}
		cfg, code := parseFlags()
		if code != 0 {
			t.Fatalf("parseFlags exit=%d; want 0", code)
		}
		if cfg.httpRetries != 2 {
			t.Fatalf("httpRetries=%d; want 2", cfg.httpRetries)
		}
		if cfg.httpBackoff.String() != "500ms" {
			t.Fatalf("httpBackoff=%s; want 500ms", cfg.httpBackoff)
		}
	})

	t.Run("env applies when flags unset", func(t *testing.T) {
			t.Setenv("OAI_HTTP_RETRIES", "5")
			t.Setenv("OAI_HTTP_RETRY_BACKOFF", "750ms")
			orig := os.Args
			defer func() { os.Args = orig }()
			os.Args = []string{"agentcli.test", "-prompt", "p"}
			cfg, code := parseFlags()
			if code != 0 {
				t.Fatalf("parseFlags exit=%d; want 0", code)
			}
			if cfg.httpRetries != 5 {
				t.Fatalf("httpRetries=%d; want 5", cfg.httpRetries)
			}
			if cfg.httpBackoff.String() != "750ms" {
				t.Fatalf("httpBackoff=%s; want 750ms", cfg.httpBackoff)
			}
		})

	t.Run("flags override env", func(t *testing.T) {
			t.Setenv("OAI_HTTP_RETRIES", "7")
			t.Setenv("OAI_HTTP_RETRY_BACKOFF", "900ms")
			orig := os.Args
			defer func() { os.Args = orig }()
			os.Args = []string{"agentcli.test", "-prompt", "p", "-http-retries", "3", "-http-retry-backoff", "1s"}
			cfg, code := parseFlags()
			if code != 0 {
				t.Fatalf("parseFlags exit=%d; want 0", code)
			}
			if cfg.httpRetries != 3 {
				t.Fatalf("httpRetries=%d; want 3", cfg.httpRetries)
			}
			if cfg.httpBackoff.String() != "1s" {
				t.Fatalf("httpBackoff=%s; want 1s", cfg.httpBackoff)
			}
		})

	t.Run("explicit zero via flags retains zero", func(t *testing.T) {
		orig := os.Args
		defer func() { os.Args = orig }()
		os.Args = []string{"agentcli.test", "-prompt", "p", "-http-retries", "0", "-http-retry-backoff", "0"}
		cfg, code := parseFlags()
		if code != 0 {
			t.Fatalf("parseFlags exit=%d; want 0", code)
		}
		if cfg.httpRetries != 0 {
			t.Fatalf("httpRetries=%d; want 0", cfg.httpRetries)
		}
		if cfg.httpBackoff != 0 {
			t.Fatalf("httpBackoff=%s; want 0", cfg.httpBackoff)
		}
	})
}

// TestImageHTTPKnobsPrecedence verifies precedence and inheritance for image HTTP knobs.
func TestImageHTTPKnobsPrecedence(t *testing.T) {
	t.Run("defaults inherit from main when neither flags nor env", func(t *testing.T) {
		// Clear envs that might affect defaults
		t.Setenv("OAI_IMAGE_HTTP_TIMEOUT", "")
		t.Setenv("OAI_IMAGE_HTTP_RETRIES", "")
		t.Setenv("OAI_IMAGE_HTTP_RETRY_BACKOFF", "")
		t.Setenv("OAI_HTTP_TIMEOUT", "")
		t.Setenv("OAI_HTTP_RETRIES", "")
		t.Setenv("OAI_HTTP_RETRY_BACKOFF", "")

		orig := os.Args
		defer func() { os.Args = orig }()
		os.Args = []string{"agentcli.test", "-prompt", "p"}
		cfg, code := parseFlags()
		if code != 0 {
			t.Fatalf("parseFlags exit=%d; want 0", code)
		}
		if cfg.imageHTTPTimeout != cfg.httpTimeout {
			t.Fatalf("imageHTTPTimeout=%s; want inherit %s", cfg.imageHTTPTimeout, cfg.httpTimeout)
		}
		if cfg.imageHTTPRetries != cfg.httpRetries {
			t.Fatalf("imageHTTPRetries=%d; want inherit %d", cfg.imageHTTPRetries, cfg.httpRetries)
		}
		if cfg.imageHTTPBackoff != cfg.httpBackoff {
			t.Fatalf("imageHTTPBackoff=%s; want inherit %s", cfg.imageHTTPBackoff, cfg.httpBackoff)
		}
	})

	t.Run("env applies when flags unset", func(t *testing.T) {
			t.Setenv("OAI_IMAGE_HTTP_TIMEOUT", "7s")
			t.Setenv("OAI_IMAGE_HTTP_RETRIES", "9")
			t.Setenv("OAI_IMAGE_HTTP_RETRY_BACKOFF", "1.5s")
			orig := os.Args
			defer func() { os.Args = orig }()
			os.Args = []string{"agentcli.test", "-prompt", "p"}
			cfg, code := parseFlags()
			if code != 0 {
				t.Fatalf("parseFlags exit=%d; want 0", code)
			}
			if cfg.imageHTTPTimeout.String() != "7s" {
				t.Fatalf("imageHTTPTimeout=%s; want 7s", cfg.imageHTTPTimeout)
			}
			if cfg.imageHTTPRetries != 9 {
				t.Fatalf("imageHTTPRetries=%d; want 9", cfg.imageHTTPRetries)
			}
			if cfg.imageHTTPBackoff.String() != "1.5s" {
				t.Fatalf("imageHTTPBackoff=%s; want 1.5s", cfg.imageHTTPBackoff)
			}
		})

	t.Run("flags override env", func(t *testing.T) {
			t.Setenv("OAI_IMAGE_HTTP_TIMEOUT", "7s")
			t.Setenv("OAI_IMAGE_HTTP_RETRIES", "9")
			t.Setenv("OAI_IMAGE_HTTP_RETRY_BACKOFF", "1.5s")
			orig := os.Args
			defer func() { os.Args = orig }()
			os.Args = []string{"agentcli.test", "-prompt", "p", "-image-http-timeout", "3s", "-image-http-retries", "5", "-image-http-retry-backoff", "1s"}
			cfg, code := parseFlags()
			if code != 0 {
				t.Fatalf("parseFlags exit=%d; want 0", code)
			}
			if cfg.imageHTTPTimeout.String() != "3s" {
				t.Fatalf("imageHTTPTimeout=%s; want 3s", cfg.imageHTTPTimeout)
			}
			if cfg.imageHTTPRetries != 5 {
				t.Fatalf("imageHTTPRetries=%d; want 5", cfg.imageHTTPRetries)
			}
			if cfg.imageHTTPBackoff.String() != "1s" {
				t.Fatalf("imageHTTPBackoff=%s; want 1s", cfg.imageHTTPBackoff)
			}
		})
}
