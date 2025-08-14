# ADR-0001 Minimal Agent CLI

## Context
We want a small, non-interactive CLI that talks to an OpenAI-compatible API (Chat Completions) and can run a small set of explicitly allowed local tools. The CLI must be vendor-agnostic and rely only on `net/http` for the API.

## Options considered
- Go vs Python vs Node: Go chosen for static binary and process control
- SDK vs raw HTTP: raw HTTP to keep provider-agnostic
- Streaming vs simple: no streaming in MVP

## Decision
- Implement a single-shot run loop using POST `/v1/chat/completions`
- Tools are declared in `tools.json` with JSON Schema; invoked via argv with JSON on stdin; output is JSON on stdout
- Per-call timeouts enforced for HTTP and tools

## Consequences
- No streaming; sequential tool execution only in MVP
- Model/tool arguments treated as untrusted input; no shells

## Contracts
- Flags: `-prompt` (required), `-tools` (path), `-system`, `-base-url`, `-api-key`, `-model`, `-max-steps`, `-timeout`, `-temp`, `-debug`
- Tool I/O: stdin receives raw JSON args; stdout returns single-line JSON; on error, CLI maps to `{"error":"..."}`

## Issue
Link to the canonical GitHub issue once created.

## Diagram
See `docs/diagrams/agentcli-seq.md` for the sequence diagram illustrating the loop.

## Related documentation
See the docs index at `docs/README.md` for navigation to related guides and diagrams. For the tool manifest contract, refer to `docs/reference/tools-manifest.md`.
