# Crossref search tool (crossref_search)

Search DOI metadata via Crossref API.

## Usage

```bash
printf '{"q":"golang","rows":5}' | ./tools/bin/crossref_search | jq
```

- Required env: `CROSSREF_MAILTO` (used in User-Agent and `mailto` parameter)
- Optional env: `HTTP_TIMEOUT_MS`

## Input

- `q` (string): search query
- `rows` (int, default 10, max 50): number of results

## Output

```json
{
  "results": [
    {
      "title": "...",
      "doi": "...",
      "issued": "YYYY[-MM[-DD]]",
      "container": "Journal ...",
      "title_short": "..."
    }
  ]
}
```

## Notes
- 8s default timeout; up to 5 redirects; SSRF guard blocks private/loopback unless `CROSSREF_ALLOW_LOCAL=1` for tests.
- On HTTP 429, the tool exits non‑zero and prints a single‑line stderr JSON with `RATE_LIMITED`.
