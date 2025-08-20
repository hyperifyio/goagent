package main

import (
	"io"
	"os"
	"strings"
)

func main() {
	os.Exit(cliMain(os.Args[1:], os.Stdout, os.Stderr))
}

// cliMain is a testable entrypoint for the CLI. It accepts argv (excluding program name)
// and writers for stdout/stderr, returns the intended process exit code, and performs
// no global side effects beyond temporarily setting os.Args for flag parsing.
func cliMain(args []string, stdout io.Writer, stderr io.Writer) int {
	// Handle help flags prior to any parsing/validation or side effects
	if helpRequested(args) {
		printUsage(stdout)
		return 0
	}
	// Handle version flags prior to parsing/validation
	if versionRequested(args) {
		printVersion(stdout)
		return 0
	}

	// Temporarily set os.Args so parseFlags() (which reads os.Args) sees our args
	origArgs := os.Args
	os.Args = append([]string{origArgs[0]}, args...)
	defer func() { os.Args = origArgs }()

	cfg, exitOn := parseFlags()
	if exitOn != 0 {
		if strings.TrimSpace(cfg.parseError) != "" {
			safeFprintln(stderr, cfg.parseError)
		} else {
			safeFprintln(stderr, "error: -prompt is required")
		}
		// Also print usage synopsis for guidance
		printUsage(stderr)
		return exitOn
	}
	// Global dry-run: print intended state actions and exit without executing network calls or writing state
	if cfg.dryRun {
		return printStateDryRunPlan(cfg, stdout, stderr)
	}
	if cfg.printConfig {
		return printResolvedConfig(cfg, stdout)
	}
	if cfg.capabilities {
		return printCapabilities(cfg, stdout, stderr)
	}
	if cfg.prepDryRun {
		return runPrepDryRun(cfg, stdout, stderr)
	}
	return runAgent(cfg, stdout, stderr)
}
