# Tools Manifest Reference (tools.json)

This page documents the `tools.json` schema consumed by `agentcli` and how it is translated into OpenAI-compatible tool definitions. It reflects the current implementation in `internal/tools/manifest.go` and the unit tests in `internal/tools/manifest_test.go`.

## Schema

Root object:
```json
{
  "tools": [ ToolSpec, ... ]
}
```

ToolSpec fields:
- `name` (string, required): Unique tool name. Must be non-empty and unique across the manifest.
- `description` (string, optional): Short human description.
- `schema` (object, optional): JSON Schema for the tool parameters. This is passed through to the model as `parameters` in the OpenAI "function" tool.
- `command` (array of string, required): Argv vector. First element is the program path (relative or absolute); subsequent elements are fixed args. When relative, it MUST start with `./tools/bin/NAME` (use `.exe` on Windows). Relative paths are resolved against the directory containing this `tools.json` (not the process working directory). The runner will execute this program and write the function call JSON arguments to stdin.
- `timeoutSec` (integer, optional): Per-call timeout override in seconds. If omitted, the CLI's `-timeout` applies.
- `envPassthrough` (array of string, optional): Allowlist of environment variable names to pass from the parent process to the tool. Names are normalized to uppercase and must match the regex `[A-Z_][A-Z0-9_]*`. Duplicates are removed preserving first occurrence. The runner always sets a minimal base environment (e.g., `PATH`, `HOME`) and augments it with only these keys if present in the parent. For observability, the audit log records only the names of keys passed (as `envKeys`), never their values.

Notes:
- Validation errors are precise and include the offending index/name.
- `command` must have at least one element (the program).
- Names must be unique (duplicates are rejected).

## OpenAI tool mapping
Each manifest entry is exported as an OpenAI tool of type `function`:
```json
{
  "type": "function",
  "function": {
    "name": "<name>",
    "description": "<description>",
    "parameters": { /* schema as provided */ }
  }
}
```

## Minimal example
```json
{
  "tools": [
    {
      "name": "get_time",
      "description": "Get current time for an IANA timezone",
      "schema": {
        "type": "object",
        "properties": {
          "timezone": {"type": "string", "description": "IANA timezone, e.g. Europe/Helsinki"},
          "tz": {"type": "string", "description": "Alias for timezone (deprecated)"}
        },
        "required": ["timezone"],
        "additionalProperties": false
      },
      "command": ["./tools/bin/get_time"],
      "timeoutSec": 5,
      "envPassthrough": ["TZ", "OAI_HTTP_TIMEOUT"]
    }
  ]
}
```

On Windows, use the `.exe` suffix for the tool binary:

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

## Common mistakes
- Missing `name`: error `tool[i]: name is required`.
- Duplicate `name`: error `tool[i] "<name>": duplicate name`.
- Empty `command`: error `tool[i] "<name>": command must have at least program name`.
- Relative `command[0]` not using the canonical bin prefix: error `tool[i] "<name>": relative command[0] must start with ./tools/bin/` (absolute paths are allowed for tests). This ensures tools are invoked from `./tools/bin/NAME` and are then resolved relative to the manifest directory.
- Relative `command[0]` that normalizes to escape the tools bin directory (e.g., `./tools/bin/../hack`): error `tool[i] "<name>": command[0] escapes ./tools/bin after normalization (got "./tools/bin/../hack" -> "./tools/hack")`.
- Invalid `envPassthrough` entry (e.g., `"OAI-API-KEY"` or `"1BAD"`): error `tool[i] "<name>": envPassthrough[j]: invalid name "..." (must match [A-Z_][A-Z0-9_]*)`.

## Execution model
- The assistant provides JSON arguments for the tool call. `agentcli` passes that JSON to the tool's stdin verbatim.
- Tools must print a single-line JSON result to stdout. On failure, print a single-line JSON error to stderr and exit non-zero. The agent maps failures to `{"error":"..."}` content for the model.
- Environment is scrubbed to a minimal allowlist (PATH, HOME) and optionally augmented by `envPassthrough`. No shell is invoked; commands are executed via argv.

## Versioning
This document describes the current stable behavior. Backward-incompatible changes will be documented in the changelog and ADRs.
