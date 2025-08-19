# Image flows examples

This page shows two working invocations demonstrating (1) same backend for chat+image and (2) split backends (e.g., OSS chat + OpenAI images).

Prerequisites:
- Go 1.21+
- `make build build-tools`
- A valid Images API key in `OAI_API_KEY` (or vendor equivalent)

Same backend for chat and image (both use OAI_BASE_URL):

```bash
export OAI_BASE_URL="${OAI_BASE_URL:-https://api.openai.com/v1}"
export OAI_API_KEY=your-key

./bin/agentcli \
  -tools ./tools.json \
  -prompt "Generate a tiny illustrative image using img_create and save it under assets/ with basename banner" \
  -model gpt-5 \
  -max-steps 3 \
  -debug
# Expect: PNG(s) under assets/ and a concise final message
```

Split backends (chat via OSS; images via OpenAI):

```bash
export OAI_BASE_URL="http://localhost:8080/v1"       # OSS chat
export OAI_IMAGE_BASE_URL="https://api.openai.com/v1" # Images
export OAI_API_KEY=your-key

./bin/agentcli \
  -tools ./tools.json \
  -prompt "Use img_create to render a small banner and save to assets/" \
  -model oss-gpt-20b \
  -max-steps 3 \
  -debug
```

Notes:
- Ensure `img_create` is in `tools.json` with `command: ["./tools/bin/img_create"]`.
- To avoid committing large binaries, consider adding `assets/` to `.gitignore` in your project.
- To return base64 instead of saving, modify the tool call to set `{"return_b64":true}` and set `IMG_CREATE_DEBUG_B64=1` if you want base64 printed in stdout.
