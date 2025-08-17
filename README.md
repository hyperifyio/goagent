# goagent — Minimal non‑interactive agent CLI for OpenAI‑compatible APIs

[![CI (lint+test+build)](https://github.com/hyperifyio/goagent/actions/workflows/ci.yml/badge.svg)](https://github.com/hyperifyio/goagent/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/hyperifyio/goagent)](https://github.com/hyperifyio/goagent/blob/main/go.mod)
[![Release](https://img.shields.io/github/v/release/hyperifyio/goagent?sort=semver)](https://github.com/hyperifyio/goagent/releases)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

goagent is a compact, vendor‑agnostic CLI for running non‑interactive, tool‑using agents against any OpenAI‑compatible Chat Completions API. It executes a minimal, auditable allowlist of local tools (argv only; no shell) and prints the model’s final answer. Use it with hosted APIs or local endpoints such as `http://localhost:1234/v1` when you need portable prototypes with strict, deterministic tool execution.

## Table of contents
- [Why goagent?](#why-goagent)
- [Features](#features)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Usage](#usage)
  - [Common flags](#common-flags)
  - [Why you usually don’t need to change knobs](#why-you-usually-don’t-need-to-change-knobs)
  - [Capabilities](#capabilities)
  - [Configuration](#configuration)
- [Examples](#examples)
  - [Zero-config with GPT-5](#zero-config-with-gpt-5)
  - [Tool calls transcript](#tool-calls-transcript)
  - [Exec tool](#exec-tool)
  - [Filesystem tools](#filesystem-tools)
- [Security](#security)
- [Troubleshooting](#troubleshooting)
- [Tests](#tests)
- [Documentation](#documentation)
- [Diagrams](#diagrams)
- [Contributing](#contributing)
- [CI quality gates](docs/operations/ci-quality-gates.md)
- [Tooling](#tooling)
- [Support](#support)
- [Roadmap](#roadmap)
- [Project status](#project-status)
- [License and credits](#license-and-credits)

## Why goagent?
- Minimal, portable, vendor‑agnostic: works with any OpenAI‑compatible endpoint
- Deterministic and auditable: argv‑only tool execution, JSON stdin/stdout, strict timeouts
- Safe by default: explicit allowlist of tools; no shell evaluation
- Batteries included: small toolbelt for filesystem and process tasks

## Installation
- Requirements: Go 1.24+, Linux/macOS/Windows. For local lint/path checks: `ripgrep` (rg) and `golangci-lint`. Network access to an OpenAI‑compatible API.

Choose one:

1) Download a binary from the [Releases](https://github.com/hyperifyio/goagent/releases) page (if available for your platform).

2) Install with Go (adds `agentcli` to your `GOBIN`):
```bash
go install github.com/hyperifyio/goagent/cmd/agentcli@latest
```

3) Build from source:
```bash
git clone https://github.com/hyperifyio/goagent
cd goagent
make bootstrap tidy build build-tools
```

Helpful developer prerequisites (examples):
```bash
# ripgrep
# - Ubuntu/Debian
sudo apt-get update && sudo apt-get install -y ripgrep
# - macOS (Homebrew)
brew install ripgrep

# golangci-lint (pinned; installs into ./bin via Makefile)
make install-golangci
./bin/golangci-lint version
```

## Configuration
Environment variables (flags take precedence):
- `OAI_BASE_URL` default `https://api.openai.com/v1` (scripts fall back from `LLM_BASE_URL` if unset)
- `OAI_MODEL` default `oss-gpt-20b` (scripts fall back from `LLM_MODEL` if unset)
- `OAI_API_KEY` only if your endpoint requires it (canonical; CLI also accepts `OPENAI_API_KEY` as a compatibility fallback)
- `OAI_HTTP_TIMEOUT` HTTP timeout for chat requests (duration string, e.g. `90s`); can also be set via `-http-timeout`

## Quick start
Build or install the CLI and point it to a reachable OpenAI‑compatible API (local or hosted):
```bash
export OAI_BASE_URL=http://localhost:1234/v1
export OAI_MODEL=oss-gpt-20b
make build build-tools # skip if installed via go install / release binary
```

Create a minimal `tools.json` next to the binary (Unix/macOS):
```json
{
  "tools": [
    {
      "name": "get_time",
      "description": "Return current time for an IANA timezone (default UTC). Accepts 'timezone' (canonical) and alias 'tz'.",
      "schema": {
        "type": "object",
        "properties": {
          "timezone": {"type": "string", "description": "e.g. Europe/Helsinki"},
          "tz": {"type": "string", "description": "Alias for timezone (deprecated)"}
        },
        "required": ["timezone"],
        "additionalProperties": false
      },
      "command": ["./tools/bin/get_time"],
      "timeoutSec": 5
    }
  ]
}
```

On Windows, use a `.exe` suffix for tool binaries:
```json
{
  "tools": [
    {
      "name": "get_time",
      "schema": {"type":"object","properties":{"timezone":{"type":"string"}},"required":["timezone"],"additionalProperties":false},
      "command": ["./tools/bin/get_time.exe"],
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

Expected behavior: the model may call `get_time`; the CLI executes `./tools/bin/get_time` (or `get_time.exe` on Windows) with JSON on stdin, appends the result as a `tool` message, calls the API again, then prints a one‑line final answer.

## Usage
Flags are order-insensitive. You can place `-prompt` and other flags in any order; precedence remains flag > environment > default.
### Common flags
```text
-prompt string         User prompt (required)
-tools string          Path to tools.json (optional)
-system string         System prompt (default: helpful and precise)
-base-url string       OpenAI‑compatible base URL (env OAI_BASE_URL; scripts accept LLM_BASE_URL fallback)
-api-key string        API key if required (env OAI_API_KEY; falls back to OPENAI_API_KEY)
-model string          Model ID (env OAI_MODEL; scripts accept LLM_MODEL fallback)
-max-steps int         Maximum reasoning/tool steps (default 8)
                       A hard ceiling of 15 is enforced; exceeding the cap
                       terminates with: "needs human review".
-http-timeout duration HTTP timeout for chat completions (env OAI_HTTP_TIMEOUT; default falls back to -timeout)
-http-retries int      Number of retries for transient HTTP failures (timeouts, 429, 5xx). Uses jittered exponential backoff. (default 2)
-http-retry-backoff duration Base backoff between HTTP retry attempts (exponential with jitter). (default 300ms)
-tool-timeout duration Per-tool timeout (default falls back to -timeout)
-timeout duration      [DEPRECATED] Global timeout; prefer -http-timeout and -tool-timeout
-temp float            Sampling temperature (default 1.0)
-top-p float           Nucleus sampling probability mass (conflicts with -temp; omits temperature when set)
-debug                 Dump request/response JSON to stderr
-capabilities          Print enabled tools and exit
-print-config          Print resolved config and exit
--version | -version   Print version and exit
```
Run `./bin/agentcli -h` to see the built‑in help.

### Why you usually don’t need to change knobs
- The default `-temp 1.0` is standardized for broad provider/model parity and GPT‑5 compatibility.
- The one‑knob rule applies: if you set `-top-p`, the agent omits `temperature`; otherwise it sends `temperature` (default 1.0) and leaves `top_p` unset.
- See the policy for details and rationale: [ADR‑0004: Default LLM policy](docs/adr/0004-default-llm-policy.md).

### Capabilities
List the enabled tools from a manifest without running the agent:
```bash
./bin/agentcli -tools ./tools.json -capabilities
```

## Examples
### Zero-config with GPT-5
Run against a GPT‑5 compatible endpoint without tuning sampling knobs. The CLI sends `temperature: 1.0` by default for models that support it.
```bash
./bin/agentcli -prompt "Say ok" -model gpt-5 -base-url "$OAI_BASE_URL" -api-key "$OAI_API_KEY" -max-steps 1 -debug
# stderr will include a request dump containing "\"temperature\": 1"
```

### Tool calls transcript
Minimal JSON transcript showing correct tool‑call sequencing:
```json
[
  {"role":"user","content":"What's the local time in Helsinki?"},
  {
    "role":"assistant",
    "content":null,
    "tool_calls":[
      {
        "id":"call_get_time_1",
        "type":"function",
        "function":{
          "name":"get_time",
          "arguments":"{\"timezone\":\"Europe/Helsinki\"}"
        }
      }
    ]
  },
  {
    "role":"tool",
    "tool_call_id":"call_get_time_1",
    "name":"get_time",
    "content":"{\"timezone\":\"Europe/Helsinki\",\"iso\":\"2025-08-17T12:34:56Z\",\"unix\":1755424496}"
  },
  {"role":"assistant","content":"It's 15:34 in Helsinki."}
]
```
Notes:
- For parallel tool calls (multiple entries in `tool_calls`), append one `role:"tool"` message per `id` before calling the API again. Order of tool messages is not significant as long as each `tool_call_id` is present exactly once.

### Worked example: tool calls and transcript
See `examples/tool_calls.md` for a self-contained, test-driven worked example that:
- Exercises default temperature 1.0
- Demonstrates a two-tool-call interaction with matching `tool_call_id`
- Captures a transcript via `-debug` showing request/response JSON dumps

Run the example test:
```bash
go test ./examples -run TestWorkedExample_ToolCalls_TemperatureOne_Sequencing -v
```

### Exec tool
Build the exec tool and run a simple command (Unix):
```bash
make build-tools
echo '{"cmd":"/bin/echo","args":["hello"]}' | ./tools/bin/exec
# => {"exitCode":0,"stdout":"hello\n","stderr":"","durationMs":<n>}
```
Timeout example:
```bash
echo '{"cmd":"/bin/sleep","args":["2"],"timeoutSec":1}' | ./tools/bin/exec
# => non-zero exit, stderr contains "timeout"
```

### Filesystem tools
The following examples assume `make build-tools` has produced binaries into `tools/bin/*`.

#### fs_read_file
```bash
make build-tools
printf 'hello world' > tmp_readme_demo.txt
echo '{"path":"tmp_readme_demo.txt"}' | ./tools/bin/fs_read_file | jq .
rm -f tmp_readme_demo.txt
```

#### fs_append_file
```bash
make build-tools
echo -n 'hello ' | base64 > b64a.txt
echo -n 'world'  | base64 > b64b.txt
echo '{"path":"tmp_append_demo.txt","contentBase64":"'"$(cat b64a.txt)"'"}' | ./tools/bin/fs_append_file | jq .
echo '{"path":"tmp_append_demo.txt","contentBase64":"'"$(cat b64b.txt)"'"}' | ./tools/bin/fs_append_file | jq .
cat tmp_append_demo.txt; rm -f tmp_append_demo.txt b64a.txt b64b.txt
```

#### fs_write_file
```bash
make build-tools
echo -n 'hello world' | base64 > b64.txt
echo '{"path":"tmp_write_demo.txt","contentBase64":"'"$(cat b64.txt)"'"}' | ./tools/bin/fs_write_file | jq .
cat tmp_write_demo.txt; rm -f tmp_write_demo.txt b64.txt
```

#### fs_mkdirp
```bash
make build-tools
echo '{"path":"tmp_mkdirp_demo/a/b/c","modeOctal":"0755"}' | ./tools/bin/fs_mkdirp | jq .
ls -ld tmp_mkdirp_demo/a/b/c
echo '{"path":"tmp_mkdirp_demo/a/b/c","modeOctal":"0755"}' | ./tools/bin/fs_mkdirp | jq .
rm -rf tmp_mkdirp_demo
```

#### fs_rm
```bash
make build-tools
printf 'temp' > tmp_rm_demo.txt
echo '{"path":"tmp_rm_demo.txt"}' | ./tools/bin/fs_rm | jq .
mkdir -p tmp_rm_dir/a/b && touch tmp_rm_dir/a/b/file.txt
echo '{"path":"tmp_rm_dir","recursive":true}' | ./tools/bin/fs_rm | jq .
rm -rf tmp_rm_dir
```

#### fs_move
```bash
make build-tools
printf 'payload' > tmp_move_src.txt
echo '{"from":"tmp_move_src.txt","to":"tmp_move_dst.txt"}' | ./tools/bin/fs_move | jq .
printf 'old' > tmp_move_dst.txt; printf 'new' > tmp_move_src.txt
echo '{"from":"tmp_move_src.txt","to":"tmp_move_dst.txt","overwrite":true}' | ./tools/bin/fs_move | jq .
rm -f tmp_move_src.txt tmp_move_dst.txt
```

#### fs_listdir
```bash
make build-tools
mkdir -p tmp_listdir_demo/a b && touch tmp_listdir_demo/.hidden tmp_listdir_demo/a/afile tmp_listdir_demo/bfile
echo '{"path":"tmp_listdir_demo"}' | ./tools/bin/fs_listdir | jq '.entries | map(.path)'
jq -n '{path:"tmp_listdir_demo",recursive:true,globs:["**/*"],includeHidden:false}' | ./tools/bin/fs_listdir | jq '.entries | map(select(.type=="file") | .path)'
rm -rf tmp_listdir_demo
```

#### fs_apply_patch
```bash
make build-tools
cat > /tmp/demo.diff <<'EOF'
--- /dev/null
+++ b/tmp_patch_demo.txt
@@ -0,0 +1,2 @@
+hello
+world
EOF
jq -n --arg d "$(cat /tmp/demo.diff)" '{unifiedDiff:$d}' | ./tools/bin/fs_apply_patch | jq .
printf 'hello
world
' | diff -u - tmp_patch_demo.txt && echo OK
```

#### fs_edit_range
```bash
make build-tools
printf 'abcdef' > tmp_edit_demo.txt
echo -n 'XY' | base64 > b64.txt
jq -n --arg b "$(cat b64.txt)" '{path:"tmp_edit_demo.txt",startByte:2,endByte:4,replacementBase64:$b}' | ./tools/bin/fs_edit_range | jq .
cat tmp_edit_demo.txt # => abXYef
rm -f tmp_edit_demo.txt b64.txt
```

#### fs_stat
```bash
make build-tools
printf 'hello world' > tmp_stat_demo.txt
echo '{"path":"tmp_stat_demo.txt","hash":"sha256"}' | ./tools/bin/fs_stat | jq .
rm -f tmp_stat_demo.txt
```

#### fs_search
```bash
make build-tools
mkdir -p tmp_search_demo && printf 'alpha\nbeta\ngamma\n' > tmp_search_demo/sample.txt
jq -n '{path:"tmp_search_demo",pattern:"^ga",glob:"**/*.txt",caseInsensitive:false}' | ./tools/bin/fs_search | jq '.matches'
rm -rf tmp_search_demo
```

## Features
- OpenAI‑compatible `POST /v1/chat/completions` via `net/http` (no SDK)
- Tool manifest `tools.json` using JSON Schema parameters (see [Tools manifest reference](docs/reference/tools-manifest.md))
- Per‑call timeouts; argv‑only execution with JSON stdin/stdout
- Deterministic tool error mapping as JSON (e.g., `{ "error": "..." }`)
- Minimal process environment for tools; audit logging with redaction

## Security
- Tools are an explicit allowlist from `tools.json`
- No shell interpretation; commands executed via argv only
- JSON contract on stdin/stdout; strict timeouts per call
- Treat model output as untrusted input; never pass it to a shell

See the full threat model in `docs/security/threat-model.md`.

### Unrestricted tools warning
- Enabling `exec` grants arbitrary command execution and may allow full network access. Treat this as remote code execution.
- Run the CLI and tools in a sandboxed environment (container/jail/VM) with least privilege.
- Keep `tools.json` minimal and audited. Do not pass secrets via tool arguments; prefer environment variables or CI secret stores.
- Audit log redaction: set `GOAGENT_REDACT` to mask sensitive values in audit entries. `OAI_API_KEY`/`OPENAI_API_KEY` are always masked if present.

## Troubleshooting
Common issues and deterministic fixes are documented with copy‑paste commands in `docs/runbooks/troubleshooting.md`.

## Documentation
Start with the [Documentation index](docs/README.md) for design docs, ADRs, and references:
- [Tools manifest reference](docs/reference/tools-manifest.md)
- [CLI reference](docs/reference/cli-reference.md)
- [Architecture: Module boundaries](docs/architecture/module-boundaries.md)
- [Security: Threat model](docs/security/threat-model.md)

## Diagrams
- Agent loop: `docs/diagrams/agentcli-seq.md`
- Toolbelt interactions: `docs/diagrams/toolbelt-seq.md`

## Tests
Run the full test suite (offline):
```bash
go test ./...
```
Lint, vet, and formatting checks:
```bash
make lint
make fmt   # apply gofmt -s -w to the repo
```

Guarded logs cleanup:
```bash
# Only removes ./logs when ./logs/STATE trimmed equals DOWN
make clean-logs

# End-to-end verification of the guard logic (creates temp dirs)
make test-clean-logs
```

Reproducible builds: the `Makefile` uses `-trimpath` and stripped `-ldflags` with VCS stamping disabled so two clean builds produce identical binaries. Verify locally by running two consecutive `make clean build build-tools` and comparing `sha256sum` outputs.

## Contributing
We welcome contributions! See `CONTRIBUTING.md` for workflow, standards, and how to run quality gates locally. Please also read `CODE_OF_CONDUCT.md`.

Useful local helpers during development:
- `make check-tools-paths` — enforce canonical `tools/cmd/NAME` sources and `tools/bin/NAME` invocations (requires `rg`)
- `make verify-manifest-paths` — ensure relative `tools.json` commands use `./tools/bin/NAME` (absolute allowed in tests)
- `make build-tool NAME=<name>` — build a single tool binary into `tools/bin/NAME`
- `make check-go-version` — fail fast if your local Go major.minor differs from `go.mod`

If your local toolchain does not match, you will see:
```text
Go toolchain mismatch: system X.Y != go.mod X.Y
```
Remediation: install the matching Go version shown by `go.mod` (e.g., from the official downloads) or switch via your version manager, then rerun `make check-go-version`.

## Tooling

This repository pins the toolchain for deterministic results:
- CI uses the Go version declared in `go.mod` across all OS jobs.
- Linting is performed with a pinned `golangci-lint` version managed by the `Makefile`.

See ADR‑0003 for the full policy and rationale: [docs/adr/0003-toolchain-and-lint-policy.md](docs/adr/0003-toolchain-and-lint-policy.md).

## Support
- Open an issue on the tracker: [Issues](https://github.com/hyperifyio/goagent/issues)

## Roadmap
Planned improvements and open ideas are tracked in `FUTURE_CHECKLIST.md`. Larger architectural decisions are recorded under `docs/adr/` (see ADR‑0001 and ADR‑0002). Contributions to the roadmap are welcome via issues and PRs.

## Project status
Experimental, but actively maintained. Interfaces may change before a stable 1.0.

## License and credits
MIT license. See `LICENSE`.

Inspired by OpenAI‑compatible agent patterns and built for portability and safety.

## More examples
See `examples/unrestricted.md` for copy‑paste prompts demonstrating `exec` + file tools to write, build, and run code in a sandboxed environment.
