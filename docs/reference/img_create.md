# Image generation tool (img_create)

Generate image(s) via an OpenAI‑compatible Images API and either save PNG files into your repository (default) or return base64 on demand. This tool is invoked by the agent as a function tool using JSON over stdin/stdout with strict timeouts and no shell.

## Contracts

- Stdin: single JSON object matching the parameters below
- Stdout (success): single‑line JSON result
- Stderr (failure): single‑line JSON object `{ "error": "...", "hint?": "..." }`, non‑zero exit

### Example: save files (default)

Input to stdin:

```json
{
  "prompt": "tiny-pixel",
  "n": 1,
  "size": "1024x1024",
  "save": {"dir": "assets", "basename": "img", "ext": "png"}
}
```

Output on stdout (shape shown; paths and hashes will vary):

```json
{
  "saved": [
    {"path": "assets/img_001.png", "bytes": 95, "sha256": "<hex>"}
  ],
  "n": 1,
  "size": "1024x1024",
  "model": "gpt-image-1"
}
```

### Example: return base64 instead of saving

Input to stdin:

```json
{
  "prompt": "tiny-pixel",
  "n": 1,
  "return_b64": true
}
```

Output on stdout by default elides base64 for transcript hygiene:

```json
{
  "images": [
    {"b64": "", "hint": "b64 elided"}
  ]
}
```

Set `IMG_CREATE_DEBUG_B64=1` (or `DEBUG_B64=1`) to include base64 in stdout for debugging.

## Parameters

| Name        | Type      | Required | Default       | Constraints                               | Notes |
|-------------|-----------|----------|---------------|-------------------------------------------|-------|
| `prompt`    | string    | yes      | —             | non‑empty                                  | Text prompt for the image(s).
| `n`         | integer   | no       | 1             | 1 ≤ n ≤ 4                                  | Number of images to generate.
| `size`      | string    | no       | `1024x1024`   | regex `^\d{3,4}x\d{3,4}$`                 | Width x height in pixels.
| `model`     | string    | no       | `gpt-image-1` | —                                         | Passed as‑is to the Images API.
| `return_b64`| boolean   | no       | false         | —                                         | When true, returns base64 JSON instead of writing files.
| `save.dir`  | string    | cond.    | —             | repo‑relative; must not escape repo root   | Required when `return_b64=false` (default).
| `save.basename` | string| no       | `img`         | must not contain path separators           | Filename stem; tool appends `_<001..>.ext`.
| `save.ext`  | string    | no       | `png`         | enum: `png`                                | Output format; currently PNG only.
| `extras`    | object    | no       | —             | shallow map of string→primitive            | Optional pass-through for known keys like `background:"transparent"`; only primitives are allowed; core keys are not overridden.

Notes:
- When saving files, the tool writes atomically to `save.dir` and returns file metadata including SHA‑256.
- Filenames are generated as `<basename>_NNN.<ext>` with zero‑padded indices starting at 001.

## HTTP behavior

- Endpoint: `POST ${OAI_IMAGE_BASE_URL:-$OAI_BASE_URL}/v1/images/generations`
- Request body:
  - `{ "model", "prompt", "n", "size", "response_format": "b64_json" }`
- Headers:
  - `Content-Type: application/json`
  - `Authorization: Bearer $OAI_API_KEY` (if present)
- Timeout: from `OAI_HTTP_TIMEOUT` (duration, default 120s)
- Retries: up to 2 retries (3 total attempts) on timeouts, HTTP 429, and 5xx with backoff `250ms, 500ms, 1s`
- Error mapping: non‑2xx responses attempt to extract a useful message from `{error}` or `{error:{message}}`; otherwise emit `api status <code>`

## Environment

- `OAI_IMAGE_BASE_URL`: Base URL for Images API (preferred)
- `OAI_BASE_URL`: Fallback base URL when `OAI_IMAGE_BASE_URL` is unset
- `OAI_API_KEY`: API key for authorization (optional for mocks)
- `OAI_HTTP_TIMEOUT`: HTTP timeout (e.g., `90s`)
- `IMG_CREATE_DEBUG_B64` / `DEBUG_B64`: When set truthy, include base64 in stdout for `return_b64=true`

The manifest allowlist passes through only the following variables to the tool:

```json
["OAI_API_KEY", "OAI_BASE_URL", "OAI_IMAGE_BASE_URL", "OAI_HTTP_TIMEOUT"]
```

## Underlying API (cURL)

For transparency, the tool issues the equivalent of:

```bash
curl -sS -X POST "$OAI_BASE_URL/v1/images/generations" \
  -H "Authorization: Bearer $OAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-image-1",
    "prompt": "tiny-pixel",
    "n": 1,
    "size": "1024x1024",
    "response_format": "b64_json"
  }'
```

When `OAI_IMAGE_BASE_URL` is set, it is used instead of `OAI_BASE_URL`.

## Safety notes

- Strict repository‑relative writes: `save.dir` must be within the repository; absolute paths and `..` escapes are rejected.
- No shell execution: the tool is executed via argv only; stdin/stdout are JSON.
- Transcript hygiene: by default, base64 is elided from stdout to prevent large transcripts. Enable debug envs to view base64 locally.

## Related documentation

- Images & vision guide: [OpenAI Images docs](https://platform.openai.com/docs/guides/images)
- Model reference: [OpenAI model catalog (gpt-image-1)](https://platform.openai.com/docs/models)
- Tools manifest reference: see `docs/reference/tools-manifest.md`
