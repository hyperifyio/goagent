# Threat Model

This document expands on the security posture, trust boundaries, and recommended mitigations for `goagent`.

## Scope
- Non-interactive CLI (`cmd/agentcli`) that communicates with an OpenAI-compatible API and executes local tool binaries declared in `tools.json`.
- Local tools are executed via argv only, with JSON on stdin and JSON on stdout/stderr.

## Trust boundaries
- Untrusted: model outputs, tool inputs from the model, remote API, and any data received over the network.
- Trusted (to the extent configured): the `tools.json` manifest, local tool binaries you build and enable, and the user-provided flags/env.

## Key risks and mitigations
- Command execution risk: Tools run processes. Mitigation: explicit allowlist (`tools.json`), argv-only (no shell), minimal environment, per-call timeouts, and repo-relative file paths for fs tools. For pre-stage, external tools are disabled by default; only in-process read-only adapters are exposed unless `-prep-tools-allow-external` is set.
- Prompt injection and tool abuse: Model may request dangerous operations. Mitigation: keep tool set minimal, prefer read-only tools (pre-stage default), and require human review for high-risk prompts. Consider running tools under containers/jails.
- Secret leakage: Avoid printing secrets. Mitigation: supply tokens via environment or CI secrets; do not commit secrets; redaction is implemented for audit logs and tool runner so secret values are never recorded.
- Output confusion: Tools should fail with non-zero exit and machine-readable stderr to map to `{"error": "..."}`. Mitigation: standardize tool error contracts (planned) and keep runner mapping strict.
- Network exposure: `tools/exec.go` is unrestricted. Mitigation: enable only when necessary and document risks.

## Environment variable passthrough (envPassthrough)

Only an explicit allowlist of environment variables is passed from the agent to child tool processes. This minimizes ambient authority and reduces risk of accidental leakage.

- Allowed keys: `OAI_API_KEY`, `OAI_BASE_URL`, `OAI_IMAGE_BASE_URL`, `OAI_HTTP_TIMEOUT`.
- Rationale: tools that make OpenAI-compatible HTTP requests need endpoint, key, and timeout settings to operate; everything else remains isolated.
- Redaction: audit logs and structured logs include the variable names but never their values.
- Configuration surface: per-tool allowlist is declared in `tools.json` under `envPassthrough`; the runner builds the child environment as `PATH,HOME` plus only those keys if present in the parent process.

## Tool privacy: img_create

The `img_create` tool interacts with the Images API and may handle large base64 payloads. Privacy and transcript hygiene are enforced by default:

- Prompts and base64 image data are not logged in audit logs or human-readable output by default. When returning base64 to the agent, the tool emits a hint that content was elided unless an explicit debug flag is enabled.
- When saving images, files are written only under a repository-relative `save.dir` path that must resolve inside the current repo; path traversal and escapes are rejected.
- Standardized stderr JSON is used for errors, avoiding accidental leakage through free-form logs.

## Operational guidance
- Run the CLI in a working directory with restricted permissions.
- Review `tools.json` before enabling tools; prefer least privilege.
- Use containers or separate user namespaces to isolate untrusted tools.
- Configure `OAI_API_KEY` via env; never commit credentials.

## References
- See `README.md` Security model for a quick summary.
- ADR-0001 documents the CLI architecture and contracts.
