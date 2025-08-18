# wayback_lookup

Query the Internet Archive Wayback Machine for the closest snapshot of a URL, optionally triggering an archival save.

## Usage

```bash
export WAYBACK_BASE_URL="https://web.archive.org"
# Lookup closest snapshot
jq -n '{url:"http://example.com"}' | ./tools/bin/wayback_lookup | jq .

# Trigger save if not available
jq -n '{url:"http://example.com", save:true}' | ./tools/bin/wayback_lookup | jq .
```

## Stdin schema

```json
{
  "type": "object",
  "properties": {
    "url": {"type": "string"},
    "save": {"type": "boolean", "default": false}
  },
  "required": ["url"],
  "additionalProperties": false
}
```

## Stdout

```json
{
  "closest_url": "http://web.archive.org/web/20200101000000/http://example.com/",
  "timestamp": "20200101000000",
  "saved": true
}
```

- 3s timeout, one retry on 5xx with small backoff.
- SSRF guard blocks loopback/private/onion unless `WAYBACK_ALLOW_LOCAL=1` during tests.
- User-Agent: inherits default Go; may be customized later.
