* [ ] Configure GitHub Actions CI to enforce quality gates: `.github/workflows/ci.yml` with jobs on push/PR that set up Go 1.21+, cache modules, run `make lint`, `go test -race -cover ./...`, and `make build build-tools`; upload `bin/agentcli` as an artifact; set `permissions: contents: read` and fail the build on any non-zero step; matrix over `os: [ubuntu-latest, macos-latest, windows-latest]`.
* [ ] Add release workflow for static binaries and checksums: `.github/workflows/release.yml` triggered on tags like `v*`; build `agentcli` for `linux,darwin,windows` × `amd64,arm64` with `CGO_ENABLED=0`; name outputs `agentcli_<os>_<arch>` (Windows `.exe`); generate `SHA256SUMS` and `SHA256SUMS.sig` (optional GPG); create GitHub Release and upload artifacts and checksums; document in README how to download and verify.
* [ ] Tag and publish `v0.1.0` once CI is green and docs/tests are complete: ensure `README.md` has usage, examples, and limitations (no streaming, sequential tool calls only); ADR-0001 and sequence diagram present; unit and integration tests passing; create annotated tag `git tag -a v0.1.0 -m "MVP non-interactive agent CLI with OpenAI-compatible tools"` and `git push --tags`.
* [ ] Add lint/type checks and formatting gates: include `.golangci.yml` enabling `govet`, `gofmt`, `errcheck`, `staticcheck`, `gocyclo` (with reasonable thresholds), `gosimple`; add `make lint` that installs `golangci-lint` if missing and runs it; ensure `go vet ./...` and `gofmt -s -l` produce no output; fail CI if any lints fail.

## Review 2025-08-14

