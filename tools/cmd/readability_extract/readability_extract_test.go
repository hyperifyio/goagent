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

func TestReadabilityExtract_Simple(t *testing.T) {
	bin := testutil.BuildTool(t, "readability_extract")
	html := `<!doctype html><html><head><title>Example</title></head><body><nav>Links</nav><article><h1>My Title</h1><p>Hello <b>world</b>.</p></article></body></html>`
	input := map[string]any{"html": html, "base_url": "https://example.org/page"}
	outStr, errStr, err := runTool(t, bin, input)
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	if !strings.Contains(outStr, "\"title\":") {
		t.Fatalf("missing title in output: %s", outStr)
	}
	if !strings.Contains(outStr, "Hello") {
		t.Fatalf("expected extracted text to include 'Hello': %s", outStr)
	}
}

func TestReadabilityExtract_NavHeavy(t *testing.T) {
	bin := testutil.BuildTool(t, "readability_extract")
	html := `<!doctype html><html><body><div id="nav">home | about | contact</div><div id="content"><h1>Article Heading</h1><p>Core content here.</p></div></body></html>`
	outStr, errStr, err := runTool(t, bin, map[string]any{"html": html, "base_url": "https://example.org/x"})
	if err != nil {
		t.Fatalf("run error: %v, stderr=%s", err, errStr)
	}
	if !strings.Contains(outStr, "Article Heading") {
		t.Fatalf("expected heading present: %s", outStr)
	}
	if !strings.Contains(outStr, "Core content here") {
		t.Fatalf("expected article text present: %s", outStr)
	}
}

func TestReadabilityExtract_LargeRejected(t *testing.T) {
	bin := testutil.BuildTool(t, "readability_extract")
	big := strings.Repeat("A", (5<<20)+1)
	outStr, errStr, err := runTool(t, bin, map[string]any{"html": big, "base_url": "https://e/x"})
	if err == nil {
		t.Fatalf("expected error for oversized html, got ok: %s", outStr)
	}
	if !strings.Contains(errStr, "html too large") {
		t.Fatalf("expected size error, got: %s", errStr)
	}
}
