package wasmrun

import (
    "encoding/base64"
    "encoding/json"
    "errors"
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
    var in Input
    if err := json.Unmarshal(raw, &in); err != nil {
        return nil, mustMarshalError("INVALID_INPUT", "invalid JSON: "+err.Error()), errInvalidInput
    }
    if in.ModuleB64 == "" {
        return nil, mustMarshalError("INVALID_INPUT", "missing module_b64"), errInvalidInput
    }
    // Validate base64 early to surface errors deterministically
    if _, err := base64.StdEncoding.DecodeString(in.ModuleB64); err != nil {
        return nil, mustMarshalError("INVALID_INPUT", "module_b64 is not valid base64: "+err.Error()), errInvalidInput
    }

    // Not yet implemented: actual wasm execution. Return a stable stub error.
    return nil, mustMarshalError("UNIMPLEMENTED", "wasm execution not yet implemented"), errors.New("unimplemented")
}

func mustMarshalError(code, msg string) []byte {
    b, err := json.Marshal(Error{Code: code, Message: msg})
    if err != nil {
        return []byte("{\"code\":\"" + code + "\",\"message\":\"" + msg + "\"}")
    }
    return b
}
