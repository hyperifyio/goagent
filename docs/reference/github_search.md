### github_search

- stdin: `{ "q": string, "type": "repositories|code|issues|commits", "per_page?": 10 }`
- stdout: `{ "results": [ ...minimal per type... ], "rate": { "remaining": int, "reset": int } }`
- env: optional `GITHUB_TOKEN`
- behavior: uses `Accept: application/vnd.github+json`, 8s timeout (override via `HTTP_TIMEOUT_MS`), 1 retry on 5xx, SSRF guard blocks private/loopback.
- rate limit: if `X-RateLimit-Remaining` is `0`, exits non-zero with stderr JSON: `{"error":"RATE_LIMITED","hint":"use GITHUB_TOKEN"}`.

Example (repositories):
```bash
echo '{"q":"language:go stars:>5000","type":"repositories"}' | ./tools/bin/github_search | jq '.results[0]'
```
