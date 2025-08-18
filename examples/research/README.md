# Research tools: manual examples (no agent)

These examples show how to run the research CLI tools directly without `agentcli`. They avoid network in CI by using comments or localhost-only fixtures.

## Prerequisites
- Build the tools first:
```bash
make build-tools
```
- Set environment variables when required by a tool.

## SearXNG meta search
Requires `SEARXNG_BASE_URL` (e.g., `http://localhost:8888`).
```bash
echo '{"q":"golang","size":5}' | SEARXNG_BASE_URL=http://localhost:8888 ./tools/bin/searxng_search | jq '.results[] | {title,url,engine}'
```

## HTTP fetch (safe)
```bash
echo '{"url":"https://example.com","method":"GET"}' | ./tools/bin/http_fetch | jq '{status, truncated}'
```

## robots.txt check
```bash
echo '{"url":"https://example.com/robots.txt","user_agent":"agentcli"}' | ./tools/bin/robots_check | jq .
```

## Readability extraction (HTML → article)
```bash
HTML=$(curl -Lsf https://example.com | head -c 200000)
jq -n --arg html "$HTML" --arg base "https://example.com" '{html:$html, base_url:$base}' | ./tools/bin/readability_extract | jq '{title, length}'
```

## Metadata extraction (OG/Twitter/JSON-LD)
```bash
HTML=$(curl -Lsf https://example.com | head -c 200000)
jq -n --arg html "$HTML" --arg base "https://example.com" '{html:$html, base_url:$base}' | ./tools/bin/metadata_extract | jq '{have_og:(.opengraph!=null), have_twitter:(.twitter!=null), jsonld_len:(.jsonld|length)}'
```

## PDF extract
Reads base64 input. Example encodes a local PDF (no network).
```bash
base64 -w0 ./examples/image-gen/example.pdf | jq -n --arg b64 "$(cat)" '{pdf_base64:$b64}' | ./tools/bin/pdf_extract | jq '{page_count, first:(.pages[0].text|.[0:120])}'
```

## RSS/Atom fetch
```bash
echo '{"url":"https://hnrss.org/frontpage"}' | ./tools/bin/rss_fetch | jq '.items[0]'
```

## Wayback lookup
```bash
echo '{"url":"https://example.com","save":false}' | ./tools/bin/wayback_lookup | jq .
```

## Wikipedia query
```bash
echo '{"titles":"Golang"}' | ./tools/bin/wiki_query | jq '.pages[0]'
# or
echo '{"search":"Golang"}' | ./tools/bin/wiki_query | jq '.pages[0]'
```

## OpenAlex search
```bash
echo '{"q":"large language models","per_page":5}' | ./tools/bin/openalex_search | jq '.results[0] | {title, publication_year, cited_by_count}'
```

## Crossref search
Requires `CROSSREF_MAILTO`.
```bash
echo '{"q":"transformers","rows":5}' | CROSSREF_MAILTO=user@example.com ./tools/bin/crossref_search | jq '.results[0] | {title, doi, issued}'
```

## GitHub search (rate-limited without token)
Optional `GITHUB_TOKEN`.
```bash
echo '{"q":"repo:golang/go scheduler","type":"code","per_page":3}' | ./tools/bin/github_search | jq '{count:(.results|length), rate:.rate.remaining}'
```

## De-duplicate and rank
```bash
jq -n '{docs:[{id:"a",title:"Title A",text:"hello world"},{id:"b",title:"Title B",text:"hello world!"}]}' | ./tools/bin/dedupe_rank | jq .
```

## Citation pack
```bash
echo '{"doc":{"url":"https://example.com","title":"Example"},"archive":{"wayback":false}}' | ./tools/bin/citation_pack | jq .
```

---

### Notes for CI/offline runs
- Network commands above are examples; keep them commented in CI.
- Prefer `httptest.Server` fixtures in tests; this page is for humans running locally.

### Offline fixtures-only test sweep (no network)
These package tests use local HTTP fixtures via `httptest.Server`.
```bash
# Safe to run in CI without network access
go test ./tools/cmd/... -count=1
```
