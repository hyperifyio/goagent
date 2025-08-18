# rss_fetch

Fetch an RSS or Atom feed and return a normalized JSON structure.

## Usage

```bash
echo '{"url":"https://example.com/feed.xml"}' | ./tools/bin/rss_fetch | jq
```

## Stdin schema

```json
{
  "type": "object",
  "properties": {
    "url": {"type": "string"},
    "if_modified_since": {"type": "string"}
  },
  "required": ["url"],
  "additionalProperties": false
}
```

## Stdout

```json
{
  "feed": {"title": "...", "link": "..."},
  "items": [
    {"title": "...", "url": "...", "published_at": "...", "summary": "..."}
  ]
}
```

- Honors If-Modified-Since header; on 304 Not Modified returns an empty items array.
- 5s timeout; up to 5 redirects with SSRF guard (blocks loopback/private/onion).
- User-Agent: agentcli-rss-fetch/0.1
