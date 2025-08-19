package jsrun

import (
    "encoding/json"
    "testing"
)

func TestRun_EmitReadInput_Succeeds(t *testing.T) {
    req := map[string]any{
        "source": "emit(read_input())",
        "input":  "hello",
        "limits": map[string]any{"output_kb": 4},
    }
    b, _ := json.Marshal(req)
    stdout, stderr, err := Run(b)
    if err != nil || len(stderr) != 0 {
        t.Fatalf("unexpected error: %v stderr=%s", err, string(stderr))
    }
    var out struct{ Output string `json:"output"` }
    if e := json.Unmarshal(stdout, &out); e != nil {
        t.Fatalf("bad json: %v", e)
    }
    if out.Output != "hello" {
        t.Fatalf("got %q want %q", out.Output, "hello")
    }
}

func TestRun_OutputLimit_TruncatesAndErrors(t *testing.T) {
    // Create input larger than 1 KiB
    big := make([]byte, 1500)
    for i := range big {
        big[i] = 'a'
    }
    req := map[string]any{
        "source": "emit(read_input())",
        "input":  string(big),
        "limits": map[string]any{"output_kb": 1},
    }
    b, _ := json.Marshal(req)
    stdout, stderr, err := Run(b)
    if err == nil {
        t.Fatalf("expected error on output limit")
    }
    if string(stderr) == "" || !json.Valid(stderr) {
        t.Fatalf("expected structured stderr json, got: %s", string(stderr))
    }
    var out struct{ Output string `json:"output"` }
    if e := json.Unmarshal(stdout, &out); e != nil {
        t.Fatalf("bad stdout json: %v", e)
    }
    if len(out.Output) != 1024 {
        t.Fatalf("expected truncated to 1024 bytes, got %d", len(out.Output))
    }
}
