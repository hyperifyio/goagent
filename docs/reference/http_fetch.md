# HTTP fetch tool (http_fetch)

Safe HTTP/HTTPS fetcher with hard byte caps, limited redirects, optional gzip decompression, and SSRF guard. The tool streams JSON over stdin/stdout; errors are single-line JSON on stderr with non-zero exit.

## Contracts

- Stdin: single JSON object
- Stdout (success): single-line JSON object
- Stderr (failure): single-line JSON `{ "error": "...", "hint?": "..." }` and non-zero exit

### Parameters

- `url` (string, required): http/https URL
- `method` (string, optional): `GET` or `HEAD` (default `GET`)
- `max_bytes` (int, optional): hard byte cap for response body (default 1048576)
- `timeout_ms` (int, optional): request timeout in milliseconds (default 10000; falls back to `HTTP_TIMEOUT_MS` env if unset)
- `decompress` (bool, optional): when true (default), enables transparent gzip decoding; when false, returns raw bytes

### Output

```
{
  "status": 200,
  "headers": {"Content-Type": "text/plain; charset=utf-8", "ETag": "\"abc123\""},
  "body_base64": "...",
  "truncated": false
}
```

### Example: GET

Input to stdin:

```json
{"url": "https://example.org/robots.txt", "max_bytes": 65536}
```

### Example: HEAD

Input to stdin:

```json
{"url": "https://example.org/", "method": "HEAD"}
```

## Behavior

- Schemes: only `http` and `https` are allowed
- Redirects: up to 5 redirects are followed; further redirects fail with `"too many redirects"`
- Headers: response headers are returned as a simple string map; `ETag` and `Last-Modified` are preserved when present
- Decompression: gzip decoding is enabled by default; set `decompress=false` to receive raw compressed bytes
- Byte cap: responses are read with a strict byte cap; when exceeded, `truncated=true` and the body is cut at `max_bytes`
- User-Agent: `agentcli-http-fetch/0.1`

## Security (SSRF guard)

- Blocks loopback, RFC1918, link-local, and IPv6 ULA destinations
- Blocks `.onion` hosts
- Redirect targets are re-validated
- For tests/local-only usage, setting `HTTP_FETCH_ALLOW_LOCAL=1` disables the block

## Environment

- `HTTP_TIMEOUT_MS` (optional): default timeout in milliseconds when `timeout_ms` is unset

## Audit

On each run, an NDJSON line is appended under `.goagent/audit/YYYYMMDD.log` with fields:

```
{tool:"http_fetch",url_host,status,bytes,truncated,ms}
```

## Manifest

Ensure an entry similar to the following exists in `tools.json`:

```json
{
  "name": "http_fetch",
  "description": "Safe HTTP/HTTPS fetcher with byte cap and redirects",
  "schema": {"type": "object", "required": ["url"], "properties": {"url": {"type": "string"}, "method": {"type": "string", "enum": ["GET", "HEAD"]}, "max_bytes": {"type": "integer", "minimum": 1, "default": 1048576}, "timeout_ms": {"type": "integer", "minimum": 1, "default": 10000}, "decompress": {"type": "boolean", "default": true}}, "additionalProperties": false},
  "command": ["./tools/bin/http_fetch"],
  "timeoutSec": 15,
  "envPassthrough": ["HTTP_TIMEOUT_MS"]
}
```
