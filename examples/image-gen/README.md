### Image generation example (direct tool invocation)

This example shows how to call the `img_create` tool directly (without running the agent), saving PNGs under `assets/` and printing the result to stdout.

Prerequisites:
- `make build-tools` has produced `./tools/bin/img_create` (or `./tools/bin/img_create.exe` on Windows)
- `OAI_API_KEY` is set; optional `OAI_BASE_URL`/`OAI_IMAGE_BASE_URL`
- Go 1.21+ installed (for the Go runner)

Quick start (from repo root):

```bash
make build-tools

# Set your API key (and base URL if needed)
export OAI_API_KEY=your-key

# Run the Go helper which invokes ./tools/bin/img_create with JSON on stdin
make -C examples/image-gen run PROMPT="tiny illustrative banner" SIZE=512x512 BASENAME=banner

# Expect: one or more PNGs under ./assets/ (e.g., assets/banner_001.png)
```

Notes:
- The Go runner simply constructs the JSON payload and streams it to `./tools/bin/img_create`, then prints the tool's JSON result.
- To request multiple images, pass `N=2` (max 4). To return base64 instead of writing files, pass `RETURN_B64=1` (the tool elides large base64 in stdout by default).
- Windows: the runner resolves the `.exe` suffix automatically.

Make targets:

```bash
# From repo root
make -C examples/image-gen help
```

Troubleshooting:
- If you see an error about credentials or timeouts, consult `docs/runbooks/troubleshooting.md` (Image generation section) and ensure `OAI_API_KEY` is set.
