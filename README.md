## goagent — Minimal non‑interactive agent CLI for OpenAI‑compatible APIs

[![CI](https://github.com/hyperifyio/goagent/actions/workflows/ci.yml/badge.svg)](https://github.com/hyperifyio/goagent/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/hyperifyio/goagent)](https://github.com/hyperifyio/goagent/blob/main/go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/hyperifyio/goagent?sort=semver)](https://github.com/hyperifyio/goagent/releases)

Small, vendor‑agnostic CLI that calls an OpenAI‑compatible Chat Completions API, executes an explicit allowlist of local tools (argv only, no shell), and prints the model’s final answer. Works with any endpoint that supports `POST /v1/chat/completions`, including a local server at `http://localhost:1234/v1`.

### Table of contents
- [Installation](#installation)
- [Quick start](#quick-start)
- [Usage](#usage)
 - [Exec tool example](#exec-tool-example)
 - [fs_read_file tool example](#fs_read_file-tool-example)
 - [fs_append_file tool example](#fs_append_file-tool-example)
 - [fs_mkdirp tool example](#fs_mkdirp-tool-example)
- [Features](#features)
- [Security model](#security-model)
- [Sequence diagram](#sequence-diagram)
- [Tests](#tests)
- [Contributing](#contributing)
- [Project status](#project-status)
- [License and credits](#license-and-credits)

### Installation
- **Prerequisites**: Go 1.21+; Linux/macOS/Windows. Network access to an OpenAI‑compatible API.

From a clean clone:
```bash
make tidy build build-tools
```

Optional environment (flags take precedence):
- `OAI_BASE_URL` default `https://api.openai.com/v1`
- `OAI_MODEL` default `oss-gpt-20b`
- `OAI_API_KEY` only if your endpoint requires it

### Quick start
Ensure an OpenAI‑compatible API is reachable (e.g., local server at `http://localhost:1234/v1`). Build the CLI and example tool:
```bash
export OAI_BASE_URL=http://localhost:1234/v1
export OAI_MODEL=openai/gpt-oss-20b
make build build-tools
```

Create a minimal `tools.json` next to the binary:
```json
{
  "tools": [
    {
      "name": "get_time",
      "description": "Return current time for an IANA timezone (default UTC).",
      "schema": {
        "type": "object",
        "properties": {
          "tz": {"type": "string", "description": "e.g. Europe/Helsinki"}
        }
      },
      "command": ["./tools/get_time"],
      "timeoutSec": 5
    }
  ]
}
```

Run the agent:
```bash
./bin/agentcli \
  -prompt "What's the local time in Helsinki? If tools are available, call get_time." \
  -tools ./tools.json \
  -debug
```

Expected behavior: the model may call `get_time`; the CLI executes `./tools/get_time` with JSON on stdin, appends the result as a `tool` message, calls the API again, then prints a one‑line final answer.

### Usage
Common flags:
```text
-prompt string         User prompt (required)
-tools string          Path to tools.json (optional)
-system string         System prompt (default: helpful and precise)
-base-url string       OpenAI‑compatible base URL (env OAI_BASE_URL)
-api-key string        API key if required (env OAI_API_KEY)
-model string          Model ID (env OAI_MODEL)
-max-steps int         Maximum reasoning/tool steps (default 8)
-timeout duration      HTTP and per‑tool timeout (default 30s)
-temp float            Sampling temperature (default 0.2)
-debug                 Dump request/response JSON to stderr
```
You can also run `./bin/agentcli -h` to see the built‑in help.

### Exec tool example
Build the exec tool and run a simple command (Unix):
```bash
make build-tools
echo '{"cmd":"/bin/echo","args":["hello"]}' | ./tools/exec
# => {"exitCode":0,"stdout":"hello\n","stderr":"","durationMs":<n>}
```
Timeout example:
```bash
echo '{"cmd":"/bin/sleep","args":["2"],"timeoutSec":1}' | ./tools/exec
# => exitCode>0 and stderr contains "timeout"
```

### fs_read_file tool example
Build the file‑read tool and read a file from the repository root (paths must be repo‑relative):
```bash
make build-tools
printf 'hello world' > tmp_readme_demo.txt
echo '{"path":"tmp_readme_demo.txt"}' | ./tools/fs_read_file | jq .
# => {"contentBase64":"aGVsbG8gd29ybGQ=","sizeBytes":11,"eof":true}
rm -f tmp_readme_demo.txt
```

### fs_append_file tool example
Append base64 content to a repo-relative file (creates the file if missing):
```bash
make build-tools
echo -n 'hello ' | base64 > b64a.txt
echo -n 'world'  | base64 > b64b.txt
echo '{"path":"tmp_append_demo.txt","contentBase64":"'"$(cat b64a.txt)"'"}' | ./tools/fs_append_file | jq .
echo '{"path":"tmp_append_demo.txt","contentBase64":"'"$(cat b64b.txt)"'"}' | ./tools/fs_append_file | jq .
cat tmp_append_demo.txt
rm -f tmp_append_demo.txt b64a.txt b64b.txt
```

### fs_write_file tool example
Atomically write a file using base64 content:
```bash
make build-tools
echo -n 'hello world' | base64 > b64.txt
echo '{"path":"tmp_write_demo.txt","contentBase64":"'"$(cat b64.txt)"'"}' | ./tools/fs_write_file | jq .
cat tmp_write_demo.txt
rm -f tmp_write_demo.txt b64.txt
```

### fs_mkdirp tool example
Create directories recursively (idempotent; returns created=true on first call, false thereafter):
```bash
# Build this tool (until aggregated in Makefile)
go build -o tools/fs_mkdirp ./tools/fs_mkdirp

echo '{"path":"tmp_mkdirp_demo/a/b/c","modeOctal":"0755"}' | ./tools/fs_mkdirp | jq .
ls -ld tmp_mkdirp_demo/a/b/c

# Second call is idempotent (created=false)
echo '{"path":"tmp_mkdirp_demo/a/b/c","modeOctal":"0755"}' | ./tools/fs_mkdirp | jq .
rm -rf tmp_mkdirp_demo
```

### Features
- OpenAI‑compatible `POST /v1/chat/completions` via `net/http` (no SDK)
- Tool manifest `tools.json` using JSON Schema for parameters
- Per‑call timeouts; argv‑only execution with JSON stdin/stdout
- Deterministic tool error mapping as JSON (e.g., `{"error":"..."}`)

### Security model
- Tools are an explicit allowlist from `tools.json`
- No shell interpretation; commands executed via argv only
- JSON contract on stdin/stdout; strict timeouts per call
- Treat model output as untrusted input; never pass to a shell

### Sequence diagram
See `docs/diagrams/agentcli-seq.md` for the message flow.

### Tests
Run everything locally:
```bash
go test ./...
```
Lint, vet, and formatting checks:
```bash
make lint
```

### Contributing
Contributions are welcome! Please open an issue and a pull request. For larger changes, discuss first in an issue. See architecture notes in `docs/adr/0001-minimal-agent-cli.md`.

### Project status
Experimental, but actively maintained. Interfaces may change before a stable 1.0.

### License and credits
MIT license. See `LICENSE`. Inspired by OpenAI‑compatible agent patterns.
