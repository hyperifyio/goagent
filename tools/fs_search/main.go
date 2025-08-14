package main

import (
    "bufio"
    "encoding/json"
    "fmt"
    "os"
)

// S02 skeleton: parse input only, no search yet

type input struct {
    Query      string   `json:"query"`
    Regex      bool     `json:"regex"`
    Globs      []string `json:"globs"`
    MaxResults int      `json:"maxResults"`
}

type match struct {
    Path    string `json:"path"`
    Line    int    `json:"line"`
    Col     int    `json:"col"`
    Preview string `json:"preview"`
}

type output struct {
    Matches   []match `json:"matches"`
    Truncated bool    `json:"truncated"`
}

func main() {
    // Read all stdin
    reader := bufio.NewReader(os.Stdin)
    var in input
    dec := json.NewDecoder(reader)
    if err := dec.Decode(&in); err != nil {
        fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", "invalid JSON input")
        os.Exit(2)
    }
    if in.Query == "" {
        fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", "query is required")
        os.Exit(2)
    }
    // Skeleton: no-op search; emit empty result so test will fail until impl
    out := output{Matches: []match{}, Truncated: false}
    enc := json.NewEncoder(os.Stdout)
    enc.SetEscapeHTML(false)
    if err := enc.Encode(out); err != nil {
        // best-effort
        fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", "encode error")
        os.Exit(1)
    }
}
