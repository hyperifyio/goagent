# ADR-0011: Persist execution state as versioned file bundles

## Status

Accepted

## Context

We need reproducible, resumable runs across CLI invocations without adding a DB. A file-based, append-only bundle under a user-provided `-state-dir` offers portability and testability.

## Decision

Introduce a versioned JSON schema (`version: "1"`) named `StateBundle` with fields:

- `version`: fixed string "1"
- `created_at`: RFC3339 UTC
- `tool_version`: CLI/tool semantic version or git describe
- `model_id`, `base_url`: effective backend configuration
- `toolset_hash`: hash of enabled tool specs
- `scope_key`: partition key to separate incompatible contexts
- `prompts`: `{system, developer,...}` strings only (no secrets)
- `prep_settings`, `context`, `tool_caps`, `custom`: JSON-serializable objects
- `source_hash`: SHA-256 over `(model_id|base_url|toolset_hash|scope_key)`
- `prev_sha` (optional): parent pointer when refining

Pointer file `latest.json` stores `{version:"1", path:"state-*.json", sha256}`. Snapshot files are named `state-<RFC3339UTC>-<8charSHA>.json` with perms 0600; directory perms 0700.

## Consequences

- Simple to save/load and diff in tests
- Forward-compatible via `version`
- No raw request/response bodies are stored by default; keys must be redacted upstream

## Verification

Unit tests cover schema validation and hashing in `internal/state/schema_test.go`. Future changes will add atomic save/load and scope handling.
