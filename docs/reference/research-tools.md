# Research tools reference

This page consolidates contracts and usage for the research-oriented tools. Each tool consumes a single JSON object on stdin and prints a single JSON object on stdout on success. On error, tools exit non‑zero and print a single-line JSON error to stderr: `{ "error": "...", "hint?": "..." }`.

Notes
- All networked tools implement an SSRF guard: they block loopback, RFC1918/ULA, link‑local, and `.onion` destinations. Redirect targets are re‑validated. Some tools allow an opt‑in env to permit local addresses in tests.
- Timeouts apply per tool; see each section for defaults and retry rules.
- Examples assume you have built tools via `make build-tools` and are running from the repo root.

## searxng_search
- Stdin: `{ "q": string, "time_range?": "day|week|month|year", "categories?": [string], "engines?": [string], "language?": string, "page?": int, "size?": int<=50 }`
- Stdout: `{ "query": string, "results": [{"title": string, "url": string, "snippet": string, "engine": string, "published_at?": string}] }`
- Env: `SEARXNG_BASE_URL` (required), `HTTP_TIMEOUT_MS` (optional)
- Network: timeout 10s; redirects ≤5; retries up to 2 on timeout/429/5xx (observes `Retry-After`)
- Security: SSRF guard; blocks local/private/ULA/`.onion`
- Example:
```bash
export SEARXNG_BASE_URL=http://localhost:8888
printf '{"q":"golang","size":3}' | ./tools/bin/searxng_search | jq .
```

## http_fetch
- Stdin: `{ "url": string, "method?": "GET|HEAD", "max_bytes?": 1048576, "timeout_ms?": 10000, "decompress?": true }`
- Stdout: `{ "status": int, "headers": object, "body_base64?": string, "truncated": bool }`
- Env: `HTTP_TIMEOUT_MS` (optional)
- Network: timeout default 10s; redirects ≤5
- Security: SSRF guard; blocks local/private/ULA/`.onion`
- Example:
```bash
printf '{"url":"https://example.org/robots.txt","max_bytes":65536}' | ./tools/bin/http_fetch | jq .status
```

## robots_check
- Purpose: Evaluate `<origin>/robots.txt` for a target URL and user agent.
- Stdin: `{ "url": string, "user_agent?": "agentcli" }`
- Stdout: `{ "allowed": bool, "crawl_delay_ms?": int, "group_rules": [string] }`
- Env: none (tests may set `ROBOTS_CHECK_ALLOW_LOCAL=1`)
- Network: timeout 5s; redirects allowed only within the same origin (no cross‑origin)
- Security: SSRF guard; DNS and IP checks block private/loopback
- Example:
```bash
echo '{"url":"https://example.org/path"}' | ./tools/bin/robots_check | jq .
```

## readability_extract
- Stdin: `{ "html": string, "base_url": string }` (≤5 MiB)
- Stdout: `{ "title": string, "byline?": string, "text": string, "content_html": string, "length": int }`
- Env: none
- Example:
```bash
echo '{"html":"<html><body><article><h1>T</h1><p>Hi</p></article></body></html>","base_url":"https://example.org/x"}' | ./tools/bin/readability_extract | jq .title
```

## metadata_extract
- Stdin: `{ "html": string, "base_url": string }`
- Stdout: `{ "opengraph": object, "twitter": object, "jsonld": [any] }`
- Env: none
- Example:
```bash
html='<!doctype html><html><head><meta property="og:title" content="T"><meta name="twitter:card" content="summary"><script type="application/ld+json">{"@context":"https://schema.org","@type":"Article","headline":"H"}</script></head><body></body></html>'
printf '{"html":%s,"base_url":"https://example.org/x"}' "$html" | ./tools/bin/metadata_extract | jq .opengraph.title
```

## pdf_extract
- Stdin: `{ "pdf_base64": string, "pages?": [int] }` (≤20 MiB)
- Stdout: `{ "page_count": int, "pages": [{"index": int, "text": string}] }`
- Env: `ENABLE_OCR` enables OCR via `tesseract` if text is missing; if unavailable, exits non‑zero with `{"error":"OCR_UNAVAILABLE"}`
- Example:
```bash
printf '{"pdf_base64":"'"$(base64 -w0 sample.pdf)"'"}' | ./tools/bin/pdf_extract | jq .page_count
```

## rss_fetch
- Stdin: `{ "url": string, "if_modified_since?": string }`
- Stdout: `{ "feed": {"title": string, "link": string}, "items": [{"title": string, "url": string, "published_at?": string, "summary?": string}] }`
- Env: none
- Network: timeout 5s; redirects ≤5; SSRF guard
- UA: `agentcli-rss-fetch/0.1`
- Example:
```bash
echo '{"url":"https://example.com/feed.xml"}' | ./tools/bin/rss_fetch | jq .items[0]
```

