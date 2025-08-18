# Documentation Index

This docs index helps you navigate architecture notes and diagrams.

- ADR-0001: Minimal Agent CLI — design context, decisions, and contracts.
  - Link: [docs/adr/0001-minimal-agent-cli.md](adr/0001-minimal-agent-cli.md)
- ADR-0002: Unrestricted toolbelt (files + network) — risks, contracts, and guidance.
  - Link: [docs/adr/0002-unrestricted-toolbelt.md](adr/0002-unrestricted-toolbelt.md)
- ADR-0003: Toolchain & Lint Policy (Go + golangci-lint) — pin Go via go.mod and a known-good golangci-lint; CI and local workflows align.
  - Link: [docs/adr/0003-toolchain-and-lint-policy.md](adr/0003-toolchain-and-lint-policy.md)
- ADR-0004: Default LLM Call Policy — default temperature 1.0, capability-based omission, one-knob rule, and observability fields.
  - Link: [docs/adr/0004-default-llm-policy.md](adr/0004-default-llm-policy.md)
- ADR-0005: Harmony pre-processing and channel-aware output — pre-stage HTTP call, parallel read-only tools, validator/audit with stage tags, and deterministic channel routing.
  - Link: [docs/adr/0005-harmony-pre-processing-and-channel-aware-output.md](adr/0005-harmony-pre-processing-and-channel-aware-output.md)
 - ADR-0006: Image generation tool (img_create) — minimal Images API integration, repo‑relative saves, env passthrough, and transcript hygiene.
   - Link: [docs/adr/0006-image-generation-tool-img_create.md](adr/0006-image-generation-tool-img_create.md)
 - ADR-0010: Adopt SearXNG & network research toolbelt (CLI-only) — credible web discovery with provenance via SearXNG and a safe, CLI-only toolbelt.
   - Link: [docs/adr/0010-research-tools-searxng.md](adr/0010-research-tools-searxng.md)
- Sequence diagrams: agent flow and toolbelt interactions.
  - Link: [docs/diagrams/agentcli-seq.md](diagrams/agentcli-seq.md)
  - Link: [docs/diagrams/toolbelt-seq.md](diagrams/toolbelt-seq.md)
  - Link: [docs/diagrams/harmony-prep-seq.md](diagrams/harmony-prep-seq.md)

- Architecture: Module boundaries and allowed imports between layers.
  - Link: [docs/architecture/module-boundaries.md](architecture/module-boundaries.md)

- Tools manifest reference: precise `tools.json` schema and mapping to OpenAI tools.
  - Link: [docs/reference/tools-manifest.md](reference/tools-manifest.md)
 
 - CLI reference: complete flag list, env precedence, exit codes.
   - Link: [docs/reference/cli-reference.md](reference/cli-reference.md)

- Tool reference: Image generation tool (`img_create`).
  - Link: [docs/reference/img_create.md](reference/img_create.md)
- Tool reference: HTTP fetch (`http_fetch`).
  - Link: [docs/reference/http_fetch.md](reference/http_fetch.md)
- Tool reference: SearXNG search (`searxng_search`).
  - Link: [docs/reference/searxng_search.md](reference/searxng_search.md)
 - Tool reference: PDF extract (`pdf_extract`).
   - Link: [docs/reference/pdf_extract.md](reference/pdf_extract.md)

- Security: Threat model and trust boundaries.
  - Link: [docs/security/threat-model.md](security/threat-model.md)

- Runbooks: Troubleshooting common errors and fixes.
  - Link: [docs/runbooks/troubleshooting.md](runbooks/troubleshooting.md)

- Migrations: Tools layout (legacy → canonical `tools/cmd/*` + `tools/bin/*`).
  - Link: [docs/migrations/tools-layout.md](migrations/tools-layout.md)

Additional guides will be added here as they are created.

Model parameter compatibility

Some reasoning-oriented models may not accept sampling parameters. The agent omits `temperature` automatically for such models while keeping the default of 1.0 for compatible families (e.g., GPT-5 variants). This avoids API errors and preserves expected defaults where applicable.
