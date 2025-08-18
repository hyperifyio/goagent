# metadata_extract

Extract OpenGraph, Twitter card, and JSON-LD metadata from HTML.

- stdin JSON:

```json
{"html":"<html>...</html>","base_url":"https://example.org/page"}
```

- stdout JSON:

```json
{"opengraph":{...},"twitter":{...},"jsonld":[ ... ]}
```

- exit codes: 0 success; non-zero with stderr JSON `{ "error": "..." }` on failure.

## Examples

```bash
html='<!doctype html><html><head><meta property="og:title" content="T"><meta name="twitter:card" content="summary"><script type="application/ld+json">{"@context":"https://schema.org","@type":"Article","headline":"H"}</script></head><body></body></html>'
printf '{"html":'%s',"base_url":"https://example.org/x"}' "$html" | ./tools/bin/metadata_extract | jq
```
