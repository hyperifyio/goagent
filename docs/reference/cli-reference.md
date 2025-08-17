# agentcli CLI reference

A concise, canonical reference for `agentcli` flags and behavior. Flags are order-insensitive; precedence is flag > environment > default.

## Flags

- `-prompt string`: User prompt (required)
- `-tools string`: Path to tools.json (optional)
- `-system string`: System prompt (default "You are a helpful, precise assistant. Use tools when strictly helpful.")
- `-base-url string`: OpenAI-compatible base URL (env `OAI_BASE_URL`, default `https://api.openai.com/v1`)
- `-api-key string`: API key if required (env `OAI_API_KEY`; falls back to `OPENAI_API_KEY`)
- `-model string`: Model ID (env `OAI_MODEL`, default `oss-gpt-20b`)
- `-max-steps int`: Maximum reasoning/tool steps (default 8)
- `-http-timeout duration`: HTTP timeout for chat completions (env `OAI_HTTP_TIMEOUT`; falls back to `-timeout` if unset)
- `-http-retries int`: Number of retries for transient HTTP failures (timeouts, 429, 5xx) (default 2)
- `-http-retry-backoff duration`: Base backoff between HTTP retry attempts (exponential) (default 300ms)
- `-tool-timeout duration`: Per-tool timeout (falls back to `-timeout` if unset)
- `-timeout duration`: [DEPRECATED] Global timeout; prefer `-http-timeout` and `-tool-timeout` (default 30s)
- `-temp float`: Sampling temperature (default 1.0; omitted for models that do not support it)
- `-top-p float`: Nucleus sampling probability mass (conflicts with `-temp`; when set, temperature is omitted per one‑knob rule and `top_p` is sent)
- `-debug`: Dump request/response JSON to stderr
- `-capabilities`: Print enabled tools and exit
- `-print-config`: Print resolved config and exit
- `--version | -version`: Print version and exit

## Environment variables

- `OAI_BASE_URL`: Base URL for chat completions API
- `OAI_MODEL`: Default model ID
- `OAI_API_KEY`: API key (canonical; CLI also accepts `OPENAI_API_KEY` as a fallback)
- `OAI_HTTP_TIMEOUT`: HTTP timeout for chat requests (e.g., `90s`)
- `LLM_TEMPERATURE`: Temperature override when `-temp` is not provided (flag takes precedence)

## Exit codes

- `0`: Success, printed final assistant message or handled help/version
- `1`: Operational error (HTTP failure, tool manifest issues, no final assistant content)
- `2`: CLI misuse (e.g., missing `-prompt`)

## Notes

- Temperature is sent only when supported by the selected model; otherwise it is omitted to avoid API errors. When `-top-p` is set, temperature is omitted, `top_p` is included, and a one-line warning is printed to stderr.
- Tools are executed via argv only with JSON stdin/stdout and strict timeouts; no shell is used.
