# readability_extract

Extract article content from HTML using go-readability.

- stdin JSON:

```json
{"html": "<html>...</html>", "base_url": "https://example.org/page"}
```

- stdout JSON:

```json
{"title":"...","byline":"...","text":"...","content_html":"<p>...</p>","length":1234}
```

- exit codes: 0 success; non-zero with stderr JSON `{ "error": "..." }` on failure.

## Examples

```bash
echo '{"html":"<html><body><article><h1>T</h1><p>Hi</p></article></body></html>","base_url":"https://example.org/x"}' | ./tools/bin/readability_extract | jq .title
```
