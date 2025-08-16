# ADR-0003 Toolchain & Lint Policy (Go + golangci-lint)

## Context
Reliable builds and linting require deterministic tool versions across developer machines and CI. We have seen failures such as “golangci-lint: unsupported export data (internal/goarch version: 2)” when the linter and Go toolchain are mismatched. The project already documents gates (lint, vet, format, tests) and needs a pinned policy so results are reproducible on Linux, macOS, and Windows.

## Options considered
- Floating Go version in CI vs using the module’s `go.mod` directive
- Using `golangci-lint` GitHub Action vs invoking a pinned binary from the repository workflow
- Installing `golangci-lint` ad‑hoc on PATH vs a deterministic location (`./bin`) or `$(go env GOPATH)/bin`
- Allowing linter auto‑updates vs pinning to a known‑good version and bumping deliberately

## Decision
- CI and local workflows must use the Go version declared by `go.mod`.
  - CI config uses `actions/setup-go@v5` with `go-version-file: go.mod` and prints `go version` for traceability.
- Linting is performed with a pinned `golangci-lint` version known to be compatible with the current Go line.
  - The Makefile installs `golangci-lint` deterministically (platform‑aware) and invokes that pinned binary, not whatever happens to be on PATH.
- Local `make lint` runs fast‑fail prechecks (tool presence, minimum version), runs formatting checks, vet, and the linter, and enforces repository hygiene checks (`check-tools-paths`, `verify-manifest-paths`).
- Upgrades to the Go toolchain or `golangci-lint` occur together via a PR that updates `go.mod`, the Makefile pin, and CI, with a passing green build.

## Consequences
- Deterministic lint results across OSes; reduced “export data” and analyzer mismatch errors.
- Slightly slower first `make lint` due to on‑demand installation of the pinned linter; subsequent runs are fast.
- Version bumps require coordination but are safer and traceable.

## Rollback
- Revert the Makefile and CI workflow edits to the previous known‑good pins. Because the policy is additive, reverting is low‑risk.

## Implementation notes
- CI prints tool versions (Go and golangci-lint) in logs for auditability.
- `make lint` includes repository‑specific checks (`check-tools-paths`, `verify-manifest-paths`) to keep tool layout consistent.
- Developer docs and runbooks include a section for the common failure mode “unsupported export data” with explicit resolution steps.

## Issue
Link to the canonical GitHub issue once created.

## Related documentation
- Docs index: `docs/README.md`
- CI quality gates: `docs/operations/ci-quality-gates.md`
- Existing ADRs: `docs/adr/0001-minimal-agent-cli.md`, `docs/adr/0002-unrestricted-toolbelt.md`
