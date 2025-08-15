# ADR-0002: Unrestricted toolbelt (files + network)

## Status
Accepted

## Context
The project exposes an extensible toolbelt that the model may invoke. Some tools (e.g., `exec`, file system read/write/move/rm, search) provide broad capabilities that, when enabled, amount to remote code execution with potential network access. We must document the decision, contracts, and consequences so users enable these tools deliberately and operate them safely.

## Decision
Introduce an "unrestricted toolbelt" set comprising file-system utilities (`fs_read_file`, `fs_write_file`, `fs_append_file`, `fs_move`, `fs_rm`, `fs_search`, future list ops) and the generic `exec` tool. The CLI will advertise these tools via the OpenAI-compatible tools array, and execute them argv-only with JSON stdin/stdout and strict per-call timeouts.

## Consequences
- Enabling the unrestricted toolbelt is opt-in via `tools.json` and carries RCE risk.
- Operators must sandbox execution (container/jail/VM), run with least privilege, and use a throwaway working directory.
- Secrets must be injected via environment/CI secrets and never committed; logs should be redacted where configured.
- The runner maps tool failures to a deterministic JSON error contract and applies timeouts and a minimal environment.
- Documentation and examples clearly flag the risks and provide copy-paste usage guarded by warnings.

## JSON contracts (summary)
- Exec tool stdin:
  - `{ "cmd": "string", "args": ["..."], "cwd?": "string", "env?": {"K":"V"}, "stdin?": "string", "timeoutSec?": int }`
  - Stdout (single line JSON): `{ "exitCode": int, "stdout": "string", "stderr": "string", "durationMs": int }`
- File tools use repo-relative paths. Writes are atomic (temp+rename) where applicable; rm supports recursive and force flags; search supports literal/regex/globs with truncation.

## Links
- Threat model: `../security/threat-model.md`
- Tools reference: `../reference/tools-manifest.md`
- Sequence diagrams:
  - Agent loop: `../diagrams/agentcli-seq.md`
  - Toolbelt interactions: `../diagrams/toolbelt-seq.md`

## Alternatives considered
- Disallow `exec` entirely: safer by default, but blocks many real workflows. Decided to allow but keep opt-in and loudly documented.
- Shell execution: rejected due to injection risk; we use argv-only process spawn.

## Notes
Changes to tool contracts must update this ADR, the README, and relevant tests. PRs modifying tool behavior should reference this ADR-0002 in their description.
