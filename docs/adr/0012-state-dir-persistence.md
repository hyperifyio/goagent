# ADR-0012: Persist and refine execution state via -state-dir

## Status

Accepted

## Context

We want deterministic, reproducible runs across CLI invocations without adding a database or introducing network/stateful dependencies. Operators frequently iterate on similar prompts and tool configurations; recomputing the pre-stage every time is wasteful and introduces variability. A simple file-based persistence mechanism, controlled explicitly by the user via `-state-dir`, enables:

- Reusing prior refined prompts and settings for faster, stable runs
- Explicit partitioning by scope to avoid cross-contamination
- Offline, testable behavior with predictable artifacts suitable for diffs

Security and privacy constraints require that we do not store raw request/response bodies or secrets by default, and that on-disk files have restrictive permissions.

## Options

- No persistence: always recompute pre-stage; simplest but slow and variable
- Database-backed store: robust but heavy, adds operational complexity
- File-based JSON bundles: lightweight, portable, easy to test/diff

## Decision

Adopt a file-based, append-only persistence under a user-provided `-state-dir`:

- Snapshot files named `state-<RFC3339UTC>-<8charSHA>.json` containing a versioned `StateBundle` (`version:"1"`) per ADR‑0011
- A pointer file `latest.json` with `{version:"1", path, sha256}` pointing to the newest snapshot
- Directory permissions enforced to `0700`; files written with `0600`, using atomic write + fsync + rename
- Partitioning via `scope_key`; default scope is a hash of `(model_id|base_url|toolset_hash)` with optional `-state-scope` override
- Redaction and safety: API keys are redacted upstream; bundles exclude raw bodies; world-writable or non-owned directories are rejected
- Refinement: `-state-refine` with either `-state-refine-text` or `-state-refine-file` produces a new snapshot with `prev_sha` pointing to the previous bundle

See ADR‑0011 for the `StateBundle` schema details.

## Consequences

- Reproducible and faster repeated runs; fewer pre-stage calls when state is restored
- Deterministic artifacts that are easy to inspect and test
- Requires user selection of a secure directory; rejects unsafe permissions
- Disk usage grows with snapshots; users can prune older snapshots manually

## Sequence (Mermaid)

```mermaid
sequenceDiagram
  autonumber
  participant U as User
  participant C as agentcli
  participant FS as -state-dir (filesystem)
  participant LLM as Pre-stage LLM

  rect rgb(245,245,245)
    Note over C,FS: First run (no state)
    U->>C: run with -state-dir
    C->>FS: LoadLatestStateBundle(scope)
    FS-->>C: (none)
    C->>LLM: Pre-stage call (derive prompts/settings)
    LLM-->>C: Harmony messages (validated)
    C->>FS: SaveStateBundle(snapshot)
    FS-->>C: latest.json -> snapshot
    C-->>U: Execute main call with merged config
  end

  rect rgb(245,245,245)
    Note over C,FS: Restore (no refine)
    U->>C: run again with same scope
    C->>FS: LoadLatestStateBundle(scope)
    FS-->>C: bundle
    C-->>U: Skip pre-stage; use restored prompts/settings
  end

  rect rgb(245,245,245)
    Note over C,FS: Refine existing state
    U->>C: run with -state-refine (-state-refine-text|file)
    C->>FS: LoadLatestStateBundle(scope)
    FS-->>C: bundle
    C->>LLM: Pre-stage refine(prompt = refine-input)
    LLM-->>C: refined messages/settings
    C->>FS: SaveStateBundle(new, prev_sha=old)
    FS-->>C: latest.json -> new snapshot
    C-->>U: Proceed with refined config
  end
```

## Usage examples

Restore and reuse prompts/settings across runs:

```bash
./bin/agentcli \
  -prompt "Summarize the repo" \
  -tools ./tools.json \
  -state-dir "$PWD/.agent-state" \
  -debug

# Second run with the same -state-dir and scope will restore and skip pre-stage
./bin/agentcli \
  -prompt "Summarize the repo" \
  -tools ./tools.json \
  -state-dir "$PWD/.agent-state"
```

Dry-run to see intended actions without touching disk:

```bash
./bin/agentcli -prompt "Say ok" -state-dir "$PWD/.agent-state" -dry-run
```

Refine an existing bundle using inline text:

```bash
./bin/agentcli \
  -prompt "Summarize the repo" \
  -state-dir "$PWD/.agent-state" \
  -state-refine \
  -state-refine-text "Tighten temperature to 0.2 and emphasize security notes"
```

Use a custom scope to keep contexts separate:

```bash
./bin/agentcli -prompt "Say ok" -state-dir "$PWD/.agent-state" -state-scope "docs-demo"
```

## Security notes

- Use a private directory owned by the current user. The CLI rejects world-writable or non-owned directories.
- Files are written `0600` and the directory is `0700`. Back up or copy with care.
- Keys and secrets are redacted upstream; raw request/response bodies are not stored by default.

## References

- ADR‑0011: State bundle schema (`docs/adr/0011-state-bundle-schema.md`)
- CLI flags reference (`docs/reference/cli-reference.md`)