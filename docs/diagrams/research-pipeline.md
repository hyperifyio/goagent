# Research pipeline

```mermaid
flowchart LR
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
