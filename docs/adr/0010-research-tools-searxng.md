# ADR-0010: Adopt SearXNG & network research toolbelt (CLI-only)

## Status
Accepted

## Context
We need credible, repeatable web discovery with provenance for an offline-by-default agent CLI. Direct calls to individual search engines vary in quality, rate limits, and output formats; scraping HTML directly is brittle and raises maintenance and legal concerns. A meta-search engine provides a uniform query interface and result schema across multiple engines, improving recall and resiliency while keeping configuration centralized. We also need a small set of CLI-only subtools for fetching and parsing content deterministically with SSRF guards, strict timeouts, and auditable outputs.

Requirements:
- Deterministic CLI tools with JSON stdin/stdout contracts
- SSRF guard: block loopback, RFC1918/4193, link‑local, and .onion; protect against DNS rebinding
- Bounded network behavior: timeouts, retry policies, and redirect limits
- Provenance: preserve URLs and basic metadata for citation
- No scraping frameworks; keep a narrow surface with testable behaviors

## Options
1. Call individual engine APIs directly (Google, Bing, etc.)
   - Pros: direct features, official quotas
   - Cons: keys/quotas per engine, divergent schemas, vendor lock‑in
2. Screen‑scrape engines or sites
   - Pros: no keys in some cases
   - Cons: brittle, ToS concerns, high maintenance, anti‑bot friction
3. Use a meta‑search engine (SearXNG) and add small focused CLI tools for follow‑ups
   - Pros: uniform JSON API, engine plurality, self‑hostable, adjustable engine set
   - Cons: one more service to run; still subject to upstream variability

## Decision
Adopt SearXNG as the single meta‑search entry point and introduce a small CLI toolbelt to perform safe retrieval and parsing around it. The initial tool set will include:
- searxng_search: query SearXNG’s JSON API with retries and SSRF guard
- http_fetch: safe HTTP/HTTPS fetcher with byte caps, redirects ≤5, gzip support
- robots_check: evaluate robots.txt for a given origin
- readability_extract: extract article content
- metadata_extract: extract OpenGraph/Twitter/JSON‑LD
- pdf_extract: extract text from PDFs (optional OCR via tesseract)
- rss_fetch: fetch and normalize RSS/Atom feeds
- wayback_lookup: lookup/save via Internet Archive
- wiki_query, openalex_search, crossref_search, github_search: narrow, well‑scoped APIs
- dedupe_rank: near‑duplicate grouping with MinHash + TF‑IDF tie‑break
- citation_pack: normalize and (optionally) archive for citations

These tools are CLI‑only, executed via argv with JSON contracts, audited, and guarded. They will be added incrementally behind explicit capabilities and documented under docs/reference/.

## Consequences
- A running SearXNG instance is assumed for meta‑search (e.g., `SEARXNG_BASE_URL=http://localhost:8888`).
- All tools ship SSRF guards, strict timeouts, limited redirects, and audit NDJSON.
- CI remains offline: tests use `httptest.Server` fixtures; no live network calls.
- Docs include a central reference and a troubleshooting runbook for common network issues.

## Flow (Mermaid)
```mermaid
flowchart TD
  A[agentcli] --> B[tool_calls]
  B --> C[searxng_search]
  C --> D[http_fetch]
  D --> E{content type}
  E -->|HTML| F[readability_extract]
  E -->|PDF| G[pdf_extract]
  E -->|Feed| H[rss_fetch]
  F --> I[metadata_extract]
  C --> J[wiki_query]
  C --> K[openalex_search]
  C --> L[crossref_search]
  C --> M[github_search]
  I --> N[dedupe_rank]
  G --> N
  H --> N
  J --> N
  K --> N
  L --> N
  N --> O[citation_pack]
  O --> P[assistant(final)]
```

## References
- Docs index: see `docs/README.md`
- Threat model and SSRF policy: `docs/security/threat-model.md`
- Tool references will live under `docs/reference/` as they are implemented
