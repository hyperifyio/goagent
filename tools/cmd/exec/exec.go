package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type execInput struct {
	Cmd        string            `json:"cmd"`
	Args       []string          `json:"args"`
	Cwd        string            `json:"cwd,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	Stdin      string            `json:"stdin,omitempty"`
	TimeoutSec int               `json:"timeoutSec,omitempty"`
}

type execOutput struct {
	ExitCode   int    `json:"exitCode"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"durationMs"`
}

func main() {
	in, err := readInput(os.Stdin)
	if err != nil {
        // Standardized error contract: write single-line JSON to stderr and exit non-zero
        msg := sanitizeError(err)
        fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", msg)
        os.Exit(1)
	}

	stdout, stderr, exitCode, dur := runCommand(in)
	writeOutput(execOutput{ExitCode: exitCode, Stdout: stdout, Stderr: stderr, DurationMs: dur})
}

func readInput(r io.Reader) (execInput, error) {
	var in execInput
	br := bufio.NewReader(r)
	data, err := io.ReadAll(br)
	if err != nil {
		return in, fmt.Errorf("read stdin: %w", err)
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return in, fmt.Errorf("parse json: %w", err)
	}
	if strings.TrimSpace(in.Cmd) == "" {
		return in, fmt.Errorf("cmd is required")
	}
	return in, nil
}

func runCommand(in execInput) (stdoutStr, stderrStr string, exitCode int, durationMs int64) {
	start := time.Now()
	ctx := context.Background()
	var cancel context.CancelFunc
	if in.TimeoutSec > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(in.TimeoutSec)*time.Second)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, in.Cmd, in.Args...)
	if strings.TrimSpace(in.Cwd) != "" {
		// Ensure cwd is clean and absolute if provided as relative
		if !filepath.IsAbs(in.Cwd) {
			abs, _ := filepath.Abs(in.Cwd)
			cmd.Dir = abs
		} else {
			cmd.Dir = in.Cwd
		}
	}
	// Start from current environment and apply overrides
	env := os.Environ()
	for k, v := range in.Env {
		if strings.Contains(k, "=") {
			// Skip invalid keys defensively
			continue
		}
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = env

	if in.Stdin != "" {
		cmd.Stdin = strings.NewReader(in.Stdin)
	}
	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	exitCode = 0
	err := cmd.Run()
	durationMs = time.Since(start).Milliseconds()

	stdoutStr = stdoutBuf.String()
	stderrStr = stderrBuf.String()

	if err == nil {
		return
	}
	// Determine exit code and normalize timeout message
	if ctxErr := ctx.Err(); ctxErr == context.DeadlineExceeded {
		// Timed out
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = 1
		}
		if !strings.Contains(strings.ToLower(stderrStr), "timeout") {
			if len(stderrStr) > 0 && !strings.HasSuffix(stderrStr, "\n") {
				stderrStr += "\n"
			}
			stderrStr += "timeout"
		}
		return
	}
	if ee, ok := err.(*exec.ExitError); ok {
		exitCode = ee.ExitCode()
	} else {
		exitCode = 1
	}
	return
}

func writeOutput(out execOutput) {
	enc, _ := json.Marshal(out)
	// Single line JSON
	fmt.Println(string(enc))
}

func sanitizeError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	// Collapse newlines to keep single-line contract
	msg = strings.ReplaceAll(msg, "\n", " ")
	return msg
}
