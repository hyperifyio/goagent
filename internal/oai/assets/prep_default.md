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

Notes:
- Keep total token usage modest.
- Ensure the JSON is syntactically valid.
- Avoid embedding large text; link via citations instead.
