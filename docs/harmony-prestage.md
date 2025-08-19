# Pre-stage Harmony Output Contract

This document specifies the required shape of Harmony messages returned by the pre-stage step. The goal is deterministic merging into the main call while preventing tool execution or assistant chatter at this stage.

## Contract

- Messages MUST be a JSON array of objects with fields: `role` and `content` (optional additional fields from the common schema are permitted but constrained as below).
- Allowed roles: `system`, `developer`.
- Disallowed roles: `user`, `assistant`, `tool`.
- `tool_calls` MUST NOT appear on any message.
- `tool_call_id` MUST NOT appear.
- `channel` MUST be omitted or empty.

Examples (valid):

```json
[
  {"role":"system","content":"You are a careful planning assistant."},
  {"role":"developer","content":"Provide 3 bullet goals and tool hints."}
]
```

Examples (invalid):

```json
[{"role":"assistant","content":"done"}]
```

```json
[{"role":"system","tool_calls":[{"id":"x","type":"function","function":{"name":"foo"}}]}]
```

## Validator

The function `internal/oai/ValidatePrestageHarmony` enforces the above. Unit tests cover allowed roles and rejection cases for assistant/user/tool roles, presence of `tool_calls`, and stray `tool_call_id`.

## Rationale

Pre-stage is for shaping prompts and configuration, not executing tools or producing end-user visible content. Restricting roles ensures deterministic merge and predictable routing.
