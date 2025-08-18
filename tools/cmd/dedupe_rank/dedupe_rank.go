package main

import (
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "os"
)

type inputDocument struct {
    ID          string `json:"id"`
    URL         string `json:"url,omitempty"`
    Title       string `json:"title,omitempty"`
    Text        string `json:"text,omitempty"`
    PublishedAt string `json:"published_at,omitempty"`
}

type toolInput struct {
    Documents []inputDocument `json:"docs"`
}

type outputGroup struct {
    RepresentativeID string   `json:"representative_id"`
    Members          []string `json:"members"`
    Score            float64  `json:"score"`
}

type toolOutput struct {
    Groups []outputGroup `json:"groups"`
}

type stderrError struct {
    Error string `json:"error"`
    Hint  string `json:"hint,omitempty"`
}

func writeErrorAndExit(err error, hint string) {
    _ = json.NewEncoder(os.Stderr).Encode(stderrError{Error: err.Error(), Hint: hint})
    os.Exit(1)
}

func main() {
    data, err := io.ReadAll(os.Stdin)
    if err != nil {
        writeErrorAndExit(err, "failed to read stdin")
        return
    }
    var in toolInput
    if err := json.Unmarshal(data, &in); err != nil {
        writeErrorAndExit(err, "invalid JSON input for dedupe_rank")
        return
    }
    if len(in.Documents) == 0 {
        writeErrorAndExit(errors.New("missing docs"), "provide docs: [{id,title?,text?,url?,published_at?}]")
        return
    }

    // Stub implementation: produce an empty grouping result deterministically.
    out := toolOutput{Groups: []outputGroup{}}
    if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
        fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", "failed to encode output")
        os.Exit(1)
    }
}
