# pdf_extract

Extract text from PDF pages with optional OCR via `tesseract`.

- stdin JSON:

```json
{"pdf_base64":"...","pages":[0,2,5]}
```

- stdout JSON:

```json
{"page_count":3,"pages":[{"index":0,"text":"..."}]}
```

- environment:
- `ENABLE_OCR`: when truthy (1/true/yes), attempts OCR for pages with no extracted text. If `tesseract` is missing, the tool exits non-zero with stderr JSON `{ "error": "OCR_UNAVAILABLE" }`.

- exit codes: 0 success; non-zero with stderr JSON `{ "error": "..." }` on failure.

## Examples

```bash
echo '{"pdf_base64":"'$(base64 -w0 sample.pdf)'"}' | ./tools/bin/pdf_extract | jq .page_count
```