* [ ] Purge committed tool binaries and add .gitignore — stop shipping build artifacts
  Context: Compiled tool binaries are tracked under `tools/`, which bloats the repo and risks stale executables being committed. These should be build outputs only.
  Evidence: Tracked files include `tools/get_time`, `tools/fs_read_file`, `tools/exec/exec`, `tools/fs_rm/fs_rm`, `tools/fs_write_file/fs_write_file`, `tools/fs_move/fs_move` (from `git ls-files -v tools`); `Makefile` builds these paths and `clean` omits `tools/fs_search`.
  Proposed change: Remove tracked binaries with `git rm` (do not delete sources), add a root `.gitignore` ignoring `bin/` and `tools/**` binaries (no-ext and `*.exe`), and update `make clean` to also remove `tools/fs_search`. Optionally standardize tool outputs to `tools/bin/<name>` to avoid name collisions.
  Affected scope: `tools/*` (tracked binaries only), `.gitignore`, `Makefile` `clean` target.
  Risk and blast radius: Low; accidental removal of sources is the main risk—mitigate by targeting only extensionless executables in known paths.
  Effort estimate: S (≤1 hour).
  Dependencies: None.
  DoD for this task: Follow TDD where applicable (e.g., a lightweight CI check that fails if binaries are tracked); tests remain deterministic and green locally and in CI; coverage does not decrease; all quality gates pass (format, lint, vet, security/secret scans) with no new findings; builds are reproducible; documentation for contributing/build is updated as needed; backward compatibility unchanged; at least one peer review completed with comments resolved; PR and commits explain intent and link to the canonical issue URL; any exception is pre-approved in the linked issue.
  Verification: `make build build-tools` produces untracked binaries; `git status` is clean; `make clean` removes all built tool binaries including `tools/fs_search`; `go test ./...` remains green.
  Rollback or mitigation: Revert the commit to restore previous state; binaries can be rebuilt via `make build-tools`.
  Traceability: Link PR to issue URL (e.g., https://github.com/hyperifyio/goagent/issues/NNN) describing artifact hygiene.

* [ ] Remove unused `logs/` directory — reduce repository clutter
  Context: An empty `logs/` directory exists but is unused by code or scripts.
  Evidence: Directory `logs/`; no references found by search for `logs/` in repository.
  Proposed change: Remove `logs/` with `git rm -r logs/`. If future logging is required, create directories on demand at runtime instead of committing empty folders.
  Affected scope: `logs/`.
  Risk and blast radius: Minimal; affects no runtime paths today.
  Effort estimate: XS (≤10 minutes).
  Dependencies: None.
  DoD for this task: All tests and quality gates pass unchanged; documentation and scripts contain no references to `logs/`; PR links to the tracking issue with clear intent; peer review completed.
  Verification: `grep -R "logs/"` returns no functional references; `go test ./...` green; repository tree no longer contains `logs/`.
  Rollback or mitigation: Recreate directory if later needed; prefer runtime creation.
  Traceability: Link PR to issue URL (e.g., https://github.com/hyperifyio/goagent/issues/NNN) documenting the removal.

* [ ] Align Go toolchain version — consistent go.mod and docs/CI
  Context: The documented minimum Go version differs from the module directive.
  Evidence: `go.mod` declares `go 1.24.6` while `README.md` states prerequisites “Go 1.21+”.
  Proposed change: Pick a single minimum supported version (e.g., Go 1.21) and update `go.mod` `go` directive accordingly; ensure CI matrix includes that version and newer; update README to match.
  Affected scope: `go.mod`, `README.md`, CI workflow (when added).
  Risk and blast radius: Low; ensure no APIs newer than the chosen baseline are used.
  Effort estimate: S (≤1 hour).
  Dependencies: CI workflow task (may be executed independently, but CI should reflect the decision once present).
  DoD for this task: Tests pass locally with the chosen baseline version; quality gates pass; documentation updated; PR links to issue and explains the rationale; peer review completed.
  Verification: `go test ./...` succeeds under the baseline toolchain and in CI; `go env GOVERSION` in CI shows expected versions.
  Rollback or mitigation: Revert `go.mod` change if incompatibilities arise.
  Traceability: Link PR to issue URL (e.g., https://github.com/hyperifyio/goagent/issues/NNN) about toolchain alignment.

* [ ] Update README build instructions and TOC formatting — remove stale notes and improve clarity
  Context: README contains a stale note implying `fs_mkdirp` is not part of the aggregated build and minor TOC indentation inconsistencies.
  Evidence: In `README.md`, the `fs_mkdirp` example says “until aggregated in Makefile” while `Makefile` already builds it; TOC entries around “Exec tool example” and “fs_rm tool example” have inconsistent indentation.
  Proposed change: Replace the stale note with `make build-tools` usage and normalize TOC indentation and anchors for consistency.
  Affected scope: `README.md` only.
  Risk and blast radius: None; documentation-only.
  Effort estimate: XS (≤20 minutes).
  Dependencies: None.
  DoD for this task: Docs lint/rendering OK on GitHub; examples remain correct and runnable; PR links to issue and passes all quality gates; peer review completed.
  Verification: Manually run the updated README commands; ensure TOC links navigate correctly.
  Rollback or mitigation: Revert the doc changes if needed.
  Traceability: Link PR to issue URL (e.g., https://github.com/hyperifyio/goagent/issues/NNN) for docs cleanup.

* [ ] Set a User-Agent on API requests — improve observability and supportability
  Context: Outgoing HTTP requests to the Chat Completions API do not set a `User-Agent`, reducing traceability in server logs.
  Evidence: `internal/oai/client.go` sets `Content-Type` and optional `Authorization` headers but not `User-Agent`.
  Proposed change: Add a `User-Agent: goagent/<version>` header in the OpenAI client; expose a package variable for version; add a unit test asserting the header is present.
  Affected scope: `internal/oai` client and tests; optional version variable in `cmd/agentcli`.
  Risk and blast radius: Low; additive header.
  Effort estimate: S (≤1 hour).
  Dependencies: None.
  DoD for this task: Unit test added; all tests and quality gates pass; documentation updated if needed; PR links to issue and is peer reviewed.
  Verification: Test server captures and asserts `User-Agent`; manual run shows header present.
  Rollback or mitigation: Revert header addition if it causes issues.
  Traceability: Link PR to issue URL (e.g., https://github.com/hyperifyio/goagent/issues/NNN) about observability.

