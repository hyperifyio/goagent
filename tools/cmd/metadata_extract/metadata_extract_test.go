package main_test

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	testutil "github.com/hyperifyio/goagent/tools/testutil"
)

func runTool(t *testing.T, bin string, input any) (string, string, error) {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(data)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

func TestMetadataExtract_ParsesOGTwitterJSONLD(t *testing.T) {
	bin := testutil.BuildTool(t, "metadata_extract")
	html := `<!doctype html><html><head>
      <meta property="og:title" content="OG Title">
      <meta name="twitter:card" content="summary_large_image">
      <script type="application/ld+json">{"@context":"https://schema.org","@type":"Article","headline":"LD Headline"}</script>
    </head><body>hi</body></html>`
	in := map[string]any{"html": html, "base_url": "https://example.org/page"}
	outStr, errStr, err := runTool(t, bin, in)
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	if !strings.Contains(outStr, "\"opengraph\"") {
		t.Fatalf("expected opengraph in output: %s", outStr)
	}
	if !strings.Contains(outStr, "\"twitter\"") {
		t.Fatalf("expected twitter in output: %s", outStr)
	}
	if !strings.Contains(outStr, "\"jsonld\"") {
		t.Fatalf("expected jsonld in output: %s", outStr)
	}
}

func TestMetadataExtract_RequiresInputs(t *testing.T) {
	bin := testutil.BuildTool(t, "metadata_extract")
	_, errStr, err := runTool(t, bin, map[string]any{"html": "", "base_url": ""})
	if err == nil {
		t.Fatalf("expected error for missing inputs")
	}
	if !strings.Contains(errStr, "required") {
		t.Fatalf("expected required error, got: %s", errStr)
	}
}
