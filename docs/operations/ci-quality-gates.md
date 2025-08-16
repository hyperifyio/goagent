# CI quality gates

This repository enforces reproducible builds, formatting, lint, static analysis, tests, and path hygiene via the Makefile and GitHub Actions.

- Timeouts:
  - HTTP requests use `-http-timeout` (can be set via `OAI_HTTP_TIMEOUT` in CI). Keep between 60–120s unless tests require less.
  - Tools use `-tool-timeout`, with per-tool `timeoutSec` in `tools.json` taking precedence.
- Lint: `make lint` (pinned golangci-lint) + `make check-tools-paths` + `make verify-manifest-paths`.
- Tests: `go test ./...` run offline with fakes; integration tests exercise tool invocation end-to-end.
- Reproducible builds: `make build build-tools` with `-trimpath` and stripped ldflags; artifacts ignored by Git.

In test scenarios that validate timeouts, prefer small values and deterministic sleeps in fakes to keep CI fast. Ensure such tests are isolated and do not introduce flakiness.
