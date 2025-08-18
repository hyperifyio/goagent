# ADR-0005: Harmony pre-processing and channel-aware output

Status: Accepted

Date: 2025-08-18

Context

The agent performs a lightweight pre-processing stage ("pre-stage") before the main chat completion to refine inputs deterministically. This stage may invoke read-only built-in tools in parallel, validates message sequencing, records structured audit logs with stage metadata, and merges roles before the main call. Output channels are routed to stdout/stderr according to explicit rules to keep the CLI output clean and stable.

Decision

- Introduce a pre-stage HTTP call that uses its own config block (`-prep-*` flags and `OAI_PREP_*` env) while inheriting sane defaults from the main call when unset.
- Restrict pre-stage tools to in-process read-only adapters by default; allow external tools only when `-prep-tools-allow-external` is set, honoring a separate manifest via `-prep-tools` with the same validations as the main manifest.
- Execute multiple tool calls in parallel during pre-stage; enforce one `role:"tool"` message per `tool_call_id` and validate the message sequence using the existing validator.
- Emit NDJSON audit entries with `stage:"prep"`, timings, and idempotency key; include pre-stage config in `-print-config`.
- Harmonize channel printing: by default, print only `assistant{channel:"final"}` to stdout; print `critic` and `confidence` to stderr with `-verbose`; dump raw JSON with `-debug`. Maintain the same routing for messages produced after pre-stage.
- Cache pre-stage results using a key derived from inputs, model/endpoint, sampling knobs, retries, and the tool set or manifest content hash; honor `-prep-cache-bust`.

Consequences

- Pre-stage failures are fail-open: the CLI logs a single warning and proceeds with the original messages.
- Auditability improves with consistent stage tagging; docs and CLI help reflect the knobs and precedence rules.
- Deterministic output routing prevents interleaving JSON with human-readable output by default.

References

- Sequence diagram: [docs/diagrams/harmony-prep-seq.md](../diagrams/harmony-prep-seq.md)
- Related policy: [ADR-0004: Default LLM Call Policy](0004-default-llm-policy.md)

Mermaid sequence (summary)

```mermaid
sequenceDiagram
  participant CLI as agentcli
  participant API as Chat Completions API
  participant TOOLS as Pre-stage tools (read-only)

  CLI->>API: POST /v1/chat/completions (pre-stage)
  API-->>CLI: assistant tool_calls[]
  par Parallel tool calls
    CLI->>TOOLS: invoke built-in tool
    TOOLS-->>CLI: JSON result
  end
  CLI->>CLI: ValidateMessageSequence + audit {stage:"prep"}
  CLI->>CLI: Merge roles; route channels
  CLI->>API: POST /v1/chat/completions (final)
  API-->>CLI: assistant final
  CLI-->>CLI: Print final; verbose channels to stderr
```