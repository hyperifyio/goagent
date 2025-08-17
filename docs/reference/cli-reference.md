# agentcli CLI reference

A concise, canonical reference for `agentcli` flags and behavior. Flags are order-insensitive; precedence is flag > environment > default.

## Flags

- `-prompt string`: User prompt (required)
- `-prompt-file string`: Path to file containing user prompt ('-' for STDIN; mutually exclusive with `-prompt`)
- `-tools string`: Path to tools.json (optional)
- `-system string`: System prompt (default "You are a helpful, precise assistant. Use tools when strictly helpful.")
- `-system-file string`: Path to file containing system prompt ('-' for STDIN; mutually exclusive with `-system`)
- `-developer string`: Developer message (repeatable)
- `-developer-file string`: Path to file containing developer message (repeatable; '-' for STDIN)
- `-base-url string`: OpenAI-compatible base URL (env `OAI_BASE_URL`, default `https://api.openai.com/v1`)
- `-api-key string`: API key if required (env `OAI_API_KEY`; falls back to `OPENAI_API_KEY`)
- `-model string`: Model ID (env `OAI_MODEL`, default `oss-gpt-20b`)
- `-max-steps int`: Maximum reasoning/tool steps (default 8)
- `-http-timeout duration`: HTTP timeout for chat completions (env `OAI_HTTP_TIMEOUT`; falls back to `-timeout` if unset)
- `-prep-http-timeout duration`: HTTP timeout for pre-stage (env `OAI_PREP_HTTP_TIMEOUT`; falls back to `-http-timeout` if unset)
- `-http-retries int`: Number of retries for transient HTTP failures (timeouts, 429, 5xx) (default 2)
- `-http-retry-backoff duration`: Base backoff between HTTP retry attempts (exponential) (default 300ms)
- `-tool-timeout duration`: Per-tool timeout (falls back to `-timeout` if unset)
- `-timeout duration`: [DEPRECATED] Global timeout; prefer `-http-timeout` and `-tool-timeout` (default 30s)
- `-temp float`: Sampling temperature (default 1.0; omitted for models that do not support it)
- `-top-p float`: Nucleus sampling probability mass (conflicts with `-temp`; when set, temperature is omitted per one‑knob rule and `top_p` is sent)
- `-prep-top-p float`: Pre-stage nucleus sampling probability mass (conflicts with `-temp`; when set, pre-stage omits temperature and sends `top_p`)
- `-prep-profile string`: Pre-stage prompt profile (`deterministic|general|creative|reasoning`); sets temperature when supported (conflicts with `-prep-top-p`)
- `-prep-model string`: Pre-stage model ID (env `OAI_PREP_MODEL`; inherits `-model` if unset)
- `-prep-base-url string`: Pre-stage base URL (env `OAI_PREP_BASE_URL`; inherits `-base-url` if unset)
- `-prep-api-key string`: Pre-stage API key (env `OAI_PREP_API_KEY`; falls back to `OAI_API_KEY`/`OPENAI_API_KEY`; inherits `-api-key` if unset)
- `-prep-http-retries int`: Pre-stage HTTP retries (env `OAI_PREP_HTTP_RETRIES`; inherits `-http-retries` if unset)
- `-prep-http-retry-backoff duration`: Pre-stage HTTP retry backoff (env `OAI_PREP_HTTP_RETRY_BACKOFF`; inherits `-http-retry-backoff` if unset)
- `-prep-cache-bust`: Skip pre-stage cache and force recompute
- `-prep-enabled`: Enable pre-stage processing (default true). When false, pre-stage is skipped and the agent proceeds directly with the original `{system,user}` messages.
- `-debug`: Dump request/response JSON to stderr
- `-verbose`: Also print non-final assistant channels (critic/confidence) to stderr
- `-quiet`: Suppress non-final output; print only final text to stdout
- `-prep-tools-allow-external`: Allow pre-stage to execute external tools from `-tools` (default false). When not set, pre-stage is limited to built-in read-only tools and ignores `-tools`.
- `-capabilities`: Print enabled tools and exit
- `-print-config`: Print resolved config and exit
- `--version | -version`: Print version and exit

## Environment variables

- `OAI_BASE_URL`: Base URL for chat completions API
- `OAI_MODEL`: Default model ID
- `OAI_API_KEY`: API key (canonical; CLI also accepts `OPENAI_API_KEY` as a fallback)
- `OAI_HTTP_TIMEOUT`: HTTP timeout for chat requests (e.g., `90s`)
- `OAI_PREP_HTTP_TIMEOUT`: HTTP timeout for pre-stage requests (e.g., `90s`); overrides inheritance from `-http-timeout`
- `LLM_TEMPERATURE`: Temperature override when `-temp` is not provided (flag takes precedence)

## Exit codes

- `0`: Success, printed final assistant message or handled help/version
- `1`: Operational error (HTTP failure, tool manifest issues, no final assistant content)
- `2`: CLI misuse (e.g., missing `-prompt`)

## Examples

- Inline developer messages (repeatable) with an inline prompt:

```bash
./bin/agentcli -developer "Follow style guide X" -developer "Prefer JSON outputs" -prompt "Summarize the repo"
```

- Read system prompt from a file and user prompt from STDIN:

```bash
echo "What changed since last release?" | ./bin/agentcli -system-file ./system.txt -prompt-file -
```

- Mix developer files and inline developer messages; read one developer message from STDIN:

```bash
echo "Security MUST be documented" | ./bin/agentcli \
  -developer-file ./dev/a.txt \
  -developer-file - \
  -developer "Add tests for every change" \
  -prompt "Implement the CLI role flags"
```

## Notes

- Temperature is sent only when supported by the selected model; otherwise it is omitted to avoid API errors. When `-top-p` is set, temperature is omitted, `top_p` is included, and a one-line warning is printed to stderr.
- Tools are executed via argv only with JSON stdin/stdout and strict timeouts; no shell is used.

## Prompt profiles

The following profiles map to sampling behaviors for convenience. Temperature is omitted when the target model does not support it.

| Profile | Effect |
|---|---|
| deterministic | temperature = 0.1 (pre-stage via `-prep-profile deterministic`) |
| general | temperature = 1.0 |
| creative | temperature = 1.0 |
| reasoning | temperature = 1.0 |
