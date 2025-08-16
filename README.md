## goagent — Minimal non‑interactive agent CLI for OpenAI‑compatible APIs

[![CI](https://github.com/hyperifyio/goagent/actions/workflows/ci.yml/badge.svg)](https://github.com/hyperifyio/goagent/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/hyperifyio/goagent)](https://github.com/hyperifyio/goagent/blob/main/go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/hyperifyio/goagent?sort=semver)](https://github.com/hyperifyio/goagent/releases)

Small, vendor‑agnostic CLI that calls an OpenAI‑compatible Chat Completions API, executes an explicit allowlist of local tools (argv only, no shell), and prints the model’s final answer. Works with any endpoint that supports `POST /v1/chat/completions`, including a local server at `http://localhost:1234/v1`.

### Table of contents
- [Documentation index](docs/README.md)
- [Tools manifest reference](docs/reference/tools-manifest.md)
- [Architecture: Module boundaries](docs/architecture/module-boundaries.md)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Usage](#usage)
  - [Exec tool example](#exec-tool-example)
  - [fs_read_file tool example](#fs_read_file-tool-example)
  - [fs_append_file tool example](#fs_append_file-tool-example)
  - [fs_mkdirp tool example](#fs_mkdirp-tool-example)
  - [fs_rm tool example](#fs_rm-tool-example)
  - [fs_move tool example](#fs_move-tool-example)
  - [fs_listdir tool example](#fs_listdir-tool-example)
  - [fs_apply_patch tool example](#fs_apply_patch-tool-example)
  - [fs_edit_range tool example](#fs_edit_range-tool-example)
- [fs_stat tool example](#fs_stat-tool-example)
- [Features](#features)
- [Security model](#security-model)
- [Troubleshooting](#troubleshooting)
- [Sequence diagram](#sequence-diagram)
- [Tests](#tests)
- [Contributing](#contributing)
- [Project status](#project-status)
- [License and credits](#license-and-credits)
  - [Unrestricted tools warning](#unrestricted-tools-warning)
  - [Unrestricted examples](#unrestricted-examples)

### Installation
- **Prerequisites**: Go 1.24+; Linux/macOS/Windows; ripgrep (`rg`) and `golangci-lint` for local lint/path checks. Network access to an OpenAI‑compatible API.

Install prerequisites (examples):

```bash
# ripgrep
# - Ubuntu/Debian
sudo apt-get update && sudo apt-get install -y ripgrep
# - macOS (Homebrew)
brew install ripgrep

# golangci-lint (installs into $(go env GOPATH)/bin)
go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.59.1
# Ensure GOPATH/bin is on your PATH so the binary is discoverable
export PATH="$(go env GOPATH)/bin:$PATH"
# Note: make lint will attempt installation if missing, but it also relies on PATH
```

From a clean clone:
```bash
make tidy build build-tools
```

Optional environment (flags take precedence):
- `OAI_BASE_URL` default `https://api.openai.com/v1` (scripts fall back from `LLM_BASE_URL` if unset)
- `OAI_MODEL` default `oss-gpt-20b` (scripts fall back from `LLM_MODEL` if unset)
- `OAI_API_KEY` only if your endpoint requires it (canonical; script and CLI also accept `OPENAI_API_KEY` as a fallback for compatibility)

### Quick start
Ensure an OpenAI‑compatible API is reachable (e.g., local server at `http://localhost:1234/v1`). Build the CLI and example tool:
```bash
export OAI_BASE_URL=http://localhost:1234/v1
export OAI_MODEL=oss-gpt-20b
make build build-tools
```

Create a minimal `tools.json` next to the binary (Unix/macOS):
```json
{
  "tools": [
    {
      "name": "get_time",
      "description": "Return current time for an IANA timezone (default UTC). Accepts 'timezone' (canonical) and also alias 'tz'.",
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

On Windows, the binary name uses a `.exe` suffix:

```json
{
  "tools": [
    {
      "name": "get_time",
      "schema": {
        "type": "object",
        "properties": {
          "timezone": {"type": "string", "description": "e.g. Europe/Helsinki"},
          "tz": {"type": "string", "description": "Alias for timezone (deprecated)"}
        },
        "required": ["timezone"],
        "additionalProperties": false
      },
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

Expected behavior: the model may call `get_time`; the CLI executes the tool binary from `./tools/bin/get_time` (or `get_time.exe` on Windows) with JSON on stdin, appends the result as a `tool` message, calls the API again, then prints a one‑line final answer.

### Usage
Common flags:
```text
-prompt string         User prompt (required)
-tools string          Path to tools.json (optional)
-system string         System prompt (default: helpful and precise)
-base-url string       OpenAI‑compatible base URL (env OAI_BASE_URL; scripts accept LLM_BASE_URL fallback)
-api-key string        API key if required (env OAI_API_KEY; falls back to OPENAI_API_KEY)
-model string          Model ID (env OAI_MODEL; scripts accept LLM_MODEL fallback)
-max-steps int         Maximum reasoning/tool steps (default 8)
-timeout duration      HTTP and per‑tool timeout (default 30s)
-temp float            Sampling temperature (default 0.2)
-debug                 Dump request/response JSON to stderr
-capabilities          Print enabled tools and exit
```
You can also run `./bin/agentcli -h` to see the built‑in help.

### Exec tool example
Build the exec tool and run a simple command (Unix):
```bash
make build-tools
echo '{"cmd":"/bin/echo","args":["hello"]}' | ./tools/bin/exec
# => {"exitCode":0,"stdout":"hello\n","stderr":"","durationMs":<n>}
```
Timeout example:
```bash
echo '{"cmd":"/bin/sleep","args":["2"],"timeoutSec":1}' | ./tools/bin/exec
# => exitCode>0 and stderr contains "timeout"
```

### fs_read_file tool example
Build the file‑read tool and read a file from the repository root (paths must be repo‑relative):
```bash
make build-tools
printf 'hello world' > tmp_readme_demo.txt
echo '{"path":"tmp_readme_demo.txt"}' | ./tools/bin/fs_read_file | jq .
# => {"contentBase64":"aGVsbG8gd29ybGQ=","sizeBytes":11,"eof":true}
rm -f tmp_readme_demo.txt
```

### fs_append_file tool example
Append base64 content to a repo-relative file (creates the file if missing):
```bash
make build-tools
echo -n 'hello ' | base64 > b64a.txt
echo -n 'world'  | base64 > b64b.txt
echo '{"path":"tmp_append_demo.txt","contentBase64":"'"$(cat b64a.txt)"'"}' | ./tools/bin/fs_append_file | jq .
echo '{"path":"tmp_append_demo.txt","contentBase64":"'"$(cat b64b.txt)"'"}' | ./tools/bin/fs_append_file | jq .
cat tmp_append_demo.txt
rm -f tmp_append_demo.txt b64a.txt b64b.txt
```

### fs_write_file tool example
Atomically write a file using base64 content:
```bash
make build-tools
echo -n 'hello world' | base64 > b64.txt
echo '{"path":"tmp_write_demo.txt","contentBase64":"'"$(cat b64.txt)"'"}' | ./tools/bin/fs_write_file | jq .
cat tmp_write_demo.txt
rm -f tmp_write_demo.txt b64.txt
```

### fs_mkdirp tool example
Create directories recursively (idempotent; returns created=true on first call, false thereafter):
```bash
make build-tools

echo '{"path":"tmp_mkdirp_demo/a/b/c","modeOctal":"0755"}' | ./tools/bin/fs_mkdirp | jq .
ls -ld tmp_mkdirp_demo/a/b/c

# Second call is idempotent (created=false)
echo '{"path":"tmp_mkdirp_demo/a/b/c","modeOctal":"0755"}' | ./tools/bin/fs_mkdirp | jq .
rm -rf tmp_mkdirp_demo
```

### fs_rm tool example
Remove a file or directory tree (paths must be repo‑relative; set `recursive:true` for directories):
```bash
make build-tools
# Remove a single file
printf 'temp' > tmp_rm_demo.txt
echo '{"path":"tmp_rm_demo.txt"}' | ./tools/bin/fs_rm | jq .

# Remove a directory tree
mkdir -p tmp_rm_dir/a/b && touch tmp_rm_dir/a/b/file.txt
echo '{"path":"tmp_rm_dir","recursive":true}' | ./tools/bin/fs_rm | jq .
rm -rf tmp_rm_dir
```

### fs_move tool example
Move (rename or cross-device copy+remove) a repo-relative file. By default, it refuses to overwrite an existing destination unless `overwrite:true` is provided.
```bash
make build-tools

# Create a source file
printf 'payload' > tmp_move_src.txt

# Basic rename when destination does not exist
echo '{"from":"tmp_move_src.txt","to":"tmp_move_dst.txt"}' | ./tools/bin/fs_move | jq .
test ! -e tmp_move_src.txt && test -e tmp_move_dst.txt

# Overwrite behavior: destination exists, require overwrite:true
printf 'old' > tmp_move_dst.txt
printf 'new' > tmp_move_src.txt
echo '{"from":"tmp_move_src.txt","to":"tmp_move_dst.txt","overwrite":true}' | ./tools/bin/fs_move | jq .
grep -qx 'new' tmp_move_dst.txt

rm -f tmp_move_src.txt tmp_move_dst.txt
```

### fs_listdir tool example
List directory entries with optional recursion, glob filtering, and hidden-file control. Returns a stable order (directories first, then files, lexicographic).

```bash
make build-tools

# Create a demo directory tree
mkdir -p tmp_listdir_demo/a b && touch tmp_listdir_demo/.hidden tmp_listdir_demo/a/afile tmp_listdir_demo/bfile

# Non-recursive list (hidden excluded by default)
echo '{"path":"tmp_listdir_demo"}' | ./tools/bin/fs_listdir | jq '.entries | map(.path)'

# Recursive list with globs for only files
jq -n '{path:"tmp_listdir_demo",recursive:true,globs:["**/*"],includeHidden:false}' | ./tools/bin/fs_listdir | jq '.entries | map(select(.type=="file") | .path)'

# Cleanup
rm -rf tmp_listdir_demo
```

### fs_apply_patch tool example
Apply a strict unified diff (no fuzz) to create a new file. Paths must be repo‑relative; absolute paths or parent traversal are rejected. Existing files are not overwritten; repeated applies are idempotent when content matches.

```bash
make build-tools

# Minimal unified diff that creates a new file with two lines
cat > /tmp/demo.diff <<'EOF'
--- /dev/null
+++ b/tmp_patch_demo.txt
@@ -0,0 +1,2 @@
+hello
+world
EOF

# Apply the patch from repository root
jq -n --arg d "$(cat /tmp/demo.diff)" '{unifiedDiff:$d}' | ./tools/bin/fs_apply_patch | jq .

# Verify file content
printf 'hello
world
' | diff -u - tmp_patch_demo.txt && echo OK

# Caution:
# - Only new-file diffs are supported in this slice (--- /dev/null to +++ b/<path>)
# - No fuzzy matching; line endings in the diff are normalized to LF
# - Will fail if the target exists with different content
```

### fs_edit_range tool example
Atomically splice a byte range in a repo‑relative file and return the new SHA‑256.

```bash
make build-tools

# Seed a demo file
printf 'abcdef' > tmp_edit_demo.txt

# Replace bytes [2:4) ("cd") with "XY"
echo -n 'XY' | base64 > b64.txt
jq -n --arg b "$(cat b64.txt)" '{path:"tmp_edit_demo.txt",startByte:2,endByte:4,replacementBase64:$b}' \
  | ./tools/bin/fs_edit_range | jq .

# Verify
cat tmp_edit_demo.txt
# => abXYef

# Cleanup
rm -f tmp_edit_demo.txt b64.txt
```

### fs_stat tool example
Report existence, type, size, mode, modTime, and optional SHA‑256 for a repo‑relative path.

```bash
make build-tools

# Create a demo file
printf 'hello world' > tmp_stat_demo.txt

# Stat with SHA‑256
echo '{"path":"tmp_stat_demo.txt","hash":"sha256"}' | ./tools/bin/fs_stat | jq .

# Cleanup
rm -f tmp_stat_demo.txt
```

### Features
- OpenAI‑compatible `POST /v1/chat/completions` via `net/http` (no SDK)
- Tool manifest `tools.json` using JSON Schema for parameters (see the
  [tools manifest reference](docs/reference/tools-manifest.md))
- Per‑call timeouts; argv‑only execution with JSON stdin/stdout
- Deterministic tool error mapping as JSON (e.g., `{"error":"..."}`)

### Security model
- Tools are an explicit allowlist from `tools.json`
- No shell interpretation; commands executed via argv only
- JSON contract on stdin/stdout; strict timeouts per call
- Treat model output as untrusted input; never pass to a shell

For a deeper analysis of risks, boundaries, and mitigations, see the full threat model at `docs/security/threat-model.md`.

### Unrestricted tools warning
- Enabling certain tools like `exec` grants arbitrary command execution and may allow full network access. Treat this as remote code execution.
- Run the CLI and tools in a sandboxed environment (container/jail/VM) with least privilege. Prefer a throwaway working directory.
- Keep `tools.json` minimal and audited; only include tools you truly need. Review commands and arguments regularly.
- Do not pass secrets via tool arguments. Supply secrets via environment or CI secret stores and ensure logs omit sensitive values.
- Audit log redaction: set `GOAGENT_REDACT` to a comma/semicolon‑separated list of regexes or literals to mask in audit entries. Values of `OAI_API_KEY`/`OPENAI_API_KEY` are always masked if present.

### Troubleshooting
See `docs/runbooks/troubleshooting.md` for common issues and deterministic fixes (missing tool binaries, path validation, timeouts, HTTP errors, and golangci-lint setup), with copy‑paste commands.

### Sequence diagrams
See `docs/diagrams/agentcli-seq.md` for the agent loop and `docs/diagrams/toolbelt-seq.md` for toolbelt interactions.

### Architecture Decision Records
- ADR-0001: Minimal Agent CLI — `docs/adr/0001-minimal-agent-cli.md`
- ADR-0002: Unrestricted toolbelt (files + network) — `docs/adr/0002-unrestricted-toolbelt.md`

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

### Unrestricted examples
See `examples/unrestricted.md` for copy‑paste prompts demonstrating `exec` + file tools to write, build, and run code in a sandboxed environment.
