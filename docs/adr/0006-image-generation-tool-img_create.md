# ADR-0006: Image generation tool (img_create)

Status: Accepted

Date: 2025-08-18

Context

The project added a minimal `img_create` tool that integrates with an OpenAI-compatible Images API to generate PNG files deterministically from a prompt. The tool is invoked by the agent via a tools manifest and can also be run directly. It enforces repository‑relative output paths, rejects path escapes, and defaults to saving decoded PNGs to disk to avoid base64 bloat in transcripts unless explicitly requested.

Decision

- Provide a standalone CLI at `tools/cmd/img_create/img_create.go` built into `tools/bin/img_create` via `make build-tools`.
- Define stdin contract and output schema; default to saving files under a repo‑relative `save.dir` with stable names and SHA‑256 reporting.
- Enforce cross‑platform path normalization and strict repo‑relative validation; reject `..` escapes and absolute paths.
- Implement retries with sane timeouts; prefer small, deterministic tests using `httptest.Server` with no external network.
- Support optional `extras` pass‑through for forward compatibility while validating and sanitizing inputs.
- Never log secrets or large base64 by default; print concise JSON and redact or elide sensitive/large payloads unless a debug flag is set.
- Allow a constrained environment to child processes via `envPassthrough` validation and runner enforcement.

Consequences

- Users can generate images deterministically in CI and locally without network flakiness by using mocks in tests.
- Auditability and safety improve via repo‑relative writes, SHA‑256 reporting, and redaction.
- Documentation includes a focused reference and examples; README and CLI reference link to this ADR.

References

- Tool source: [tools/cmd/img_create/img_create.go](../../tools/cmd/img_create/img_create.go)
- Tool reference: [docs/reference/img_create.md](../reference/img_create.md)
- Tools manifest schema and validation: [internal/tools/manifest.go](../../internal/tools/manifest.go)
- Tool runner env pass‑through and auditing: [internal/tools/runner.go](../../internal/tools/runner.go)
- Sequence diagram: [docs/diagrams/toolbelt-seq.md](../diagrams/toolbelt-seq.md)
- Related ADR: [ADR-0004: Default LLM Call Policy](0004-default-llm-policy.md), [ADR-0005: Harmony pre-processing and channel-aware output](0005-harmony-pre-processing-and-channel-aware-output.md)
