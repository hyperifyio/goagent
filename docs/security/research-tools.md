# Security posture for research tools

This page documents the security posture, guardrails, and operational guidance for the CLI-only research tools (e.g., `searxng_search`, `http_fetch`, `robots_check`, `readability_extract`, `metadata_extract`, `pdf_extract`, `rss_fetch`, `wayback_lookup`, `wiki_query`, `openalex_search`, `crossref_search`, `dedupe_rank`, `citation_pack`). It complements the broader threat model by focusing on network egress safety, provenance, and audit discipline for web-facing tools.

## Network egress and SSRF protections

Tools that reach the network must enforce a strict allowlist for schemes and a denylist for address families to prevent SSRF and lateral movement:

- Allowed schemes: `http`, `https` only. All others are rejected.
- Denylist targets (reject both direct IPs and DNS results that resolve to these ranges):
  - Loopback: `127.0.0.0/8`, `::1/128`
  - RFC1918 private IPv4: `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`
  - Link-local: `169.254.0.0/16` (IPv4), `fe80::/10` (IPv6)
  - Unique local IPv6 (RFC4193): `fc00::/7`
  - Multicast/broadcast and unspecified: `224.0.0.0/4`, `ff00::/8`, `255.255.255.255`, `0.0.0.0`, `::`
  - Tor/onion services: hostnames ending in `.onion`
- Redirect handling: follow at most 5 redirects and re-apply SSRF checks on each hop before connecting.
- DNS rebinding protection: resolve the destination host and validate every resolved address against the denylist; reject when any hop resolves to a blocked range.
- Byte caps and timeouts: enforce tool-specific response size caps and sane timeouts; prefer connection + overall deadlines over idle timeouts.

## Robots compliance

- Respect `robots.txt` per RFC 9309. Evaluate rules using the effective origin of the fetch, preferring the most specific user-agent group.
- Use `robots_check` to determine `allowed` and optional `crawl_delay_ms` before `http_fetch` or other network retrievals when applicable.
- Do not follow redirects to a different origin for `robots.txt` evaluation.

## Outbound User-Agent and etiquette

- Each tool sends a distinct User-Agent: `agentcli-<tool-name>/0.1` (e.g., `agentcli-searxng/0.1`, `agentcli-http-fetch/0.1`).
- Honor server guidance: handle `Retry-After` on 429/503; back off with capped retries as specified per tool.
- Keep requests minimal and purpose-limited; avoid fetching binary content unless explicitly required by the tool.

## Audit logging and redaction policy

- All networked tools emit structured NDJSON audit records with fields appropriate to the tool. Common fields include:
  - `ts` (RFC3339 UTC timestamp)
  - `tool` (string)
  - `url_host` (hostname only)
  - Operation-specific fields (e.g., `status`, `bytes`, `ms`, `retries`, `truncated`, `saved`)
- Redaction rules:
  - Never log secrets or tokens. Do not include full URLs with query parameters when they may contain credentials; prefer host-only or redact sensitive keys.
  - For potentially sensitive queries (e.g., long search strings), truncate beyond 256 characters and include `query_truncated: true`.
  - Base64 payloads and large bodies are never written to audit logs.

## Environment and sandboxing guidance

- Pass only an explicit allowlist of environment variables per-tool via `envPassthrough`. Avoid ambient secrets.
- Run tools as an unprivileged user. Consider containerization, user namespaces, or seccomp/AppArmor where feasible.
- Constrain filesystem effects to repository-relative paths validated against traversal and symlink escapes.
- Apply process-level limits (ulimits) and per-invocation time/size caps to prevent resource abuse.

## Deterministic behavior and retries

- Enforce hard redirect limits, bounded retries with jitter on transient errors (e.g., timeouts, 429, 5xx), and clear non-zero exits with single-line JSON on stderr describing the error.
- Keep stdout schema stable and validated; treat deviations as errors.

## Related documents

- Threat model and trust boundaries: see [security/threat-model.md](threat-model.md)
- Tool reference for stdin/stdout schemas and env passthrough: see [reference/research-tools.md](../reference/research-tools.md)
