package main

import (
	"fmt"
	"io"
	"strings"
)

// Build-time variables set via -ldflags; defaults are useful for dev builds.
var (
	version   = "v0.0.0-dev"
	commit    = "unknown"
	buildDate = "unknown"
)

// printVersion writes a concise single-line version string to stdout.
func printVersion(w io.Writer) {
	safeFprintln(w, fmt.Sprintf("agentcli version %s (commit %s, built %s)", version, shortCommit(commit), buildDate))
}

func shortCommit(c string) string {
	c = strings.TrimSpace(c)
	if len(c) > 7 {
		return c[:7]
	}
	if c == "" {
		return "unknown"
	}
	return c
}
