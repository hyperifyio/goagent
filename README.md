# goagent — Minimal, safe, non‑interactive agent CLI

[![CI (lint+test+build)](https://github.com/hyperifyio/goagent/actions/workflows/ci.yml/badge.svg)](https://github.com/hyperifyio/goagent/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/hyperifyio/goagent)](https://github.com/hyperifyio/goagent/blob/main/go.mod)
[![Go Reference](https://pkg.go.dev/badge/github.com/hyperifyio/goagent.svg)](https://pkg.go.dev/github.com/hyperifyio/goagent)
[![Release](https://img.shields.io/github/v/release/hyperifyio/goagent?sort=semver)](https://github.com/hyperifyio/goagent/releases)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

goagent is a compact, vendor‑agnostic command‑line tool for running non‑interactive, tool‑using agents against any OpenAI‑compatible Chat Completions API. It executes a small, auditable allowlist of local tools (argv only; no shell), streams JSON in/out, and prints a concise final answer.

- Why use it: deterministic, portable, and safe by default. Works with hosted providers and with local endpoints like `http://localhost:1234/v1`.
- Who it’s for: engineers who want a minimal agent runner with clear guarantees and zero vendor lock‑in.

## Table of contents
- [Why goagent?](#why-goagent)
- [Features](#features)
- [Installation](#installation)
- [Configuration](#configuration)
- [Quick start](#quick-start)
- [Usage](#usage)
  - [Common flags](#common-flags)
  - [Why you usually don’t need to change knobs](#why-you-usually-dont-need-to-change-knobs)
  - [Capabilities](#capabilities)
- [Examples](#examples)
  - [Zero-config with GPT-5](#zero-config-with-gpt-5)
  - [Tool calls transcript](#tool-calls-transcript)
  - [Worked example: tool calls and transcript](#worked-example-tool-calls-and-transcript)
  - [View refined messages (pre-stage and final)](#view-refined-messages-pre-stage-and-final)
  - [Exec tool](#exec-tool)
  - [Filesystem tools](#filesystem-tools)
  - [Image generation tool (img_create)](#image-generation-tool-img_create)
  - [fs_search](#fs_search)
- [Security](#security)
- [Troubleshooting](#troubleshooting)
- [Tests](#tests)
- [Documentation](#documentation)
- [Diagrams](#diagrams)
- [Contributing](#contributing)
- [Tooling](#tooling)
- [Support](#support)
- [Roadmap](#roadmap)
- [Project status](#project-status)
- [License and credits](#license-and-credits)
- [More examples](#more-examples)
- [CI quality gates](docs/operations/ci-quality-gates.md)

## Why goagent?
- Minimal, portable, vendor‑agnostic: works with any OpenAI‑compatible endpoint
- Deterministic and auditable: argv‑only tool execution, JSON stdin/stdout, strict timeouts
- Safe by default: explicit allowlist of tools; no shell evaluation
- Batteries included: small toolbelt for filesystem and process tasks

## Features
- OpenAI‑compatible `POST /v1/chat/completions` via `net/http` (no SDK)
- Tool manifest `tools.json` using JSON Schema parameters (see [Tools manifest reference](docs/reference/tools-manifest.md))
- Per‑call timeouts; argv‑only execution with JSON stdin/stdout
- Deterministic tool error mapping as JSON (e.g., `{ "error": "..." }`)
- Minimal process environment for tools; audit logging with redaction

## Installation

### Requirements
- Go 1.24+ on Linux, macOS, or Windows
- Network access to an OpenAI‑compatible API
- For development: `ripgrep` (rg) and `golangci-lint`

### Install options
1) Download a binary: see [Releases](https://github.com/hyperifyio/goagent/releases)

2) With Go (adds `agentcli` to your `GOBIN`):
```bash
go install github.com/hyperifyio/goagent/cmd/agentcli@latest
```

3) Build from source:
```bash
git clone https://github.com/hyperifyio/goagent
cd goagent
make bootstrap tidy build build-tools
```

Developer prerequisites (examples):
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
Configuration precedence is: **flags > environment > built‑in defaults**.

Environment variables:
- `OAI_BASE_URL` — API base (default `https://api.openai.com/v1`). Helper scripts will also read `LLM_BASE_URL` if present.
- `OAI_MODEL` — model ID (default `oss-gpt-20b`). Helper scripts will also read `LLM_MODEL` if present.
- `OAI_API_KEY` — API key when required. The CLI also accepts `OPENAI_API_KEY` for compatibility.
- `OAI_HTTP_TIMEOUT` — HTTP timeout for chat requests (e.g., `90s`). Mirrors `-http-timeout`.
  `OAI_PREP_HTTP_TIMEOUT` — HTTP timeout for pre-stage; overrides inheritance from `-http-timeout`.

## Quick start
Install the CLI and point it to a reachable OpenAI‑compatible API (local or hosted):
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

Expected behavior: the model may call `get_time`; the CLI executes `./tools/bin/get_time` (or `get_time.exe` on Windows) with JSON on stdin, appends the result as a `tool` message, calls the API again, then prints a concise final answer.

## Usage
Flags are order‑insensitive. You can place `-prompt` and other flags in any order; precedence remains flag > environment > default.
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
-prep-http-timeout duration HTTP timeout for pre-stage (env OAI_PREP_HTTP_TIMEOUT; default falls back to -http-timeout)
-prep-model string      Pre-stage model ID (env OAI_PREP_MODEL; inherits -model if unset)
-prep-base-url string   Pre-stage base URL (env OAI_PREP_BASE_URL; inherits -base-url if unset)
-prep-api-key string    Pre-stage API key (env OAI_PREP_API_KEY; falls back to OAI_API_KEY/OPENAI_API_KEY; inherits -api-key if unset)
-prep-http-retries int  Pre-stage HTTP retries (env OAI_PREP_HTTP_RETRIES; inherits -http-retries if unset)
-prep-http-retry-backoff duration Pre-stage HTTP retry backoff (env OAI_PREP_HTTP_RETRY_BACKOFF; inherits -http-retry-backoff if unset)
-prep-dry-run           Run pre-stage only, print refined Harmony messages to stdout, and exit 0
-print-messages         Pretty-print the final merged message array to stderr before the main call
-http-retries int      Number of retries for transient HTTP failures (timeouts, 429, 5xx). Uses jittered exponential backoff. (default 2)
-http-retry-backoff duration Base backoff between HTTP retry attempts (exponential with jitter). (default 300ms)
-tool-timeout duration Per-tool timeout (default falls back to -timeout)
-timeout duration      [DEPRECATED] Global timeout; prefer -http-timeout and -tool-timeout
-temp float            Sampling temperature (default 1.0)
-top-p float           Nucleus sampling probability mass (conflicts with -temp; omits temperature when set)
-prep-top-p float      Nucleus sampling probability mass for pre-stage (conflicts with -temp; omits temperature when set)
-prep-profile string   Pre-stage prompt profile (deterministic|general|creative|reasoning); sets temperature when supported (conflicts with -prep-top-p)
-prep-enabled          Enable pre-stage (default true). When false, skip pre-stage and proceed directly to main call.
-debug                 Dump request/response JSON to stderr
-verbose               Also print non-final assistant channels (critic/confidence) to stderr
-channel-route name=stdout|stderr|omit
                       Override default channel routing (final→stdout, critic/confidence→stderr); repeatable
-quiet                 Suppress non-final output; print only final text to stdout
-capabilities          Print enabled tools and exit
-print-config          Print resolved config and exit
--version | -version   Print version and exit
```
Run `./bin/agentcli -h` to see the built‑in help.

### Why you usually don’t need to change knobs
- The default `-temp 1.0` is standardized for broad provider/model parity and GPT‑5 compatibility.
- The one‑knob rule applies: if you set `-top-p`, the agent omits `temperature`; otherwise it sends `temperature` (default 1.0) and leaves `top_p` unset.
- The one‑knob rule applies for both stages: if you set `-top-p` (or `-prep-top-p`), the agent omits `temperature` for that stage; otherwise it sends `temperature` (default 1.0) when supported. Pre‑stage profiles are available via `-prep-profile`, e.g. `deterministic` sets temperature to 0.1 when supported.
- See the policy for details and rationale: [ADR‑0004: Default LLM policy](docs/adr/0004-default-llm-policy.md).

### Capabilities
List enabled tools from a manifest without running the agent. The output includes a prominent header warning, and certain tools like `img_create` are annotated with an extra warning because they make outbound network calls and can save files:
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
- Transcript hygiene: when running without `-debug`, the CLI replaces any single tool message content larger than 8 KiB with `{"truncated":true,"reason":"large-tool-output"}` before sending to the API. Use `-debug` to inspect full payloads during troubleshooting.

### Worked example: tool calls and transcript
See `examples/tool_calls.md` for a self-contained, test-driven worked example that:
- Exercises default temperature 1.0
- Demonstrates a two-tool-call interaction with matching `tool_call_id`
- Captures a transcript via `-debug` showing request/response JSON dumps

Run the example test:
```bash
go test ./examples -run TestWorkedExample_ToolCalls_TemperatureOne_Sequencing -v
```

### View refined messages (pre-stage and final)
See also ADR‑0005 for the pre‑stage flow and channel routing details: `docs/adr/0005-harmony-pre-processing-and-channel-aware-output.md`.
Inspect message arrays deterministically without running the full loop:

```bash
# Pre-stage only: print refined Harmony messages and exit
./bin/agentcli -prompt "Say ok" -prep-dry-run | jq .

# Before the main call: pretty-print merged messages to stderr, then proceed
./bin/agentcli -prompt "Say ok" -print-messages 2> >(jq .)
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

### Image generation tool (img_create)

Generate images via an OpenAI‑compatible Images API and save files into your repository (default) or return base64 on demand.

Quickstart (Unix/macOS/Windows via `make build-tools`):

```bash
make build-tools
```

Minimal `tools.json` entry (copy/paste next to your binary):

```json
{
  "tools": [
    {
      "name": "img_create",
      "description": "Generate image(s) with OpenAI Images API and save to repo or return base64",
      "schema": {
        "type": "object",
        "required": ["prompt"],
        "properties": {
          "prompt": {"type": "string"},
          "n": {"type": "integer", "minimum": 1, "maximum": 4, "default": 1},
          "size": {"type": "string", "pattern": "^\\d{3,4}x\\d{3,4}$", "default": "1024x1024"},
          "model": {"type": "string", "default": "gpt-image-1"},
          "return_b64": {"type": "boolean", "default": false},
          "save": {
            "type": "object",
            "required": ["dir"],
            "properties": {
              "dir": {"type": "string"},
              "basename": {"type": "string", "default": "img"},
              "ext": {"type": "string", "enum": ["png"], "default": "png"}
            },
            "additionalProperties": false
          }
        },
        "additionalProperties": false
      },
      "command": ["./tools/bin/img_create"],
      "timeoutSec": 120,
      "envPassthrough": ["OAI_API_KEY", "OAI_BASE_URL", "OAI_IMAGE_BASE_URL", "OAI_HTTP_TIMEOUT"]
    }
  ]
}
```

Run the agent with a prompt that instructs the assistant to call `img_create` and save under `assets/`:

```bash
export OAI_BASE_URL=${OAI_BASE_URL:-https://api.openai.com/v1}
export OAI_API_KEY=your-key

./bin/agentcli \
  -tools ./tools.json \
  -prompt "Generate a tiny illustrative image using img_create and save it under assets/ with basename banner" \
  -debug

# Expect: one or more PNGs under assets/ (e.g., assets/banner_001.png) and a concise final message on stdout
```

Notes:
- By default, the tool writes image files and does not include base64 in transcripts, avoiding large payloads.
- To return base64 instead, pass `{ "return_b64": true }` to the tool; base64 is elided in stdout unless `IMG_CREATE_DEBUG_B64=1` or `DEBUG_B64=1` is set.
- Windows: the built binary is `./tools/bin/img_create.exe` and `tools.json` should reference the `.exe`.
- See Troubleshooting for network/API issues and timeouts: `docs/runbooks/troubleshooting.md`.

#### fs_search
```bash
make build-tools
mkdir -p tmp_search_demo && printf 'alpha\nbeta\ngamma\n' > tmp_search_demo/sample.txt
jq -n '{path:"tmp_search_demo",pattern:"^ga",glob:"**/*.txt",caseInsensitive:false}' | ./tools/bin/fs_search | jq '.matches'
rm -rf tmp_search_demo
```

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
- [ADR‑0005: Harmony pre‑processing and channel‑aware output](docs/adr/0005-harmony-pre-processing-and-channel-aware-output.md)

## Diagrams
- Agent loop: `docs/diagrams/agentcli-seq.md`
- Toolbelt interactions: `docs/diagrams/toolbelt-seq.md`
- Pre‑stage flow: `docs/diagrams/harmony-prep-seq.md`

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
Contributions are welcome! See `CONTRIBUTING.md` for workflow, coding standards, and how to run quality gates locally. Please also read `CODE_OF_CONDUCT.md`.

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
