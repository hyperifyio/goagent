# PR Plan: ordered slicing from develop to main

This plan enumerates independent feature PRs sliced from develop to main in minimal, reviewable units. Each PR is based on main, transfers only scoped paths from develop, and includes tests/docs as applicable. Sequencing keeps diffs small and CI green.

## Order
1. Scaffold repository (no code)
2. CLI: minimal entrypoint (main.go only)
3. Flags & help (no network)
4. OpenAI-compatible HTTP client (types + client, no integration)
5. Model defaults & capability map (serialization only)
6. Tools manifest loader
7. Secure tool runner (no sandbox yet)
8. Baseline docs & diagrams
9. Example tool: get_time + tools.json + Makefile wiring
10. Quickstart README runnable
11. Minimal unit tests enabling (core only)
12. One PR per remaining tool (each isolated)
13. Error-contract standardization (one tool per PR)
14. Makefile wiring (TOOLS list, build-tools/clean)
15. Scripts and CI utilities
16. Security & runbooks
17. ADRs (one per PR unless trivial)
18. Diagrams (grouped if tightly related)
19. CLI feature increments (small, focused)
20. Parallel tool calls (main loop) isolated
21. Audit logs and redaction (split HTTP vs tools)
22. Research toolbelt (one PR per tool)
23. Integration tests (minimal e2e)
24. README finalization and examples
25. Post-migration cleanup

## Branch naming
- pr/01-scaffold
- pr/02-cli-main
- pr/03-flags-help
- pr/04-oai-client
- pr/05-model-defaults
- pr/06-tools-manifest
- pr/07-tool-runner
- pr/09-docs-diagrams
- pr/10-get-time
- pr/11-quickstart
- pr/12-core-tests
- pr/tool-<name>
- pr/makefile-tools
- pr/scripts
- pr/security-runbooks
- pr/adr-XXXX
- pr/diagrams
- pr/parallel-tool-calls

## Notes
- Rebase each feature branch on latest origin/main before pushing.
- Keep PRs independent; extract shared prerequisites into tiny PRs.
- Tests: run only scope-relevant suites per PR; keep CI green.
