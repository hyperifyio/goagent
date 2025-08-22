package main

import (
    "encoding/json"
    "fmt"
    "io"
    "os"
    "sort"
)

// printCapabilities prints a human-readable summary of enabled tools based on the
// provided tools manifest path. Output is stable and testable:
// - When no manifest is provided or found, prints a friendly message.
// - When a manifest is present, lists tools sorted by name with description.
// - For img_create, an explicit warning is appended.
func printCapabilities(cfg cliConfig, stdout io.Writer, _ io.Writer) int {
    type toolEntry struct {
        Name        string `json:"name"`
        Description string `json:"description"`
    }
    type manifest struct {
        Tools []toolEntry `json:"tools"`
    }

    // Header to make intent clear in CLI output
    _, _ = io.WriteString(stdout, "Capabilities (enabled tools):\n")

    if cfg.toolsPath == "" || !fileExists(cfg.toolsPath) {
        _, _ = io.WriteString(stdout, "No tools enabled\n")
        return 0
    }

    // Read and parse manifest
    data, err := os.ReadFile(cfg.toolsPath)
    if err != nil {
        // Fall back to minimal notice; keep CLI resilient
        _, _ = io.WriteString(stdout, "No tools enabled\n")
        return 0
    }
    var m manifest
    if err := json.Unmarshal(data, &m); err != nil {
        _, _ = io.WriteString(stdout, "No tools enabled\n")
        return 0
    }

    if len(m.Tools) == 0 {
        _, _ = io.WriteString(stdout, "No tools enabled\n")
        return 0
    }

    // Sort tools by name for deterministic output
    sort.Slice(m.Tools, func(i, j int) bool { return m.Tools[i].Name < m.Tools[j].Name })

    for _, t := range m.Tools {
        line := fmt.Sprintf("- %s: %s", t.Name, t.Description)
        if t.Name == "img_create" {
            line += " [WARNING: makes outbound network calls and can save files]"
        }
        _, _ = io.WriteString(stdout, line+"\n")
    }
    return 0
}

func fileExists(p string) bool {
    if p == "" {
        return false
    }
    if _, err := os.Stat(p); err == nil {
        return true
    }
    return false
}
