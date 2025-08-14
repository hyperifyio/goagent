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
- `command` (array of string, required): Argv vector. First element is the program path (relative or absolute); subsequent elements are fixed args. The runner will execute this program and write the function call JSON arguments to stdin.
- `timeoutSec` (integer, optional): Per-call timeout override in seconds. If omitted, the CLI's `-timeout` applies.

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
        "properties": {"tz": {"type": "string"}},
        "required": ["tz"],
        "additionalProperties": false
      },
      "command": ["./tools/get_time"],
      "timeoutSec": 5
    }
  ]
}
```

## Common mistakes
- Missing `name`: error `tool[i]: name is required`.
- Duplicate `name`: error `tool[i] "<name>": duplicate name`.
- Empty `command`: error `tool[i] "<name>": command must have at least program name`.

## Execution model
- The assistant provides JSON arguments for the tool call. `agentcli` passes that JSON to the tool's stdin verbatim.
- Tools must print a single-line JSON result to stdout. On failure, print a single-line JSON error to stderr and exit non-zero. The agent maps failures to `{"error":"..."}` content for the model.
- Environment is scrubbed to a minimal allowlist (PATH, HOME). No shell is invoked; commands are executed via argv.

## Versioning
This document describes the current stable behavior. Backward-incompatible changes will be documented in the changelog and ADRs.
