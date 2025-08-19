## Interface: code.sandbox.js.run

The JavaScript sandbox executes a short snippet with a strict deny-by-default security model and bounded resource usage. It is intended for tiny, deterministic computations on assistant-provided inputs without ambient access to the host environment.

- Purpose: run isolated JS with no filesystem, network, timers, or console; only minimal host bindings are exposed.
- Security: deny-by-default; only `emit` and `read_input` are available. No `require`, no `console`, no timers, no Promise scheduling. Treat untrusted code as hostile; limits are enforced best-effort.
- Limits: wall-clock timeout and output size cap; output is truncated when exceeding the cap and an `OUTPUT_LIMIT` error is returned.

### JSON contract

- stdin (object):
  - `source` (string, required): JavaScript source code to evaluate.
  - `input` (string, required): Opaque input made available to the script via `read_input()`.
  - `limits` (object, required):
    - `wall_ms` (int, optional): Maximum wall-clock time in milliseconds. Default 1000 ms.
    - `output_kb` (int, optional): Maximum output size in KiB before truncation. Default 64 KiB.

- stdout (on success):
```json
{"output":"<string>"}
```

- stderr (on failure): single-line JSON with a stable error code:
```json
{"code":"EVAL_ERROR","message":"<details>"}
{"code":"TIMEOUT","message":"execution exceeded <ms> ms"}
{"code":"OUTPUT_LIMIT","message":"output exceeded <KB> KB"}
```

### Host bindings available inside the VM

- `read_input(): string` — returns the provided `input` string.
- `emit(s: string): void` — appends `s` to the output buffer. When the buffer reaches `output_kb`, the VM aborts with `OUTPUT_LIMIT` after returning truncated stdout.

All other globals are intentionally undefined (e.g., `typeof require === 'undefined'`, `typeof console === 'undefined'`, `typeof setTimeout === 'undefined'`).

### Examples

- Echo input:
```json
{
  "source": "emit(read_input())",
  "input": "hello",
  "limits": {"output_kb": 4}
}
```
Expected stdout:
```json
{"output":"hello"}
```

- Output limit with truncation and error:
```json
{
  "source": "emit(read_input())",
  "input": "<1500 x 'a'>",
  "limits": {"output_kb": 1}
}
```
Expected behavior: stdout contains 1024 bytes of `"a"`; stderr is `{"code":"OUTPUT_LIMIT",...}` and the process exits non‑zero.

### Quick verification via CLI (local repository)

You can verify the interface using the existing unit tests:
```bash
# Run a subset of tests for the sandbox
go test ./internal/tools/jsrun -run 'TestRun_EmitReadInput_Succeeds|TestRun_OutputLimit_TruncatesAndErrors|TestRun_Timeout_Interrupts' -v
```
These tests cover happy-path echo, output truncation, and timeout interruption.

### Security model and pitfalls

- No ambient capabilities: the VM has no filesystem, network, or OS access.
- No timers or async scheduling: `setTimeout`, `setInterval`, and Promise-based scheduling are not available by default.
- Deterministic budget: keep snippets small; large computations may hit the `wall_ms` timeout.
- Do not embed secrets in `source` or `input`; logs may include error metadata.

### Status

- Implementation: `internal/tools/jsrun/handler.go`
- Tests: `internal/tools/jsrun/handler_test.go`
- Consumers: intended for future internal tool wiring; not exposed as an external tool binary at this time.
