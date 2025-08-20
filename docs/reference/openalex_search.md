# OpenAlex search tool (openalex_search)

Search scholarly works via the OpenAlex API.

- Stdin JSON: {"q":string,"from?":string,"to?":string,"per_page?":int<=50}
- Stdout JSON: {"results":[{"title":string,"doi?":string,"publication_year":int,"open_access_url?":string,"authorships":[...] ,"cited_by_count":int}],"next_cursor?":string}
- Env: OPENALEX_BASE_URL (optional, default https://api.openalex.org), HTTP_TIMEOUT_MS (optional)
- Retries: up to 1 on timeout or 5xx
- SSRF guard: blocks loopback/RFC1918/link-local/ULA and .onion

Example:

```bash
printf '{"q":"golang","per_page":5}' | ./tools/bin/openalex_search | jq
```
