```mermaid
sequenceDiagram
    participant CLI as agentcli
    participant API as OpenAI API
    participant TOOLS as Pre-stage tools (in-process)

    Note over CLI: Resolve prep config (-prep-*, env), compute cache key

    CLI->>API: POST /v1/chat/completions [prep system/user + tools]
    API-->>CLI: assistant tool_calls: [{name,args}, ...]

    par Parallel tool calls (pre-stage)
        CLI->>TOOLS: invoke built-in read-only tool (e.g., fs.read_file)
        TOOLS-->>CLI: tool result JSON
        CLI->>TOOLS: invoke built-in read-only tool (e.g., env.get)
        TOOLS-->>CLI: tool result JSON
    end

    CLI->>CLI: ValidateMessageSequence(messages, stage:"prep")
    CLI->>CLI: Audit NDJSON {stage:"prep", timings, idempotency_key}
    CLI->>CLI: Merge roles (system/developer/user) per precedence

    Note over CLI,API: Proceed to main call with merged messages

    CLI->>API: POST /v1/chat/completions [final merged messages]
    API-->>CLI: assistant final (streamed channel:"final"; others buffered)

    CLI-->>CLI: Route channels; print final to stdout
```