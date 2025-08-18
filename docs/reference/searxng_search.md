# SearXNG search tool (searxng_search)

Run a web meta search via SearXNG's JSON API.

- Stdin JSON: {"q":string,"time_range?":"day|week|month|year","categories?":[string],"engines?":[string],"language?":string,"page?":int,"size?":int<=50}
- Stdout JSON: {"query":string,"results":[{"title":string,"url":string,"snippet":string,"engine":string,"published_at?":string}]}
- Env: SEARXNG_BASE_URL (required), HTTP_TIMEOUT_MS (optional)
- Retries: up to 2 on timeout, 429 (observes Retry-After), or 5xx
- SSRF guard: blocks loopback/RFC1918/link-local/ULA and .onion

Example:

```bash
export SEARXNG_BASE_URL=http://localhost:8888
printf '{"q":"golang","size":3}' | ./tools/bin/searxng_search | jq
```
