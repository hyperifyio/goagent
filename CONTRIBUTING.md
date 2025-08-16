## Contributing to goagent

Thank you for your interest in improving `goagent`! Contributions are welcome.

### Ways to contribute
- Report bugs and request features via the issue tracker
- Improve documentation and examples
- Submit pull requests for fixes and enhancements

### Development setup
- Prerequisites: Go 1.24+, `ripgrep` (rg), `golangci-lint`
- Recommended commands from a clean clone:
```bash
make tidy build build-tools
make test
make lint
```

### Workflow
1. Fork the repository and create a feature branch from `develop`
2. Write tests first (unit/integration). Keep tests deterministic and offline
3. Implement the smallest change to make tests pass; keep code clear and readable
4. Run quality gates locally:
```bash
make test
make lint
```
5. Open a pull request that:
   - Explains the intent and links to the canonical GitHub issue
   - Describes user-facing changes and risks
   - Updates docs as needed

### Coding standards
- Go formatting: run `gofmt -s` (enforced by `make lint`)
- Linting: `golangci-lint run` (invoked by `make lint`)
- Avoid deep nesting; prefer early returns and clear error handling
- Add tests for all changed behaviors; maintain or improve coverage

### Tools layout conventions
- Tool sources live under `tools/cmd/NAME/*.go`
- Tool binaries are built to `tools/bin/NAME` (or `NAME.exe` on Windows)
- Use `make build-tools` or `make build-tool NAME=<name>` to build
- Validate paths and manifests locally:
```bash
make check-tools-paths
make verify-manifest-paths
```

### Commit messages
- Keep the subject concise; focus on the why, not just the what
- Reference the issue with a full URL in either the PR description or commit body

### Code of Conduct
By participating, you agree to abide by the project Code of Conduct. See `CODE_OF_CONDUCT.md`.

### Getting help
If you are blocked while contributing, open a draft PR or an issue with details. We'll help you move forward.
