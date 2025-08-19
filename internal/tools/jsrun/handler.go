package jsrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dop251/goja"
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

var (
	errOutputLimit = errors.New("OUTPUT_LIMIT")
	errTimeout     = errors.New("TIMEOUT")
)

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

	// Prepare bounded output buffer
	var outBuf bytes.Buffer

	// Build a Goja VM with minimal bindings
	vm := goja.New()

	// Helper to write to bounded buffer and signal limit
	writeAndMaybeLimit := func(s string) error {
		writeBounded(&outBuf, s, capBytes)
		if outBuf.Len() >= capBytes && len(s) > capBytes {
			return errOutputLimit
		}
		return nil
	}

	// Bind read_input(): returns provided input string
	if err := vm.Set("read_input", func() string { return in.Input }); err != nil {
		return nil, mustMarshalError("EVAL_ERROR", "failed to bind read_input"), err
	}

	// Bind emit(s): appends to bounded buffer
	if err := vm.Set("emit", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) > 0 {
			arg := call.Arguments[0].String()
			if e := writeAndMaybeLimit(arg); e != nil {
				// Trigger a JS exception that we map after execution
				panic(errOutputLimit)
			}
		}
		return goja.Undefined()
	}); err != nil {
		return nil, mustMarshalError("EVAL_ERROR", "failed to bind emit"), err
	}

	// Timeout handling with interrupt
	wall := in.Limits.WallMS
	if wall <= 0 {
		wall = 1000 // 1s default
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(wall)*time.Millisecond)
	defer cancel()

	// Arrange to interrupt VM on timeout
	done := make(chan struct{})
	var runErr error
	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				// Propagate as error for classification below
				if errVal, ok := r.(error); ok {
					runErr = errVal
				} else {
					runErr = fmt.Errorf("panic: %v", r)
				}
			}
		}()
		_, runErr = vm.RunString(in.Source)
	}()

	select {
	case <-done:
		// Completed or panicked; classify below
	case <-ctx.Done():
		vm.Interrupt("timeout")
		<-done
		runErr = errTimeout
	}

	// Classify results
	if runErr != nil {
		switch runErr {
		case errOutputLimit:
			outJSON, mErr := json.Marshal(Output{Output: outBuf.String()})
			if mErr != nil {
				return nil, mustMarshalError("EVAL_ERROR", mErr.Error()), mErr
			}
			return outJSON, mustMarshalError("OUTPUT_LIMIT", fmt.Sprintf("output exceeded %d KB", maxKB)), errOutputLimit
		case errTimeout:
			return nil, mustMarshalError("TIMEOUT", fmt.Sprintf("execution exceeded %d ms", wall)), errTimeout
		default:
			return nil, mustMarshalError("EVAL_ERROR", runErr.Error()), runErr
		}
	}

	outJSON, mErr := json.Marshal(Output{Output: outBuf.String()})
	if mErr != nil {
		return nil, mustMarshalError("EVAL_ERROR", mErr.Error()), mErr
	}
	return outJSON, nil, nil
}

func mustMarshalError(code, msg string) []byte {
	b, err := json.Marshal(Error{Code: code, Message: msg})
	if err != nil {
		// Fallback minimal JSON to avoid panics in error paths
		return []byte(`{"code":"` + code + `","message":"` + msg + `"}`)
	}
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
