package main_test

import (
    "bytes"
    "encoding/json"
    "os/exec"
    "sort"
    "strings"
    "testing"

    testutil "github.com/hyperifyio/goagent/tools/testutil"
)

type group struct {
    RepresentativeID string   `json:"representative_id"`
    Members          []string `json:"members"`
    Score            float64  `json:"score"`
}

type output struct {
    Groups []group `json:"groups"`
}

func runTool(t *testing.T, bin string, input any) (output, string, error) {
    t.Helper()
    var out output
    data, err := json.Marshal(input)
    if err != nil {
        t.Fatalf("marshal: %v", err)
    }
    cmd := exec.Command(bin)
    cmd.Stdin = bytes.NewReader(data)
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr
    err = cmd.Run()
    if err == nil {
        if decErr := json.Unmarshal([]byte(strings.TrimSpace(stdout.String())), &out); decErr != nil {
            t.Fatalf("parse output: %v; raw=%s", decErr, stdout.String())
        }
    }
    return out, strings.TrimSpace(stderr.String()), err
}

// TestDedupeRank_GroupsNearDuplicates encodes the expected behavior:
// - Near-duplicate documents should be grouped together under one representative id
// - The representative is the best-ranked member; tie-breaks use TF-IDF-like signal
// This test is intentionally added before the implementation and should fail until implemented.
func TestDedupeRank_GroupsNearDuplicates(t *testing.T) {
    bin := testutil.BuildTool(t, "dedupe_rank")

    docs := []map[string]any{
        {"id": "a", "title": "Go Programming Language", "text": "Golang is a programming language created at Google."},
        {"id": "b", "title": "The Go Language", "text": "Go is a programming language by Google."},
        {"id": "c", "title": "Python Info", "text": "Python is a different programming language."},
    }

    in := map[string]any{"docs": docs}
    out, errStr, err := runTool(t, bin, in)
    if err != nil {
        t.Fatalf("dedupe_rank errored: %v, stderr=%s", err, errStr)
    }
    if len(out.Groups) == 0 {
        t.Fatalf("expected at least one group, got none")
    }
    // find group containing both a and b
    var ab []string
    for _, g := range out.Groups {
        hasA := false
        hasB := false
        for _, id := range g.Members {
            if id == "a" {
                hasA = true
            } else if id == "b" {
                hasB = true
            }
        }
        if hasA && hasB {
            ab = append([]string{}, g.Members...)
            // order members for deterministic comparison in golden-style tests
            sort.Strings(ab)
            break
        }
    }
    if len(ab) == 0 {
        t.Fatalf("expected docs 'a' and 'b' to be grouped together; groups=%v", out.Groups)
    }
}
