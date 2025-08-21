package main

import (
    "bytes"
    "strings"
    "testing"
)

func TestHelpAndVersionRequested(t *testing.T) {
    if !helpRequested([]string{"--help"}) { t.Fatalf("--help not detected") }
    if !helpRequested([]string{"-h"}) { t.Fatalf("-h not detected") }
    if !helpRequested([]string{"help"}) { t.Fatalf("help not detected") }
    if helpRequested([]string{"--nohelp"}) { t.Fatalf("false positive help") }

    if !versionRequested([]string{"--version"}) { t.Fatalf("--version not detected") }
    if !versionRequested([]string{"-version"}) { t.Fatalf("-version not detected") }
    if versionRequested([]string{"version"}) { t.Fatalf("false positive version") }
}

func TestPrintUsage_ContainsKeySections(t *testing.T) {
    var buf bytes.Buffer
    printUsage(&buf)
    out := buf.String()
    for _, want := range []string{
        "Usage:",
        "Flags (precedence:",
        "-prompt string",
        "-tools string",
        "--version | -version",
        "Examples:",
    } {
        if !strings.Contains(out, want) {
            t.Fatalf("usage missing %q in:\n%s", want, out)
        }
    }
}
