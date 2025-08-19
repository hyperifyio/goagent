package wasmrun

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Input models the expected stdin JSON for code.sandbox.wasm.run
type Input struct {
	ModuleB64 string `json:"module_b64"`
	Entry     string `json:"entry"`
	Input     string `json:"input"`
	Limits    struct {
		WallMS   int `json:"wall_ms"`
		MemPages int `json:"mem_pages"`
		OutputKB int `json:"output_kb"`
	} `json:"limits"`
}

// Output is the successful stdout JSON shape
type Output struct {
	Output string `json:"output"`
}

// Error represents a structured error payload for stderr JSON
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

var (
	errInvalidInput = errors.New("INVALID_INPUT")
)

// Run parses input and (in future) executes the provided WebAssembly module.
// Returns (stdoutJSON, stderrJSON, err). For now, only input validation is implemented.
func Run(raw []byte) ([]byte, []byte, error) {
	start := time.Now()
	var in Input
	if err := json.Unmarshal(raw, &in); err != nil {
		// audit invalid input
		_ = appendAudit(map[string]any{ //nolint:errcheck // best-effort audit
			"ts":             time.Now().UTC().Format(time.RFC3339Nano),
			"tool":           "code.sandbox.wasm.run",
			"span":           "tools.wasm.run",
			"ms":             time.Since(start).Milliseconds(),
			"module_bytes":   0,
			"wall_ms":        0,
			"mem_pages_used": 0,
			"bytes_out":      0,
			"event":          "INVALID_INPUT",
		})
		return nil, mustMarshalError("INVALID_INPUT", "invalid JSON: "+err.Error()), errInvalidInput
	}
	if in.ModuleB64 == "" {
		_ = appendAudit(map[string]any{ //nolint:errcheck // best-effort audit
			"ts":             time.Now().UTC().Format(time.RFC3339Nano),
			"tool":           "code.sandbox.wasm.run",
			"span":           "tools.wasm.run",
			"ms":             time.Since(start).Milliseconds(),
			"module_bytes":   0,
			"wall_ms":        in.Limits.WallMS,
			"mem_pages_used": 0,
			"bytes_out":      0,
			"event":          "INVALID_INPUT",
		})
		return nil, mustMarshalError("INVALID_INPUT", "missing module_b64"), errInvalidInput
	}
	// Validate base64 early to surface errors deterministically
	modBytes, err := base64.StdEncoding.DecodeString(in.ModuleB64)
	if err != nil {
		_ = appendAudit(map[string]any{ //nolint:errcheck // best-effort audit
			"ts":             time.Now().UTC().Format(time.RFC3339Nano),
			"tool":           "code.sandbox.wasm.run",
			"span":           "tools.wasm.run",
			"ms":             time.Since(start).Milliseconds(),
			"module_bytes":   0,
			"wall_ms":        in.Limits.WallMS,
			"mem_pages_used": 0,
			"bytes_out":      0,
			"event":          "INVALID_INPUT",
		})
		return nil, mustMarshalError("INVALID_INPUT", "module_b64 is not valid base64: "+err.Error()), errInvalidInput
	}
	// Validate limits
	if in.Limits.OutputKB <= 0 {
		_ = appendAudit(map[string]any{ //nolint:errcheck // best-effort audit
			"ts":             time.Now().UTC().Format(time.RFC3339Nano),
			"tool":           "code.sandbox.wasm.run",
			"span":           "tools.wasm.run",
			"ms":             time.Since(start).Milliseconds(),
			"module_bytes":   len(modBytes),
			"wall_ms":        in.Limits.WallMS,
			"mem_pages_used": 0,
			"bytes_out":      0,
			"event":          "INVALID_INPUT",
		})
		return nil, mustMarshalError("INVALID_INPUT", "limits.output_kb must be > 0"), errInvalidInput
	}
	if in.Limits.WallMS <= 0 {
		_ = appendAudit(map[string]any{ //nolint:errcheck // best-effort audit
			"ts":             time.Now().UTC().Format(time.RFC3339Nano),
			"tool":           "code.sandbox.wasm.run",
			"span":           "tools.wasm.run",
			"ms":             time.Since(start).Milliseconds(),
			"module_bytes":   len(modBytes),
			"wall_ms":        in.Limits.WallMS,
			"mem_pages_used": 0,
			"bytes_out":      0,
			"event":          "INVALID_INPUT",
		})
		return nil, mustMarshalError("INVALID_INPUT", "limits.wall_ms must be > 0"), errInvalidInput
	}
	if in.Limits.MemPages <= 0 {
		_ = appendAudit(map[string]any{ //nolint:errcheck // best-effort audit
			"ts":             time.Now().UTC().Format(time.RFC3339Nano),
			"tool":           "code.sandbox.wasm.run",
			"span":           "tools.wasm.run",
			"ms":             time.Since(start).Milliseconds(),
			"module_bytes":   len(modBytes),
			"wall_ms":        in.Limits.WallMS,
			"mem_pages_used": 0,
			"bytes_out":      0,
			"event":          "INVALID_INPUT",
		})
		return nil, mustMarshalError("INVALID_INPUT", "limits.mem_pages must be > 0"), errInvalidInput
	}

	// Deny WASI by default: detect imports of wasi_snapshot_preview1 and fail fast.
	// This is a conservative check prior to implementing full wasm execution.
	if bytes.Contains(modBytes, []byte("wasi_snapshot_preview1")) {
		_ = appendAudit(map[string]any{ //nolint:errcheck // best-effort audit
			"ts":             time.Now().UTC().Format(time.RFC3339Nano),
			"tool":           "code.sandbox.wasm.run",
			"span":           "tools.wasm.run",
			"ms":             time.Since(start).Milliseconds(),
			"module_bytes":   len(modBytes),
			"wall_ms":        in.Limits.WallMS,
			"mem_pages_used": 0,
			"bytes_out":      0,
			"event":          "MISSING_IMPORT",
		})
		return nil, mustMarshalError("MISSING_IMPORT", "WASI is not available by default; modules requiring 'wasi_snapshot_preview1' are unsupported"), errors.New("missing import: wasi_snapshot_preview1")
	}

	// Not yet implemented: actual wasm execution. Return a stable stub error.
	_ = appendAudit(map[string]any{ //nolint:errcheck // best-effort audit
		"ts":             time.Now().UTC().Format(time.RFC3339Nano),
		"tool":           "code.sandbox.wasm.run",
		"span":           "tools.wasm.run",
		"ms":             time.Since(start).Milliseconds(),
		"module_bytes":   len(modBytes),
		"wall_ms":        in.Limits.WallMS,
		"mem_pages_used": 0,
		"bytes_out":      0,
		"event":          "UNIMPLEMENTED",
	})
	return nil, mustMarshalError("UNIMPLEMENTED", "wasm execution not yet implemented"), errors.New("unimplemented")
}

func mustMarshalError(code, msg string) []byte {
	b, err := json.Marshal(Error{Code: code, Message: msg})
	if err != nil {
		return []byte("{\"code\":\"" + code + "\",\"message\":\"" + msg + "\"}")
	}
	return b
}

// appendAudit writes an NDJSON line under .goagent/audit/YYYYMMDD.log at the repo root.
func appendAudit(entry any) error {
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	root := moduleRoot()
	dir := filepath.Join(root, ".goagent", "audit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	fname := time.Now().UTC().Format("20060102") + ".log"
	path := filepath.Join(dir, fname)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // best-effort close
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// moduleRoot walks upward from CWD to the directory containing go.mod; falls back to CWD.
func moduleRoot() string {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		return "."
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd
		}
		dir = parent
	}
}
