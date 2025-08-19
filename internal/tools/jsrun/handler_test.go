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
	b, merr := json.Marshal(req)
	if merr != nil {
		t.Fatalf("marshal: %v", merr)
	}
	stdout, stderr, err := Run(b)
	if err != nil || len(stderr) != 0 {
		t.Fatalf("unexpected error: %v stderr=%s", err, string(stderr))
	}
	var out struct {
		Output string `json:"output"`
	}
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
	b, merr := json.Marshal(req)
	if merr != nil {
		t.Fatalf("marshal: %v", merr)
	}
	stdout, stderr, err := Run(b)
	if err == nil {
		t.Fatalf("expected error on output limit")
	}
	if string(stderr) == "" || !json.Valid(stderr) {
		t.Fatalf("expected structured stderr json, got: %s", string(stderr))
	}
	var errObj struct{ Code, Message string }
	if uerr := json.Unmarshal(stderr, &errObj); uerr != nil {
		t.Fatalf("stderr not JSON: %v: %s", uerr, string(stderr))
	}
	if errObj.Code != "OUTPUT_LIMIT" {
		t.Fatalf("expected OUTPUT_LIMIT code, got %q (%s)", errObj.Code, errObj.Message)
	}
	var out struct {
		Output string `json:"output"`
	}
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
	b, merr := json.Marshal(req)
	if merr != nil {
		t.Fatalf("marshal: %v", merr)
	}
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

func TestRun_EvalError_ThrownError(t *testing.T) {
	req := map[string]any{
		"source": "throw new Error('boom')",
		"input":  "",
		"limits": map[string]any{"output_kb": 1},
	}
	b, merr := json.Marshal(req)
	if merr != nil {
		t.Fatalf("marshal: %v", merr)
	}
	stdout, stderr, err := Run(b)
	if err == nil {
		t.Fatalf("expected EVAL_ERROR")
	}
	if len(stdout) != 0 {
		t.Fatalf("expected no stdout on eval error, got: %s", string(stdout))
	}
	var e struct{ Code, Message string }
	if jerr := json.Unmarshal(stderr, &e); jerr != nil {
		t.Fatalf("stderr not JSON: %v: %s", jerr, string(stderr))
	}
	if e.Code != "EVAL_ERROR" {
		t.Fatalf("expected EVAL_ERROR code, got %q (%s)", e.Code, e.Message)
	}
	if e.Message == "" {
		t.Fatalf("expected non-empty error message")
	}
}

func TestRun_EvalError_ReferenceError(t *testing.T) {
	// Referencing an undefined symbol should raise an evaluation error
	req := map[string]any{
		"source": "emit(does_not_exist)",
		"input":  "",
		"limits": map[string]any{"output_kb": 1},
	}
	b, merr := json.Marshal(req)
	if merr != nil {
		t.Fatalf("marshal: %v", merr)
	}
	stdout, stderr, err := Run(b)
	if err == nil {
		t.Fatalf("expected EVAL_ERROR for reference error")
	}
	if len(stdout) != 0 {
		t.Fatalf("expected no stdout on eval error, got: %s", string(stdout))
	}
	var e struct{ Code, Message string }
	if jerr := json.Unmarshal(stderr, &e); jerr != nil {
		t.Fatalf("stderr not JSON: %v: %s", jerr, string(stderr))
	}
	if e.Code != "EVAL_ERROR" {
		t.Fatalf("expected EVAL_ERROR code, got %q (%s)", e.Code, e.Message)
	}
}

func TestRun_DenyByDefault_UndefinedGlobals(t *testing.T) {
	// Verify that require/console are not bound and evaluate to undefined via typeof
	req := map[string]any{
		"source": "emit(typeof require + '|' + typeof console)",
		"input":  "",
		"limits": map[string]any{"output_kb": 1},
	}
	b, merr := json.Marshal(req)
	if merr != nil {
		t.Fatalf("marshal: %v", merr)
	}
	stdout, stderr, err := Run(b)
	if err != nil || len(stderr) != 0 {
		t.Fatalf("unexpected error: %v stderr=%s", err, string(stderr))
	}
	var out struct {
		Output string `json:"output"`
	}
	if e := json.Unmarshal(stdout, &out); e != nil {
		t.Fatalf("bad json: %v", e)
	}
	if out.Output != "undefined|undefined" {
		t.Fatalf("got %q want %q", out.Output, "undefined|undefined")
	}
}

func TestRun_DenyByDefault_UndefinedTimers(t *testing.T) {
	// Timers like setTimeout must not exist unless explicitly bound
	req := map[string]any{
		"source": "emit(typeof setTimeout + '|' + typeof setInterval)",
		"input":  "",
		"limits": map[string]any{"output_kb": 1},
	}
	b, merr := json.Marshal(req)
	if merr != nil {
		t.Fatalf("marshal: %v", merr)
	}
	stdout, stderr, err := Run(b)
	if err != nil || len(stderr) != 0 {
		t.Fatalf("unexpected error: %v stderr=%s", err, string(stderr))
	}
	var out struct {
		Output string `json:"output"`
	}
	if e := json.Unmarshal(stdout, &out); e != nil {
		t.Fatalf("bad json: %v", e)
	}
	if out.Output != "undefined|undefined" {
		t.Fatalf("got %q want %q", out.Output, "undefined|undefined")
	}
}
