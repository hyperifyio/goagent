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

func TestRun_Timeout_Interrupts(t *testing.T) {
    // Infinite loop; should be interrupted by wall_ms
    req := map[string]any{
        "source": "for(;;){}",
        "input":  "",
        "limits": map[string]any{"output_kb": 1, "wall_ms": 50},
    }
    b, _ := json.Marshal(req)
    stdout, stderr, err := Run(b)
    if err == nil {
        t.Fatalf("expected timeout error")
    }
    if len(stdout) != 0 {
        t.Fatalf("expected no stdout on timeout, got: %s", string(stdout))
    }
    var e struct{ Code, Message string }
    if jerr := json.Unmarshal(stderr, &e); jerr != nil {
        t.Fatalf("stderr not JSON: %v: %s", jerr, string(stderr))
    }
    if e.Code != "TIMEOUT" {
        t.Fatalf("expected TIMEOUT code, got %q (%s)", e.Code, e.Message)
    }
}
