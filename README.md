## goagent — Minimal non-interactive agent CLI for OpenAI‑compatible APIs

Small, vendor-agnostic CLI that calls an OpenAI-compatible Chat Completions API, executes allowed local tools (argv only, no shell), and returns the model's final answer. Works with any endpoint speaking `POST /v1/chat/completions`, including a local server at `http://localhost:1234/v1`.

Badges: [![CI](https://github.com/hyperifyio/goagent/actions/workflows/ci.yml/badge.svg)](https://github.com/hyperifyio/goagent/actions/workflows/ci.yml) [![Go Version](https://img.shields.io/github/go-mod/go-version/hyperifyio/goagent)](LICENSE) [![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE) [![Release](https://img.shields.io/github/v/release/hyperifyio/goagent?sort=semver)](https://github.com/hyperifyio/goagent/releases)

### Table of contents
- Installation
- Quick start
- Features
- Security model
- Sequence diagram
- Tests
- Roadmap and contributing
- License and credits

### Installation
Prerequisites: Go 1.21+

Commands:
```bash
make tidy build build-tools
```

Environment (optional; flags take precedence):
- `OAI_BASE_URL` default `https://api.openai.com/v1`
- `OAI_MODEL` default `oss-gpt-20b`
- `OAI_API_KEY` only if your endpoint requires it

### Quick start
Ensure an OpenAI-compatible API is reachable, e.g. local server at `http://localhost:1234/v1` with model `openai/gpt-oss-20b`.

```bash
export OAI_BASE_URL=http://localhost:1234/v1
export OAI_MODEL=openai/gpt-oss-20b
make build build-tools
./bin/agentcli \
  -prompt "What's the local time in Helsinki? If tools are available, call get_time." \
  -tools ./tools.json \
  -debug
```

Expected behavior: the model may trigger the `get_time` tool; the CLI runs `./tools/get_time` with JSON stdin, appends the result as a `tool` message, asks the model again, then prints a one-line final answer.

### Features
- OpenAI-compatible `POST /v1/chat/completions` via `net/http` (no SDK)
- Tool manifest `tools.json` with JSON Schema for parameters
- Per-call timeouts; argv-only tool execution with JSON stdin/stdout
- Deterministic error mapping for tools: `{"error":"..."}`

### Sequence diagram
See `docs/diagrams/agentcli-seq.md` for the message flow.

### Security model
- Tools are an explicit allowlist from `tools.json`
- No shell interpretation; commands are executed via argv only
- JSON contract on stdin/stdout; per-call timeout enforcement
- Treat model outputs as untrusted input; never pass to a shell

### Tests
Run the suite:
```bash
go test ./...
```

### Roadmap and contributing
- Add diverse tests and end-to-end fixtures against local endpoints
- Document architecture as ADR (`docs/adr/0001-minimal-agent-cli.md`)
- Contributions welcome: open an issue and PR. License: MIT.

Support: please open issues in the tracker.
Roadmap: see `docs/ROADMAP.md`.

### License and credits
MIT. Inspired by OpenAI-compatible agent patterns.
