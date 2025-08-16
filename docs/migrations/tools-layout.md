# Tools layout migration: from legacy `tools/*` to canonical `tools/cmd/*` + `tools/bin/*`

This guide explains the ongoing migration that standardizes tool source and binary locations.

## Why migrate
- Consistent, unique binary names under `tools/bin/NAME` simplify manifests and docs
- Clear source location under `tools/cmd/NAME/*.go` enables per‑tool packages and tests
- Hygiene checks in `Makefile` enforce paths to avoid drift

## Canonical layout
- Source: `tools/cmd/NAME/*.go`
- Binary (Unix/macOS): `tools/bin/NAME`
- Binary (Windows): `tools/bin/NAME.exe`

## Building tools
- All tools: `make build-tools`
- Single tool: `make build-tool NAME=<name>`

Binaries are emitted to `tools/bin/` regardless of OS. On Windows, a `.exe` suffix is added automatically when `GOOS=windows`.

## Updating tools.json
- Relative `command[0]` must start with `./tools/bin/NAME` (use `.exe` on Windows)
- Absolute paths are allowed in tests only

Examples:
```json
{"command":["./tools/bin/fs_read_file"]}
```
```json
{"command":["./tools/bin/fs_read_file.exe"]}
```

## Test helpers
Use `tools/testutil/buildtool.go::BuildTool(t, "NAME")` to compile a tool from `tools/cmd/NAME` into a temp dir in tests.

## Migration steps (summary)
1. Emit binaries under `tools/bin/` while keeping legacy sources building
2. Update `tools.json` to reference `./tools/bin/NAME`
3. Move sources into `tools/cmd/NAME/NAME.go` with matching tests
4. Delete legacy sources under `tools/*` once all tools are migrated
5. Tighten `.gitignore` to ignore only `tools/bin/**`
6. Keep docs/examples using `./tools/bin/NAME` exclusively

## Lint and path hygiene
- `make check-tools-paths` fails on legacy invocations or sources outside canonical layout
- `make verify-manifest-paths` validates `tools.json` commands

## Troubleshooting
See `docs/runbooks/troubleshooting.md` for Windows examples and path validation errors.
