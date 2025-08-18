package main_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os/exec"
	"testing"

	testutil "github.com/hyperifyio/goagent/tools/testutil"
)

// no output struct needed for this negative test

func TestPdfExtract_OversizeReject(t *testing.T) {
	bin := testutil.BuildTool(t, "pdf_extract")
	// Create >20 MiB decoded payload; base64 will be larger but limit checks decoded bytes
	raw := bytes.Repeat([]byte{'A'}, 20*1024*1024+1)
	b64 := base64.StdEncoding.EncodeToString(raw)
	in := map[string]any{"pdf_base64": b64}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(data)
	if err := cmd.Run(); err == nil {
		t.Fatalf("expected oversize rejection error")
	}
}
