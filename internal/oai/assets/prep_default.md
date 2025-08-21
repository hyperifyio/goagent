# Smart Prep Prompt (Default)

The goal of this pre-stage is to deterministically derive:

- A concise but complete system prompt suitable for the main run.
- Zero or more developer prompts to guide style and constraints.
- Tool configuration hints, including image-generation guidance when applicable.
- Optional image instructions for downstream image tools.

Requirements:

- Output MUST be Harmony messages JSON: an array of objects with optional `system`, zero-or-more `developer`, and optional `tool_config` and `image_instructions` fields.
- Do not include `role:"tool"` entries and do not include tool calls in this stage.
- Be explicit about safety, redaction of secrets, and source attribution.

Guidelines:

- Keep prompts minimal but sufficient. Avoid verbosity that wastes tokens.
- Prefer declarative constraints to prescriptive long-form text.
- If image generation is likely, include high-level image guidelines (style, quality, size) without locking to a provider-specific model.
- Annotate any assumptions clearly.

Steps:

1. Read the user request and any provided context.
2. Identify missing constraints and fill reasonable defaults.
3. Propose the system prompt that sets behavior boundaries and goals.
4. Provide optional developer prompts for formatting, tone, and structure.
5. Provide optional `tool_config` hints describing which tools are likely useful and with which key parameters.
6. Provide optional `image_instructions` when image generation is relevant.
7. Return a single JSON array as the only output.

Example minimal output (JSON):

[
  {
    "system": "You are a helpful assistant. Prioritize correctness and cite sources when tools provide them."
  },
  {
    "developer": "Return concise answers; use bullet lists when appropriate."
  },
  {
    "tool_config": {
      "enable_tools": ["searxng_search","http_fetch","readability_extract"],
      "hints": {"http_fetch.max_bytes": 1048576}
    }
  },
  {
    "image_instructions": {
      "style": "natural",
      "quality": "standard",
      "size": "1024x1024"
    }
  }
]

Extended guidance:

- System prompt should set policy boundaries (no PII leakage, safety, determinism when possible).
- Developer prompts can add formatting rules or domain-specific constraints.
- Tool config hints should be suggestive; the main stage may override them.
- Image instructions should avoid vendor lock-in and focus on intent.

Notes:

- Keep total token usage modest.
- Ensure the JSON is syntactically valid.
- Avoid embedding large text; link via citations instead.

Reference checklists (expand as needed):

- Inputs
  - Describe the user's primary goal.
  - List any constraints (time, budget, style).
  - Identify missing details and reasonable defaults.
- Outputs
  - Specify required sections and data shapes.
  - Define success criteria the main stage can verify.
- Safety
  - Avoid secrets; redact keys and tokens.
  - Respect robots and site policies in research.
- Tools
  - Prefer read-then-summarize workflows.
  - Cap network sizes and timeouts.

Quality bar:

- The pre-stage content must be sufficient for a non-interactive main call.
- Prompts must be reproducible and stable across runs.
- Avoid model-specific jargon unless required by capabilities.

Frequently used tool hints:

- `searxng_search`: prefer time range `month` for fresh content.
- `http_fetch`: `max_bytes` 1 MiB; follow ≤ 5 redirects.
- `readability_extract`: use for long-form articles; fall back to metadata.
- `pdf_extract`: avoid OCR unless explicitly allowed.
- `dedupe_rank`: group near-duplicates and pick representatives.
- `citation_pack`: normalize and optionally archive.

Image instructions template (when applicable):

- `style`: one of `natural|vivid`.
- `quality`: one of `standard|hd`.
- `size`: `1024x1024` by default.
- Keep content-safe, no sensitive data, no logos unless permitted.

Example tasks to consider:

- Research and summarize a topic with citations.
- Extract key facts from a PDF and produce a table.
- Generate step-by-step instructions for a procedure.
- Propose image concepts for an article header.

Do not output explanations outside the JSON array.

---

Additional elaboration (to ensure clarity and completeness):

