# citation_pack

Normalize citation metadata and optionally attach a Wayback archive URL.

## Stdin schema

```json
{
  "doc": {
    "title": "string?",
    "url": "string",
    "published_at": "string?"
  },
  "archive": {
    "wayback": "boolean?"
  }
}
```

## Stdout schema

```json
{
  "title": "string?",
  "url": "string",
  "host": "string",
  "accessed_at": "string",
  "archive_url": "string?"
}
```

- "accessed_at" is an RFC3339 UTC timestamp of when the pack was created.
- When `archive.wayback` is true, the tool queries a Wayback-compatible endpoint for an existing snapshot and includes its URL if available.

## Environment

- `WAYBACK_BASE_URL` (optional): Base URL for Wayback API (defaults to `https://web.archive.org`).

## Exit codes

- 0: success
- non-zero: error; stderr contains a single-line JSON `{ "error": "..." }`.

## Examples

- Minimal normalization:

```bash
echo '{"doc":{"url":"https://example.com/post"}}' | ./tools/bin/citation_pack | jq .
```

- Include Wayback lookup (using a local test server):

```bash
export WAYBACK_BASE_URL="http://localhost:8080"
echo '{"doc":{"url":"https://example.com/post"},"archive":{"wayback":true}}' | ./tools/bin/citation_pack | jq .
```
