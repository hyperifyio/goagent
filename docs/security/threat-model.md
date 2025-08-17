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
- Secret leakage: Avoid printing secrets. Mitigation: supply tokens via environment or CI secrets; do not commit secrets; consider redaction in logs (planned).
- Output confusion: Tools should fail with non-zero exit and machine-readable stderr to map to `{"error": "..."}`. Mitigation: standardize tool error contracts (planned) and keep runner mapping strict.
- Network exposure: `tools/exec.go` is unrestricted. Mitigation: enable only when necessary and document risks.

## Operational guidance
- Run the CLI in a working directory with restricted permissions.
- Review `tools.json` before enabling tools; prefer least privilege.
- Use containers or separate user namespaces to isolate untrusted tools.
- Configure `OAI_API_KEY` via env; never commit credentials.

## References
- See `README.md` Security model for a quick summary.
- ADR-0001 documents the CLI architecture and contracts.