- The `system` entry sets guardrails: identity, goals, non-goals.
- The `developer` entries add style and formatting constraints.
- The `tool_config` entry outlines suggested tools; it is advisory.
- The `image_instructions` entry captures defaults for image generation.
- If any section is unnecessary, omit it rather than emitting placeholders.
- Keep nouns concrete and avoid ambiguous verbs.
- Prefer consistent terminology across entries.
- Use American English unless the user specifies otherwise.
- For code-related tasks, specify language version and formatting tools.
- For data tasks, specify units, rounding, and acceptable error tolerances.
- For summarization, specify target length and inclusion/exclusion rules.
- For tables, specify column order, headers, and data types.
- For lists, specify ordering (by relevance, date, alphabetical).
- For citations, specify acceptable sources and minimum quality bar.
- For timelines, specify granularity (day, week, month) and time zone.
- For scheduling, specify preferred windows and constraints.
- For APIs, specify rate limits and pagination strategies.
- For retries, specify backoff policy and maximum attempts.
- For timeouts, specify per-call limits and overall budget.
- For errors, specify how to degrade gracefully.
- For privacy, specify redaction policies and data retention.
- For security, specify SSRF guard expectations and allowlists.
- For compliance, specify any domain-specific regulations.
- For logging, specify verbosity and structure.
- For observability, specify required metrics and traces.
- For testing, specify golden data and determinism.
- For performance, specify budgets and memory limits.
- For accessibility, specify alt text and contrast requirements.
- For internationalization, specify locale handling and encoding.
- For numerical work, specify precision and rounding rules.
- For randomness, specify seeds and determinism guidelines.
- For formatting, specify Markdown vs plain text expectations.
- For output, specify whether to include code fences and languages.
- For validation, specify schema and invariants.
- For edge cases, enumerate and clarify behavior.
- For fallbacks, specify secondary strategies.
- For caching, specify keys and TTLs.
- For state, specify persistence and scoping.
- For cleanup, specify deletion policies and audit trails.
- For versioning, specify compatibility and migration notes.
- For feature flags, specify rollouts and defaults.
- For user consent, specify prompts and storage.
- For rate control, specify burst vs steady-state limits.
- For concurrency, specify limits and contention strategies.
- For resource usage, specify budgets per phase.
- For tool failure, specify retries and substitutions.
- For summaries, specify structure and key sections.
- For diagrams, specify Mermaid types and layout.
- For images, specify composition, subject, and constraints.
- For typography, specify fonts and legibility.
- For color, specify palette and accessibility.
- For datasets, specify sources and licenses.
- For provenance, specify how to capture and present.
- For ethics, specify content boundaries.
- For disclaimers, specify when to show them.
- For conflict resolution, specify tie-break rules.
- For prioritization, specify ordering heuristics.
- For monitoring, specify alerts and thresholds.
- For maintenance, specify ownership and runbooks.
- For backups, specify cadence and retention.
- For restorations, specify RTO/RPO targets.
- For migrations, specify cutover and rollback.
- For deprecations, specify policy and timelines.
- For documentation, specify structure and examples.
- For onboarding, specify quickstarts and references.
- For support, specify channels and SLAs.
- For community, specify contribution guidelines.
- For licensing, specify terms and obligations.
- For branding, specify usage and restrictions.
- For analytics, specify metrics and privacy.
- For experiments, specify hypotheses and success metrics.
- For A/B tests, specify sampling and durations.
- For reporting, specify cadence and format.
- For audits, specify scope and evidence.
- For secrets, specify storage and rotation.
- For key management, specify roles and access.
- For infra, specify regions and redundancy.
- For cost, specify budgets and alerts.
- For scaling, specify triggers and steps.
- For queues, specify backpressure and dead-letter.
- For schedulers, specify cron expressions and windows.
- For notifications, specify channels and templates.
- For emails, specify DKIM/SPF/DMARC setup.
- For webhooks, specify retries and signatures.
- For APIs, specify auth and scopes.
- For clients, specify SDKs and versions.
- For logs, specify retention and scrubbing.
- For metrics, specify cardinality and costs.
- For traces, specify spans and attributes.
- For dashboards, specify panels and alerts.
- For runbooks, specify steps and verifications.
- For incident response, specify severities and comms.
- For postmortems, specify blamelessness and actions.
- For governance, specify reviews and approvals.
- For change management, specify CAB and windows.
- For risk, specify categories and mitigations.
- For threats, specify models and assumptions.
- For PKI, specify certs and rotation.
- For SSO, specify providers and claims.
- For RBAC, specify roles and scopes.
- For data, specify classification and handling.
- For retention, specify policies and exceptions.
- For deletion, specify requests and SLAs.
- For exports, specify formats and limits.
- For imports, specify validations and partials.
- For reconciliations, specify joins and conflicts.
- For ETL, specify batch sizes and windows.
- For streaming, specify topics and schemas.
- For ML, specify datasets and drift monitoring.
- For labeling, specify guidelines and QA.
- For feature stores, specify TTLs and lineage.
- For explainability, specify techniques and limits.
- For feedback, specify channels and triage.
- For abuse, specify detection and responses.
- For rate abuse, specify throttling and bans.
- For content, specify moderation and appeals.
- For localization, specify languages and testing.
- For telemetry, specify opt-in/out and docs.
- For APIs, specify versioning and sunset.
- For CI/CD, specify gates and rollbacks.
- For tests, specify coverage and flakes.
- For flaky tests, specify quarantine and limits.
- For IDEs, specify settings and formatters.
- For linters, specify rules and suppressions.
- For code reviews, specify checklists and SLAs.
- For merges, specify strategies and protections.
- For releases, specify cadence and notes.
- For packaging, specify artifacts and signatures.
- For binaries, specify platforms and flags.
- For containers, specify bases and scanning.
- For SBOM, specify generation and storage.
- For signatures, specify attestations and verifications.
- For supply chain, specify provenance and pinning.
- For dependencies, specify updates and vetting.
- For forks, specify sync strategy.
- For mirrors, specify freshness checks.
- For archives, specify formats and access.
- For APIs, specify limits and paginations.
- For storage, specify classes and lifecycle.
- For caches, specify invalidation and warming.
- For CDN, specify keys and TTLs.
- For edge, specify rules and fallbacks.
- For mobile, specify platforms and SDKs.
- For desktop, specify installers and updates.
- For web, specify compatibility and polyfills.
- For SEO, specify tags and sitemaps.
- For robots, specify disallows and delays.
- For sitemaps, specify generation cadence.
- For backups, specify verification and drills.
- For billing, specify models and proration.
- For taxation, specify regions and rates.
- For legal, specify terms and privacy.
- For compliance, specify audits and controls.
- For HR, specify onboarding and offboarding.
- For training, specify materials and refreshers.
- For knowledge base, specify structure and curation.
- For search, specify indexing and ranking.
- For UXR, specify studies and sampling.
- For roadmaps, specify horizons and themes.
