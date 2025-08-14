# Troubleshooting

This runbook covers common errors and deterministic fixes for `goagent`.

## Missing tool binaries
- Symptom: `exec: "./tools/<name>": file does not exist` or tool not found in `tools.json` command path.
- Fix:
```bash
# Build all tools
make build-tools
# Or build one tool explicitly
go build -o tools/fs_read_file ./tools/fs_read_file
```
- Verify:
```bash
./tools/fs_read_file -h 2>/dev/null || echo ok
ls -l tools | grep -E 'exec|fs_'
```

## Repo-relative path violations
- Symptom: file tools return errors about paths outside repository or cannot find the path.
- Fix: paths passed to fs tools must be relative to repository root. `cd` to repo root and retry.
```bash
# From repo root
echo '{"path":"README.md"}' | ./tools/fs_read_file | jq .
```

## Tool timeouts
- Symptom: `{"error":"tool timed out"}` mapped by the runner or non-zero exit due to timeout.
- Fix: increase per-tool `timeoutSec` in `tools.json` or use a larger global `-timeout` flag.
```bash
# Example: command that sleeps too long
echo '{"cmd":"/bin/sleep","args":["2"],"timeoutSec":1}' | ./tools/exec || true
# Increase timeout and retry
echo '{"cmd":"/bin/sleep","args":["1"],"timeoutSec":2}' | ./tools/exec
```

## HTTP errors to the API
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
