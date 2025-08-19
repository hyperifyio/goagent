package wasmrun

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestRun_InvalidJSON(t *testing.T) {
	stdout, stderr, err := Run([]byte("not-json"))
	if err == nil {
		t.Fatalf("expected error for invalid JSON")
	}
	if len(stdout) != 0 {
		t.Fatalf("expected no stdout, got: %s", string(stdout))
	}
	var e struct{ Code, Message string }
	if jerr := json.Unmarshal(stderr, &e); jerr != nil {
		t.Fatalf("stderr not JSON: %v: %s", jerr, string(stderr))
	}
	if e.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %q (%s)", e.Code, e.Message)
	}
}

func TestRun_MissingModuleB64(t *testing.T) {
	req := map[string]any{
		"entry":  "main",
		"input":  "",
		"limits": map[string]any{"output_kb": 1, "wall_ms": 10, "mem_pages": 1},
	}
	b, merr := json.Marshal(req)
	if merr != nil {
		t.Fatalf("marshal failed: %v", merr)
	}
	stdout, stderr, err := Run(b)
	if err == nil {
		t.Fatalf("expected error for missing module_b64")
	}
	if len(stdout) != 0 {
		t.Fatalf("expected no stdout, got: %s", string(stdout))
	}
	var e struct{ Code, Message string }
	if jerr := json.Unmarshal(stderr, &e); jerr != nil {
		t.Fatalf("stderr not JSON: %v: %s", jerr, string(stderr))
	}
	if e.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %q (%s)", e.Code, e.Message)
	}
}

func TestRun_BadBase64(t *testing.T) {
	req := map[string]any{
		"module_b64": "!!!not-base64!!!",
		"entry":      "main",
		"input":      "",
		"limits":     map[string]any{"output_kb": 1, "wall_ms": 10, "mem_pages": 1},
	}
	b, merr := json.Marshal(req)
	if merr != nil {
		t.Fatalf("marshal failed: %v", merr)
	}
	stdout, stderr, err := Run(b)
	if err == nil {
		t.Fatalf("expected error for invalid base64")
	}
	if len(stdout) != 0 {
		t.Fatalf("expected no stdout, got: %s", string(stdout))
	}
	var e struct{ Code, Message string }
	if jerr := json.Unmarshal(stderr, &e); jerr != nil {
		t.Fatalf("stderr not JSON: %v: %s", jerr, string(stderr))
	}
	if e.Code != "INVALID_INPUT" {
		t.Fatalf("expected INVALID_INPUT, got %q (%s)", e.Code, e.Message)
	}
}

func TestRun_UnimplementedOnValidInput(t *testing.T) {
	// module_b64 is valid base64 but not necessarily a valid wasm; current stub only validates base64
	req := map[string]any{
		"module_b64": "AA==", // base64 for single zero byte
		"entry":      "main",
		"input":      "",
		"limits":     map[string]any{"output_kb": 1, "wall_ms": 10, "mem_pages": 1},
	}
	b, merr := json.Marshal(req)
	if merr != nil {
		t.Fatalf("marshal failed: %v", merr)
	}
	stdout, stderr, err := Run(b)
	if err == nil {
		t.Fatalf("expected unimplemented error")
	}
	if len(stdout) != 0 {
		t.Fatalf("expected no stdout, got: %s", string(stdout))
	}
	var e struct{ Code, Message string }
	if jerr := json.Unmarshal(stderr, &e); jerr != nil {
		t.Fatalf("stderr not JSON: %v: %s", jerr, string(stderr))
	}
	if e.Code != "UNIMPLEMENTED" {
		t.Fatalf("expected UNIMPLEMENTED, got %q (%s)", e.Code, e.Message)
	}
}

func TestRun_InvalidLimits(t *testing.T) {
	cases := []map[string]any{
		{"module_b64": "AA==", "entry": "main", "input": "", "limits": map[string]any{"output_kb": 0, "wall_ms": 10, "mem_pages": 1}},
		{"module_b64": "AA==", "entry": "main", "input": "", "limits": map[string]any{"output_kb": 1, "wall_ms": 0, "mem_pages": 1}},
		{"module_b64": "AA==", "entry": "main", "input": "", "limits": map[string]any{"output_kb": 1, "wall_ms": 10, "mem_pages": 0}},
	}
	for i, req := range cases {
		b, merr := json.Marshal(req)
		if merr != nil {
			t.Fatalf("case %d: marshal failed: %v", i, merr)
		}
		stdout, stderr, err := Run(b)
		if err == nil {
			t.Fatalf("case %d: expected error for invalid limits", i)
		}
		if len(stdout) != 0 {
			t.Fatalf("case %d: expected no stdout, got: %s", i, string(stdout))
		}
		var e struct{ Code, Message string }
		if jerr := json.Unmarshal(stderr, &e); jerr != nil {
			t.Fatalf("case %d: stderr not JSON: %v: %s", i, jerr, string(stderr))
		}
		if e.Code != "INVALID_INPUT" {
			t.Fatalf("case %d: expected INVALID_INPUT, got %q (%s)", i, e.Code, e.Message)
		}
	}
}

func TestRun_DenyWASIByDefault(t *testing.T) {
	// Any bytes containing the string "wasi_snapshot_preview1" should be denied
	// even before actual execution is implemented.
	wasmLike := base64.StdEncoding.EncodeToString([]byte("xxwasi_snapshot_preview1xx"))
	req := map[string]any{
		"module_b64": wasmLike,
		"entry":      "main",
		"input":      "",
		"limits":     map[string]any{"output_kb": 1, "wall_ms": 10, "mem_pages": 1},
	}
	b, merr := json.Marshal(req)
	if merr != nil {
		t.Fatalf("marshal failed: %v", merr)
	}
	stdout, stderr, err := Run(b)
	if err == nil {
		t.Fatalf("expected error for WASI-dependent module")
	}
	if len(stdout) != 0 {
		t.Fatalf("expected no stdout, got: %s", string(stdout))
	}
	var e struct{ Code, Message string }
	if jerr := json.Unmarshal(stderr, &e); jerr != nil {
		t.Fatalf("stderr not JSON: %v: %s", jerr, string(stderr))
	}
	if e.Code != "MISSING_IMPORT" {
		t.Fatalf("expected MISSING_IMPORT, got %q (%s)", e.Code, e.Message)
	}
}
