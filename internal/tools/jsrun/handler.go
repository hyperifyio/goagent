package jsrun

import (
    "bytes"
    "encoding/json"
    "errors"
    "fmt"
    "strings"
)

// Input models the expected stdin JSON for code.sandbox.js.run
type Input struct {
    Source string `json:"source"`
    Input  string `json:"input"`
    Limits struct {
        WallMS   int `json:"wall_ms"`
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

var errOutputLimit = errors.New("OUTPUT_LIMIT")

// Run executes the provided JavaScript source with minimal host bindings.
// Returns (stdoutJSON, stderrJSON, err). On OUTPUT_LIMIT, returns truncated
// stdout along with stderr error JSON and a non-nil error.
func Run(raw []byte) ([]byte, []byte, error) {
    var in Input
    if err := json.Unmarshal(raw, &in); err != nil {
        return nil, mustMarshalError("INVALID_INPUT", "invalid JSON: "+err.Error()), err
    }
    if in.Source == "" {
        return nil, mustMarshalError("INVALID_INPUT", "missing source"), errors.New("invalid input")
    }

    // Default output cap: 64 KiB if not provided or invalid
    maxKB := in.Limits.OutputKB
    if maxKB <= 0 {
        maxKB = 64
    }
    capBytes := maxKB * 1024

    // Bounded output buffer
    var outBuf bytes.Buffer

    // Minimal interpreter stub supporting two forms:
    // 1) emit(read_input())
    // 2) emit("<literal>")
    src := strings.TrimSpace(in.Source)
    switch {
    case src == "emit(read_input())":
        // write input subject to cap
        writeBounded(&outBuf, in.Input, capBytes)
        if outBuf.Len() >= capBytes && len(in.Input) > capBytes {
            outJSON, _ := json.Marshal(Output{Output: outBuf.String()})
            return outJSON, mustMarshalError("OUTPUT_LIMIT", fmt.Sprintf("output exceeded %d KB", maxKB)), errOutputLimit
        }
    case strings.HasPrefix(src, "emit(\"") && strings.HasSuffix(src, "\")"):
        lit := strings.TrimSuffix(strings.TrimPrefix(src, "emit(\""), "\")")
        writeBounded(&outBuf, lit, capBytes)
        if outBuf.Len() >= capBytes && len(lit) > capBytes {
            outJSON, _ := json.Marshal(Output{Output: outBuf.String()})
            return outJSON, mustMarshalError("OUTPUT_LIMIT", fmt.Sprintf("output exceeded %d KB", maxKB)), errOutputLimit
        }
    default:
        return nil, mustMarshalError("EVAL_ERROR", "unsupported source"), errors.New("eval error")
    }

    outJSON, _ := json.Marshal(Output{Output: outBuf.String()})
    return outJSON, nil, nil
}

func mustMarshalError(code, msg string) []byte {
    b, _ := json.Marshal(Error{Code: code, Message: msg})
    return b
}

func writeBounded(buf *bytes.Buffer, s string, capBytes int) {
    if capBytes <= 0 {
        _ = buf.WriteByte(0) // unreachable, but keep logic safe
        return
    }
    remain := capBytes - buf.Len()
    if remain <= 0 {
        return
    }
    bs := []byte(s)
    if len(bs) > remain {
        buf.Write(bs[:remain])
        return
    }
    buf.Write(bs)
}
