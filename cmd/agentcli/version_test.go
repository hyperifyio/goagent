package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestShortCommit(t *testing.T) {
	if got := shortCommit(""); got != "unknown" { t.Fatalf("empty commit => %q", got) }
	if got := shortCommit("abc"); got != "abc" { t.Fatalf("short commit => %q", got) }
	if got := shortCommit("abcdef012345"); got != "abcdef0" { t.Fatalf("long commit => %q", got) }
}

func TestPrintVersion(t *testing.T) {
	version = "v1.2.3"
	commit = "abcdef012345"
	buildDate = "2025-08-21"
	var buf bytes.Buffer
	printVersion(&buf)
	out := buf.String()
	if !strings.Contains(out, "agentcli version v1.2.3") { t.Fatalf("missing version: %q", out) }
	if !strings.Contains(out, "commit abcdef0") { t.Fatalf("missing short commit: %q", out) }
	if !strings.Contains(out, "built 2025-08-21") { t.Fatalf("missing build date: %q", out) }
}
