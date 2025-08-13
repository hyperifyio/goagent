package execcli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
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

type execResult struct {
	ExitCode   int    `json:"exitCode"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"durationMs"`
}

func readAllStdin() ([]byte, error) {
	reader := bufio.NewReader(os.Stdin)
	var sb strings.Builder
	for {
		chunk, isPrefix, err := reader.ReadLine()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		sb.Write(chunk)
		if !isPrefix {
			// newline removed by ReadLine; keep JSON as single line
		}
	}
	return []byte(sb.String()), nil
}

func Main() {
	start := time.Now()
	var inp execInput
	data, err := readAllStdin()
	if err != nil {
		writeResultAndExit(start, -1, "", fmt.Sprintf("failed to read stdin: %v", err), 2)
	}
	if len(data) == 0 {
		writeResultAndExit(start, -1, "", "empty stdin", 2)
	}
	if err := json.Unmarshal(data, &inp); err != nil {
		writeResultAndExit(start, -1, "", fmt.Sprintf("invalid JSON: %v", err), 2)
	}
	if strings.TrimSpace(inp.Cmd) == "" {
		writeResultAndExit(start, -1, "", "missing cmd", 2)
	}

	ctx := context.Background()
	var cancel context.CancelFunc
	if inp.TimeoutSec > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(inp.TimeoutSec)*time.Second)
		defer cancel()
	}

	command := exec.CommandContext(ctx, inp.Cmd, inp.Args...)
	if inp.Cwd != "" {
		command.Dir = inp.Cwd
	}
	// inherit env then override with provided
	env := []string{}
	for _, e := range os.Environ() {
		env = append(env, e)
	}
	if len(inp.Env) > 0 {
		lookup := func(key string) int {
			prefix := key + "="
			for i, e := range env {
				if strings.HasPrefix(e, prefix) {
					return i
				}
			}
			return -1
		}
		for k, v := range inp.Env {
			if idx := lookup(k); idx >= 0 {
				env[idx] = k + "=" + v
			} else {
				env = append(env, k+"="+v)
			}
		}
	}
	command.Env = env

	if inp.Stdin != "" {
		command.Stdin = strings.NewReader(inp.Stdin)
	}
	var outBuf, errBuf strings.Builder
	command.Stdout = &outBuf
	command.Stderr = &errBuf

	execErr := command.Run()
	dur := time.Since(start)
	exitCode := 0
	stderr := errBuf.String()
	if execErr != nil {
		if ctxErr := ctx.Err(); ctxErr == context.DeadlineExceeded {
			stderr = appendMsg(stderr, "timeout")
		}
		if exitErr, ok := execErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	res := execResult{
		ExitCode:   exitCode,
		Stdout:     outBuf.String(),
		Stderr:     stderr,
		DurationMs: dur.Milliseconds(),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(res); err != nil {
		fmt.Fprintf(os.Stdout, "{\"exitCode\":%d,\"stdout\":\"\",\"stderr\":\"encode error\",\"durationMs\":%d}\n", exitCode, dur.Milliseconds())
	}
}

func writeResultAndExit(start time.Time, exitCode int, stdout, stderr string, processExit int) {
	res := execResult{
		ExitCode:   exitCode,
		Stdout:     stdout,
		Stderr:     stderr,
		DurationMs: time.Since(start).Milliseconds(),
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(res)
	os.Exit(processExit)
}

func appendMsg(base, add string) string {
	if base == "" {
		return add
	}
	return base + "; " + add
}
