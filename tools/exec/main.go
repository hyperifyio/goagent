package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type execInput struct {
	Cmd        string            `json:"cmd"`
	Args       []string          `json:"args"`
	Cwd        string            `json:"cwd"`
	Env        map[string]string `json:"env"`
	Stdin      string            `json:"stdin"`
	TimeoutSec int               `json:"timeoutSec"`
}

type execOutput struct {
	ExitCode   int    `json:"exitCode"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"durationMs"`
}

func main() {
	start := time.Now()
	out := execOutput{}

	inBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		finishAndPrint(&out, start, 1, "", fmt.Sprintf("read stdin: %s", sanitize(err.Error())))
		return
	}
	if len(bytes.TrimSpace(inBytes)) == 0 {
		inBytes = []byte("{}")
	}
	var in execInput
	if err := json.Unmarshal(inBytes, &in); err != nil {
		finishAndPrint(&out, start, 1, "", fmt.Sprintf("bad json: %s", sanitize(err.Error())))
		return
	}
	if strings.TrimSpace(in.Cmd) == "" {
		finishAndPrint(&out, start, 1, "", "missing cmd")
		return
	}

	ctx := context.Background()
	if in.TimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(in.TimeoutSec)*time.Second)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, in.Cmd, in.Args...)
	if strings.TrimSpace(in.Cwd) != "" {
		cmd.Dir = in.Cwd
	}
	// Build environment from provided map only (deterministic, minimal)
	if len(in.Env) > 0 {
		pairs := make([]string, 0, len(in.Env))
		for k, v := range in.Env {
			pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
		}
		sort.Strings(pairs)
		cmd.Env = pairs
	}

	if in.Stdin != "" {
		cmd.Stdin = strings.NewReader(in.Stdin)
	}
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err = cmd.Run()
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = 1
		}
		// If context deadline exceeded, annotate stderr to include "timeout"
		if ctx.Err() == context.DeadlineExceeded {
			if !strings.Contains(strings.ToLower(stderrBuf.String()), "timeout") {
				if stderrBuf.Len() > 0 {
					stderrBuf.WriteString("\n")
				}
				stderrBuf.WriteString("timeout")
			}
		}
	}

	finishAndPrint(&out, start, exitCode, stdoutBuf.String(), stderrBuf.String())
}

func finishAndPrint(out *execOutput, start time.Time, exit int, stdout, stderr string) {
	out.ExitCode = exit
	out.Stdout = stdout
	out.Stderr = stderr
	out.DurationMs = time.Since(start).Milliseconds()
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
}

func sanitize(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
