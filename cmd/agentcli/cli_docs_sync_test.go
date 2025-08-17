package main

import (
    "os"
    "path/filepath"
    "runtime"
    "strings"
    "testing"
)

// TestCLIReference_IncludesAllFlagsFromHelp ensures that docs/reference/cli-reference.md
// includes every flag token that appears in the CLI's built-in help output.
func TestCLIReference_IncludesAllFlagsFromHelp(t *testing.T) {
    // Render help text via the same function used by the CLI for --help/-h/help.
    var b strings.Builder
    printUsage(&b)
    help := b.String()

    // Extract flag tokens from help output. Lines start with two spaces, a hyphen, then the flag.
    var flags []string
    for _, line := range strings.Split(help, "\n") {
        line = strings.TrimRight(line, "\r")
        if strings.HasPrefix(line, "  -") || strings.HasPrefix(line, "  --") {
            // Take the first whitespace-separated token as the flag token (e.g., -prompt, --version)
            fields := strings.Fields(line)
            if len(fields) > 0 {
                token := fields[0]
                // Normalize trailing punctuation if any
                token = strings.TrimRight(token, ":")
                flags = append(flags, token)
            }
        }
    }
    if len(flags) == 0 {
        t.Fatalf("no flags parsed from help; help was:\n%s", help)
    }

    // Load CLI reference doc. Resolve relative to this test file's directory for robustness.
    _, thisFile, _, ok := runtime.Caller(0)
    if !ok {
        t.Fatalf("runtime.Caller failed")
    }
    thisDir := filepath.Dir(thisFile)
    // Repo root is the parent of the parent (.. of cmd/agentcli => repo root)
    repoRoot := filepath.Dir(filepath.Dir(thisDir))
    tryPaths := []string{
        filepath.Join(repoRoot, "docs", "reference", "cli-reference.md"),
        filepath.Join(repoRoot, "README.md"), // fallback so test gives a clearer error if mislocated
    }
    var data []byte
    var err error
    var usedPath string
    for _, p := range tryPaths {
        if b, e := os.ReadFile(p); e == nil {
            data, err, usedPath = b, nil, p
            break
        } else {
            err = e
        }
    }
    if data == nil {
        t.Fatalf("failed to read CLI reference doc from %v: last error: %v", tryPaths, err)
    }
    _ = usedPath // retained for potential future diagnostics

    doc := string(data)

    // For each flag token from help, assert that the doc mentions it.
    // We look for the raw token (e.g., "-prompt") to keep this simple and robust to formatting.
    for _, token := range flags {
        // The version line in help is "--version | -version". Ensure both variants are present in docs.
        if token == "--version" {
            if !strings.Contains(doc, "--version") || !strings.Contains(doc, "-version") {
                t.Fatalf("docs missing one of version tokens: --version or -version; flags=%v", flags)
            }
            continue
        }
        // Skip duplicate check for -version since it is covered by the --version case.
        if token == "-version" {
            continue
        }
        if !strings.Contains(doc, token) {
            t.Fatalf("docs/reference/cli-reference.md missing flag token %q from help; help line present, doc needs update", token)
        }
    }
}
