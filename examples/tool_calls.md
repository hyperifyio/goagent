This worked example demonstrates a tool-call session that exercises default temperature (1.0), sequential execution of two tool calls with matching tool_call_id, and a captured transcript via -debug.

Quick start (from repo root):

1) Build binaries as needed:

```bash
make build build-tools
```

2) Run the example test which builds a temporary agent binary, compiles two tools, and drives a mock server with two tool calls:

```bash
go test ./examples -run TestWorkedExample_ToolCalls_TemperatureOne_Sequencing -v
```

Expected: test passes, stdout contains "ok", and the debug transcript (stderr) includes a request with "temperature": 1 and a response with tool_calls. The test also verifies tool message sequencing by checking that a tool message is appended for each tool_call_id.