## wayback_lookup
- Stdin: `{ "url": string, "save?": false }`
- Stdout: `{ "closest_url?": string, "timestamp?": string, "saved?": bool }`
- Env: `WAYBACK_BASE_URL` (optional; default `https://web.archive.org`)
- Network: timeout 3s; one retry on 5xx with jitter
- Security: SSRF guard
- Example:
```bash
jq -n '{url:"http://example.com", save:true}' | ./tools/bin/wayback_lookup | jq .
```

## wiki_query
- Stdin: `{ "titles?": string, "search?": string, "language?": "en" }` (exactly one of `titles` or `search` required)
- Stdout: `{ "pages": [{"title": string, "url": string, "extract": string}] }`
- Env: optional `MEDIAWIKI_BASE_URL` to override, otherwise `https://{language}.wikipedia.org`
- Network: timeout 5s
- Security: SSRF guard (blocks local/private)
- Example:
```bash
echo '{"search":"Golang","language":"en"}' | ./tools/bin/wiki_query | jq .pages[0]
```

## openalex_search
- Stdin: `{ "q": string, "from?": string, "to?": string, "per_page?": int }`
- Stdout: `{ "results": [{"title": string, "doi?": string, "publication_year": int, "open_access_url?": string, "authorships": [...], "cited_by_count": int}], "next_cursor?": string }`
- Env: optional `OPENALEX_BASE_URL` (default `https://api.openalex.org`), `HTTP_TIMEOUT_MS`
- Network: timeout 8s; retries 1 on 5xx/timeout; SSRF guard
- Example:
```bash
printf '{"q":"golang","per_page":5}' | ./tools/bin/openalex_search | jq .results[0]
```

## crossref_search
- Stdin: `{ "q": string, "rows?": int }`
- Stdout: `{ "results": [{"title": string, "doi": string, "issued": string, "container": string, "title_short?": string}] }`
- Env: required `CROSSREF_MAILTO`; optional `HTTP_TIMEOUT_MS`
- Network: timeout 8s; redirects ≤5; SSRF guard
- Rate limits: on 429, exits non‑zero with stderr JSON `{ "error": "RATE_LIMITED", "hint": "use GITHUB_TOKEN" }`
- Example:
```bash
printf '{"q":"golang","rows":5}' | ./tools/bin/crossref_search | jq .results[0]
```

## github_search
- Stdin: `{ "q": string, "type": "repositories|code|issues|commits", "per_page?": int }`
- Stdout: `{ "results": [ ...minimal per type... ], "rate": { "remaining": int, "reset": int } }`
- Env: optional `GITHUB_TOKEN`, `HTTP_TIMEOUT_MS`
- Network: timeout 8s; retry 1 on 5xx; SSRF guard
- Rate limits: if `X‑RateLimit‑Remaining` is `0`, exits non‑zero with stderr JSON `{ "error":"RATE_LIMITED", "hint":"use GITHUB_TOKEN" }`
- Example:
```bash
echo '{"q":"language:go stars:>5000","type":"repositories"}' | ./tools/bin/github_search | jq .results[0]
```

## dedupe_rank
- Stdin: `{ "docs": [{"id": string, "url?": string, "title?": string, "text?": string, "published_at?": string}] }`
- Stdout: `{ "groups": [{"representative_id": string, "members": [string], "score": number}] }`
- Env: optional `AUTHORITY_HINTS_JSON` (if supported) to bias ranking
- Behavior: MinHash‑like 3‑shingle + TF‑IDF tie‑break; deterministic output
- Example:
```bash
jq -n '{docs:[{id:"a",title:"Intro to Go"},{id:"b",title:"Go introduction"},{id:"c",title:"Rust book"}]}' | ./tools/bin/dedupe_rank | jq .
```

## citation_pack
- Stdin: `{ "doc": {"title?": string, "url": string, "published_at?": string}, "archive?": {"wayback?": bool} }`
- Stdout: `{ "title?": string, "url": string, "host": string, "accessed_at": string, "archive_url?": string }`
- Env: optional `WAYBACK_BASE_URL`
- Behavior: if `archive.wayback` is true, queries Wayback for an existing snapshot
- Example:
```bash
echo '{"doc":{"url":"https://example.com/post"},"archive":{"wayback":true}}' | ./tools/bin/citation_pack | jq .
```

---

Exit codes
- 0: success; stdout contains a single JSON object
- non‑zero: failure; stderr contains a single‑line JSON error with optional `hint`

Security and SSRF
- All network tools validate destinations and block private, loopback, link‑local, ULA, and `.onion` addresses. A few tools support a test‑only override via an env variable; see the tool’s section.
