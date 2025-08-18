package main_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	testutil "github.com/hyperifyio/goagent/tools/testutil"
	gofpdf "github.com/jung-kurt/gofpdf"
)

// no output struct needed for the negative oversize test

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

type outPage struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
}

type outPayload struct {
	PageCount int       `json:"page_count"`
	Pages     []outPage `json:"pages"`
}

func buildPDF(t *testing.T, withText bool) []byte {
	t.Helper()
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	if withText {
		pdf.SetFont("Arial", "", 16)
		pdf.Cell(40, 10, "Hello PDF")
	} else {
		// Draw a rectangle to create non-text content
		pdf.SetLineWidth(1)
		pdf.Rect(10, 10, 50, 30, "D")
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		t.Fatalf("generate pdf: %v", err)
	}
	return buf.Bytes()
}

func runTool(t *testing.T, bin string, b64 string, env map[string]string) (int, outPayload, string, error) {
	t.Helper()
	in := map[string]any{"pdf_base64": b64}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	cmd := exec.Command(bin)
	cmd.Stdin = bytes.NewReader(data)
	if env != nil {
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return 0, outPayload{}, stderr.String(), err
	}
	var out outPayload
	if uerr := json.Unmarshal(stdout.Bytes(), &out); uerr != nil {
		t.Fatalf("decode output: %v; raw=%s", uerr, stdout.String())
	}
	return cmd.ProcessState.ExitCode(), out, stderr.String(), nil
}

func TestPdfExtract_TextPDF_Extracts(t *testing.T) {
	bin := testutil.BuildTool(t, "pdf_extract")
	pdfBytes := buildPDF(t, true)
	b64 := base64.StdEncoding.EncodeToString(pdfBytes)
	code, out, stderr, err := runTool(t, bin, b64, nil)
	if err != nil || code != 0 {
		t.Fatalf("run failed: code=%d err=%v stderr=%s", code, err, stderr)
	}
	if out.PageCount < 1 || len(out.Pages) < 1 {
		t.Fatalf("expected at least 1 page, got %+v", out)
	}
	norm := strings.ToLower(strings.ReplaceAll(out.Pages[0].Text, "\n", ""))
	norm = strings.ReplaceAll(norm, " ", "")
	if !strings.Contains(norm, "hellopdf") && !strings.Contains(norm, "hello") {
		t.Fatalf("expected extracted text to contain 'hello', got %q (norm=%q)", out.Pages[0].Text, norm)
	}
}

func TestPdfExtract_ImageOnly_NoOCR(t *testing.T) {
	bin := testutil.BuildTool(t, "pdf_extract")
	pdfBytes := buildPDF(t, false)
	b64 := base64.StdEncoding.EncodeToString(pdfBytes)
	code, out, stderr, err := runTool(t, bin, b64, nil)
	if err != nil || code != 0 {
		t.Fatalf("run failed: code=%d err=%v stderr=%s", code, err, stderr)
	}
	if out.PageCount < 1 || len(out.Pages) < 1 {
		t.Fatalf("expected at least 1 page, got %+v", out)
	}
	if strings.TrimSpace(out.Pages[0].Text) != "" {
		t.Fatalf("expected empty text without OCR, got %q", out.Pages[0].Text)
	}
}

func TestPdfExtract_ImageOnly_WithOCR_Mock(t *testing.T) {
	bin := testutil.BuildTool(t, "pdf_extract")
	pdfBytes := buildPDF(t, false)
	b64 := base64.StdEncoding.EncodeToString(pdfBytes)

	// Create a mock 'tesseract' in PATH
	mockDir := testutil.MakeRepoRelTempDir(t, "mockbin_")
	exeName := "tesseract"
	if runtime.GOOS == "windows" {
		exeName += ".bat"
	}
	mockPath := filepath.Join(mockDir, exeName)
	script := "#!/bin/sh\necho 'HELLO OCR'\n"
	if runtime.GOOS == "windows" {
		script = "@echo HELLO OCR\r\n"
	}
	if err := os.WriteFile(mockPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write mock tesseract: %v", err)
	}

	absMock, err := filepath.Abs(mockDir)
	if err != nil {
		t.Fatalf("abs mockdir: %v", err)
	}
	env := map[string]string{
		"PATH":       absMock,
		"ENABLE_OCR": "true",
	}
	code, out, stderr, err := runTool(t, bin, b64, env)
	if err != nil || code != 0 {
		t.Fatalf("run failed: code=%d err=%v stderr=%s", code, err, stderr)
	}
	if out.PageCount < 1 || len(out.Pages) < 1 {
		t.Fatalf("expected at least 1 page, got %+v", out)
	}
	if strings.TrimSpace(out.Pages[0].Text) != "HELLO OCR" {
		t.Fatalf("expected OCR text 'HELLO OCR', got %q", out.Pages[0].Text)
	}
}
