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

#### Enable HTTP retries
- When transient, enable retries to mask brief outages and 429/5xx with backoff.
```bash
# Retry up to 2 times with capped backoff (Unix/macOS)
./bin/agentcli \
  -prompt "What's the local time in Helsinki? Use get_time." \
  -tools ./tools.json \
  -http-retries 2 \
  -http-retry-backoff 500ms

# Windows (PowerShell)
./bin/agentcli.exe `
  -prompt "What's the local time in Helsinki? Use get_time." `
  -tools ./tools.json `
  -http-retries 2 `
  -http-retry-backoff 500ms

# Inspect attempts in the audit log (.goagent/audit/YYYYMMDD.log)
rg -n "http_attempt" .goagent/audit || true
```

## fs_search exclusions and file size limits
- Behavior: `fs_search` intentionally skips known binary/output directories to keep scans fast and predictable: `.git/`, `bin/`, `logs/`, and `tools/bin/` are excluded. It also enforces a per‑file size cap of 1 MiB.
- Symptom: expected matches inside excluded folders are not returned, or the tool exits non‑zero with a `FILE_TOO_LARGE` message.
- Fix:
```bash
# Verify exclusion behavior (create a file in an excluded dir and one in a normal dir)
mkdir -p tmp_search_demo/{bin,logs,ok}
printf 'NEEDLE' > tmp_search_demo/bin/skip.txt
printf 'NEEDLE' > tmp_search_demo/ok/scan.txt

echo '{"query":"NEEDLE","globs":["**/*.txt"],"maxResults":10}' | ./tools/bin/fs_search | jq .
# Expect only tmp_search_demo/ok/scan.txt to appear; files under bin/ or logs/ are skipped

# Demonstrate FILE_TOO_LARGE
python3 - <<'PY'
from pathlib import Path
p = Path('tmp_search_demo/ok/big.bin')
p.write_bytes(b'A' * (1024*1024 + 1))
print(p, p.stat().st_size)
PY
echo '{"query":"A","globs":["tmp_search_demo/ok/big.bin"]}' | ./tools/bin/fs_search || true
# Expect non-zero exit and stderr JSON containing "FILE_TOO_LARGE"

rm -rf tmp_search_demo
```

On Windows (PowerShell), use the `.exe` binary:
```powershell
New-Item -ItemType Directory -Force tmp_search_demo/bin,tmp_search_demo/logs,tmp_search_demo/ok | Out-Null
Set-Content -NoNewline -Path tmp_search_demo/bin/skip.txt -Value NEEDLE
Set-Content -NoNewline -Path tmp_search_demo/ok/scan.txt -Value NEEDLE
echo '{"query":"NEEDLE","globs":["**/*.txt"],"maxResults":10}' | ./tools/bin/fs_search.exe | jq .

# FILE_TOO_LARGE (PowerShell)
python - <<'PY'
from pathlib import Path
p = Path('tmp_search_demo/ok/big.bin')
p.write_bytes(b'A' * (1024*1024 + 1))
print(p, p.stat().st_size)
PY
echo '{"query":"A","globs":["tmp_search_demo/ok/big.bin"]}' | ./tools/bin/fs_search.exe; if ($LASTEXITCODE -eq 0) { Write-Error 'expected non-zero' }
Remove-Item -Recurse -Force tmp_search_demo
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

## Image generation errors (img_create)

- Invalid API key or missing base URL
  - Symptom: stderr JSON like `{"error":"missing OAI_IMAGE_BASE_URL or OAI_BASE_URL"}` or API error `{"error":"unauthorized"}`.
  - Fix:
    ```bash
    # Set base URL and API key (Unix/macOS)
    export OAI_IMAGE_BASE_URL=https://api.openai.com
    export OAI_API_KEY=sk-...
    # Optional: fallback base if OAI_IMAGE_BASE_URL is unset
    export OAI_BASE_URL=https://api.openai.com
    ```
    See reference: docs/reference/img_create.md

- HTTP 429 (rate limited) or 5xx
  - Behavior: the tool retries up to 2 times with backoff (250ms, 500ms, 1s) and then emits `{"error":"api status 429"}` or a server message if present.
  - Fix: wait and retry; reduce parallel invocations; consider lowering `n` or image `size` to lessen load:
    ```bash
    echo '{"prompt":"tiny-pixel","n":1,"size":"512x512","save":{"dir":"assets"}}' | ./tools/bin/img_create || true
    ```

- Moderation/refusal or API 400 with message
  - Behavior: non-2xx with body `{error:"..."}` or `{error:{message:"..."}}` is surfaced as that message in stderr JSON.
  - Fix: adjust the prompt to comply with policy; verify `size` matches `^\d{3,4}x\d{3,4}$` and `n` is 1..4.

- Request timeout
  - Symptom: `{"error":"http error: context deadline exceeded"}` when the Images API is slow.
  - Fix: increase the HTTP timeout and retry:
    ```bash
    export OAI_HTTP_TIMEOUT=180s
    echo '{"prompt":"tiny-pixel","n":1,"size":"1024x1024","save":{"dir":"assets"}}' | ./tools/bin/img_create || true
    ```
  - If timeouts persist, try a smaller `size` or lower `n`.

- Missing save.dir when not returning base64
  - Symptom: `{"error":"save.dir is required when return_b64=false"}`.
  - Fix: provide a repo-relative directory under `save.dir` or set `return_b64:true`:
    ```bash
    # Save to repo-relative assets/
    echo '{"prompt":"tiny-pixel","save":{"dir":"assets"}}' | ./tools/bin/img_create

    # Or return base64 (elided by default)
    echo '{"prompt":"tiny-pixel","return_b64":true}' | ./tools/bin/img_create
    ```

Notes:
- The tool only writes under the repository root and rejects absolute paths or `..` escapes.
- By default, base64 in stdout is elided; set `IMG_CREATE_DEBUG_B64=1` (or `DEBUG_B64=1`) to include it when `return_b64=true`.

## General verification
- Run the test suite (offline):
```bash
go test ./...
```
- Rebuild CLI and tools deterministically:
```bash
make tidy build build-tools
```

## Invalid tool message sequencing
- ## Pre-stage built-in tools
- Behavior: during pre-stage, external tools from `-tools` are ignored by default. Only built-in read-only adapters are available: `fs.read_file`, `fs.list_dir`, `fs.stat`, `env.get`, `os.info`.
- Symptom: a pre-stage `tool_calls` entry like `echo` or `exec` results in a tool message `{"error":"unknown tool: ..."}`.
- Fix: either rely on built-ins, or explicitly enable external tools for pre-stage with `-prep-tools-allow-external` (use with caution).

- Symptom: the CLI exits with an error like:
- `error: invalid message sequence at index N: found role:"tool" without a prior assistant message containing tool_calls; each tool message must respond to an assistant tool call id`
- or: `error: invalid message sequence at index N: role:"tool" has tool_call_id "..." that does not match any id from the most recent assistant tool_calls`
- Cause: a tool result message was appended without a preceding assistant message that requested tool calls, or the `tool_call_id` does not match the most recent assistant `tool_calls` ids.
- Fix: ensure the message flow strictly follows: assistant with `tool_calls[]` → one tool message per `tool_call_id` → assistant. Do not emit standalone tool messages. The CLI enforces this pre-flight and will refuse to send an invalid transcript to the API. This validator runs for both the main loop and the pre-stage (prep) call; errors during pre-stage may appear as `prep invalid message sequence`.
