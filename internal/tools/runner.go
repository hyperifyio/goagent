package tools

import (
    "context"
    "errors"
    "fmt"
    "os"
    "os/exec"
    "time"
)

// RunToolWithJSON executes the tool command with args JSON provided on stdin.
// Returns stdout bytes and an error if the command fails. The caller is responsible
// for mapping errors to deterministic JSON per product rules.
// timeNow is a package-level clock to enable deterministic tests.
// In production it defaults to time.Now.
var timeNow = time.Now

// computeToolTimeout derives the timeout for a tool execution, honoring
// spec.TimeoutSec when provided; otherwise it falls back to the default.
func computeToolTimeout(spec ToolSpec, defaultTimeout time.Duration) time.Duration {
	if spec.TimeoutSec > 0 {
		return time.Duration(spec.TimeoutSec) * time.Second
	}
	return defaultTimeout
}

// buildToolEnvironment constructs a minimal environment for the tool process
// and returns the environment slice along with the list of env keys that were
// passed through (for audit visibility).
func buildToolEnvironment(spec ToolSpec) (env []string, passedKeys []string) {
	if v := os.Getenv("PATH"); v != "" {
		env = append(env, "PATH="+v)
	}
	if v := os.Getenv("HOME"); v != "" {
		env = append(env, "HOME="+v)
	}
	if len(spec.EnvPassthrough) > 0 {
		for _, key := range spec.EnvPassthrough {
			if val, ok := os.LookupEnv(key); ok {
				env = append(env, key+"="+val)
				passedKeys = append(passedKeys, key)
			}
		}
	}
	return env, passedKeys
}

// normalizeWaitError maps timeout and process errors to deterministic errors.
func normalizeWaitError(ctx context.Context, waitErr error, stderrText string) error {
	if ctx.Err() == context.DeadlineExceeded {
		return errors.New("tool timed out")
	}
	if waitErr != nil {
		msg := stderrText
		if msg == "" {
			msg = waitErr.Error()
		}
		return errors.New(msg)
	}
	return nil
}

func RunToolWithJSON(parentCtx context.Context, spec ToolSpec, jsonInput []byte, defaultTimeout time.Duration) ([]byte, error) {
	start := time.Now()
	// Derive timeout, honoring per-tool override when provided.
	to := computeToolTimeout(spec, defaultTimeout)
	ctx, cancel := context.WithTimeout(parentCtx, to)
	defer cancel()

	cmd := exec.CommandContext(ctx, spec.Command[0], spec.Command[1:]...)
	// Build minimal environment and record passed-through keys for audit.
	env, passedKeys := buildToolEnvironment(spec)
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	// Write JSON to stdin
	if len(jsonInput) == 0 {
		jsonInput = []byte("{}")
	}
	if _, err := stdin.Write(jsonInput); err != nil {
		return nil, fmt.Errorf("write stdin: %w", err)
	}
	// Best-effort close; log failure to audit but do not fail run
	if err := stdin.Close(); err != nil {
		// Capture the close error as a best-effort audit line
		if err2 := appendAuditLog(map[string]any{
			"ts":    timeNow().UTC().Format(time.RFC3339Nano),
			"event": "stdin_close_error",
			"tool":  spec.Name,
			"error": err.Error(),
		}); err2 != nil {
			_ = err2
		}
	}

    // Read stdout and stderr fully
    outCh := make(chan []byte, 1)
    errCh := make(chan []byte, 1)
    go func() { outCh <- safeReadAll(stdout) }()
    go func() { errCh <- safeReadAll(stderr) }()

	err = cmd.Wait()
	out := <-outCh
	serr := <-errCh

	exitCode := 0
	if err != nil {
		// Try to capture exit code when available
		if ee, ok := err.(*exec.ExitError); ok && ee.ProcessState != nil {
			exitCode = ee.ProcessState.ExitCode()
		} else {
			// Unknown exit (e.g., timeout/cancel)
			exitCode = -1
		}
	}
    // Best-effort audit (failures do not affect tool result)
    writeAudit(spec, start, exitCode, len(out), len(serr), passedKeys)

	if normErr := normalizeWaitError(ctx, err, string(serr)); normErr != nil {
		return nil, normErr
	}
	return out, nil
}
