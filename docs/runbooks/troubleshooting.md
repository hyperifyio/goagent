# Troubleshooting

This runbook covers common errors and deterministic fixes for `goagent`.

## Missing tool binaries
- Symptom: `exec: "./tools/<name>": file does not exist` or tool not found in `tools.json` command path.
- Fix:
```bash
# Build all tools
make build-tools
```
- Build a single tool from source (post‑migration layout):
```bash
# Unix/macOS
mkdir -p tools/bin
go build -o tools/bin/fs_read_file ./tools/cmd/fs_read_file

# Windows (PowerShell or cmd)
mkdir tools\bin 2> NUL
go build -o tools\bin\fs_read_file.exe .\tools\cmd\fs_read_file
```
- Verify:
```bash
# Unix/macOS
./tools/bin/fs_read_file -h 2>/dev/null || echo ok
ls -l tools/bin | grep -E 'exec|fs_'

# Windows
./tools/bin/fs_read_file.exe -h 2> NUL || echo ok
dir tools\\bin
```

## Repo-relative path violations
- Symptom: file tools return errors about paths outside repository or cannot find the path.
- Fix: paths passed to fs tools must be relative to repository root. `cd` to repo root and retry.
```bash
# From repo root (Unix/macOS)
echo '{"path":"README.md"}' | ./tools/bin/fs_read_file | jq .

# From repo root (Windows)
echo '{"path":"README.md"}' | ./tools/bin/fs_read_file.exe | jq .
```

## Tool timeouts
- Symptom: `{"error":"tool timed out"}` mapped by the runner or non-zero exit due to timeout.
- Fix: increase per-tool `timeoutSec` in `tools.json`, raise CLI `-tool-timeout` (preferred), or use a larger global `-timeout` (deprecated).

### HTTP request times out (`context deadline exceeded`)
- Cause: slow model/endpoint, proxy timeouts, or too-small `-http-timeout`.
- Fix: increase `-http-timeout` (or set env `OAI_HTTP_TIMEOUT`), reduce prompt size/model latency, or tune proxy timeouts.
```bash
# Example (Unix/macOS): command that sleeps too long
echo '{"cmd":"/bin/sleep","args":["2"],"timeoutSec":1}' | ./tools/bin/exec || true
# Increase timeout and retry
echo '{"cmd":"/bin/sleep","args":["1"],"timeoutSec":2}' | ./tools/bin/exec

# Example (Windows): use the built binary with .exe
echo '{"cmd":"timeout","args":["/T","2"],"timeoutSec":1}' | ./tools/bin/exec.exe || true
echo '{"cmd":"timeout","args":["/T","1"],"timeoutSec":2}' | ./tools/bin/exec.exe
```

## HTTP errors to the API
### Inspecting HTTP retry attempts
- When retries are enabled (`-http-retries > 0`), each attempt is logged to `.goagent/audit/YYYYMMDD.log` as an `http_attempt` entry with fields `{attempt,max,status,backoffMs,endpoint}`.
- Use this to confirm whether `Retry-After` or exponential backoff was applied and how many attempts occurred.

- Symptom: non-2xx from the Chat Completions endpoint with body included.
- Fix: ensure `OAI_BASE_URL`, `OAI_MODEL`, and `OAI_API_KEY` (if required) are set correctly.
```bash
# Minimal local setup example
export OAI_BASE_URL=http://localhost:1234/v1
export OAI_MODEL=oss-gpt-20b
# Optional if your endpoint requires it
export OAI_API_KEY=example-token

# Quick smoke
./scripts/smoke-test.sh || true
```

## golangci-lint not found
- Symptom: `golangci-lint: command not found` when running `make lint`.
- Fix:
```bash
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
export PATH="$(go env GOPATH)/bin:$PATH"
```

## General verification
- Run the test suite (offline):
```bash
go test ./...
```
- Rebuild CLI and tools deterministically:
```bash
make tidy build build-tools
```
