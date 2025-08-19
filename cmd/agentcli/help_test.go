package main

import (
	"strings"
	"testing"
)

// TestHelpSnapshot_ContainsAllExpectedTokens asserts that the built-in help output
// contains all critical flags and key phrases (inheritance, conflicts, defaults).
// This guards against accidental drift in help documentation.
func TestHelpSnapshot_ContainsAllExpectedTokens(t *testing.T) {
	var b strings.Builder
	printUsage(&b)
	help := b.String()

	// Minimal sanity checks
	for _, token := range []string{
		"agentcli —",
		"Usage:",
		"precedence: flag > env > default",
	} {
		if !strings.Contains(help, token) {
			t.Fatalf("help missing required token %q; help=\n%s", token, help)
		}
	}

	// Flags to assert are documented in help
	flags := []string{
		"-prompt string",
		"-tools string",
		"-system string",
		"-system-file string",
		"-developer string",
		"-developer-file string",
		"-prompt-file string",
		"-base-url string",
		"-api-key string",
		"-model string",
		"-max-steps int",
		"-timeout duration",
		"-http-timeout duration",
		"-prep-http-timeout duration",
		"-tool-timeout duration",
		"-http-retries int",
		"-http-retry-backoff duration",
		"-image-base-url string",
		"-image-model string",
		"-image-api-key string",
		"-image-http-timeout duration",
		"-image-http-retries int",
		"-image-http-retry-backoff duration",
		"-temp float",
		"-top-p float",
		"-prep-profile string",
		"-prep-model string",
		"-prep-base-url string",
		"-prep-api-key string",
		"-prep-http-retries int",
		"-prep-http-retry-backoff duration",
		"-prep-temp float",
		"-prep-top-p float",
		"-prep-system string",
		"-prep-system-file string",
		"-image-n int",
		"-image-size string",
		"-image-quality string",
		"-image-style string",
		"-image-response-format string",
		"-image-transparent-background",
		"-debug",
		"-verbose",
		"-quiet",
		"-prep-tools-allow-external",
		"-prep-cache-bust",
		"-prep-tools string",
		"-prep-dry-run",
		"-print-messages",
		"-stream-final",
		"-channel-route",
		"-save-messages string",
		"-load-messages string",
		"-prep-enabled",
		"-capabilities",
		"-print-config",
		"--version | -version",
	}
	for _, f := range flags {
		if !strings.Contains(help, f) {
			t.Fatalf("help missing flag token %q; help=\n%s", f, help)
		}
	}

	// Key phrases to ensure important semantics remain documented
	phrases := []string{
		"conflicts with -temp",
		"conflicts with -prep-top-p",
		"conflicts with -prep-temp",
		"mutually exclusive with -prep-system-file",
		"mutually exclusive with -system",
		"mutually exclusive with -prompt",
		"'-' for STDIN",
		"repeatable",
		"inherits -http-timeout if unset",
		"inherits -http-retries if unset",
		"inherits -http-retry-backoff if unset",
		"default 1.0",
		"default 2",
		"default 500ms",
		"default 1024x1024",
		"default standard",
		"default natural",
		"default url",
	}
	for _, p := range phrases {
		if !strings.Contains(help, p) {
			t.Fatalf("help missing key phrase %q; help=\n%s", p, help)
		}
	}
}
