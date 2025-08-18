package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	pdf "github.com/ledongthuc/pdf"
)

type input struct {
	PDFBase64 string `json:"pdf_base64"`
	Pages     []int  `json:"pages"`
}

type pageOut struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
}

type output struct {
	PageCount int       `json:"page_count"`
	Pages     []pageOut `json:"pages"`
}

const maxPDFSizeBytes = 20 * 1024 * 1024 // 20 MiB

func main() {
	if err := run(); err != nil {
		msg := strings.ReplaceAll(err.Error(), "\n", " ")
		fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", msg)
		os.Exit(1)
	}
}

func run() error {
	in, err := decodeInput()
	if err != nil {
		return err
	}
	if strings.TrimSpace(in.PDFBase64) == "" {
		return errors.New("pdf_base64 is required")
	}
	data, err := base64.StdEncoding.DecodeString(in.PDFBase64)
	if err != nil {
		return fmt.Errorf("decode base64: %w", err)
	}
	if len(data) > maxPDFSizeBytes {
		return fmt.Errorf("pdf too large: %d bytes (limit %d)", len(data), maxPDFSizeBytes)
	}

	// Write to a temp file for the parser and potential OCR tools
	tmpDir, err := os.MkdirTemp("", "pdf_extract_*")
	if err != nil {
		return fmt.Errorf("mktemp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	pdfPath := filepath.Join(tmpDir, "in.pdf")
	if err := os.WriteFile(pdfPath, data, 0o600); err != nil {
		return fmt.Errorf("write temp pdf: %w", err)
	}

	// Parse PDF
	f, r, err := pdf.Open(pdfPath)
	if err != nil {
		return fmt.Errorf("open pdf: %w", err)
	}
	defer func() { _ = f.Close() }()

	totalPages := r.NumPage()
	targetPages, err := normalizePages(in.Pages, totalPages)
	if err != nil {
		return err
	}

	texts := make([]string, totalPages)
	emptyFlags := make([]bool, totalPages)
	for i := 1; i <= totalPages; i++ { // 1-based
		p := r.Page(i)
		if p.V.IsNull() {
			texts[i-1] = ""
			emptyFlags[i-1] = true
			continue
		}
		content := p.Content()
		var b strings.Builder
		// The library exposes extracted text spans under Content.Text ([]pdf.Text)
		for _, span := range content.Text {
			s := span.S
			if s != "" {
				b.WriteString(s)
				b.WriteString("\n")
			}
		}
		txt := strings.TrimSpace(b.String())
		texts[i-1] = txt
		emptyFlags[i-1] = (txt == "")
	}

	// Optional OCR for empty pages when enabled
	if isOCREnabled() && anyEmptyRequested(emptyFlags, targetPages) {
		ocrTexts, ocrErr := runTesseractOCR(pdfPath, totalPages, countEmptyRequested(emptyFlags, targetPages))
		if ocrErr != nil {
			return ocrErr
		}
		for _, idx := range targetPages {
			if idx >= 0 && idx < len(texts) && strings.TrimSpace(texts[idx]) == "" {
				if idx < len(ocrTexts) {
					texts[idx] = strings.TrimSpace(ocrTexts[idx])
				}
			}
		}
	}

	var outPages []pageOut
	for _, idx := range targetPages {
		if idx < 0 || idx >= totalPages {
			continue
		}
		outPages = append(outPages, pageOut{Index: idx, Text: texts[idx]})
	}

	out := output{PageCount: totalPages, Pages: outPages}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	return nil
}

func decodeInput() (input, error) {
	var in input
	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	if err := dec.Decode(&in); err != nil {
		return in, fmt.Errorf("parse json: %w", err)
	}
	return in, nil
}

func normalizePages(pages []int, total int) ([]int, error) {
	if total < 0 {
		total = 0
	}
	if len(pages) == 0 {
		out := make([]int, total)
		for i := 0; i < total; i++ {
			out[i] = i
		}
		return out, nil
	}
	seen := make(map[int]struct{}, len(pages))
	out := make([]int, 0, len(pages))
	for _, p := range pages {
		if p < 0 || p >= total {
			return nil, fmt.Errorf("page index out of range: %d (total %d)", p, total)
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out, nil
}

func anyEmptyRequested(empty []bool, targets []int) bool {
	for _, idx := range targets {
		if idx >= 0 && idx < len(empty) && empty[idx] {
			return true
		}
	}
	return false
}

func countEmptyRequested(empty []bool, targets []int) int {
	c := 0
	for _, idx := range targets {
		if idx >= 0 && idx < len(empty) && empty[idx] {
			c++
		}
	}
	if c == 0 {
		return 1
	}
	return c
}

func isOCREnabled() bool {
	v := strings.TrimSpace(os.Getenv("ENABLE_OCR"))
	if v == "" {
		return false
	}
	v = strings.ToLower(v)
	return v == "1" || v == "true" || v == "yes"
}

var errOCRUnavailable = errors.New("ocr unavailable")

func runTesseractOCR(pdfPath string, pageCount int, emptyRequested int) ([]string, error) {
	if _, err := exec.LookPath("tesseract"); err != nil {
		return nil, errOCRUnavailable
	}
	if pageCount <= 0 {
		return []string{}, nil
	}
	timeout := 10 * time.Second * time.Duration(emptyRequested)
	if timeout < 10*time.Second {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tesseract", pdfPath, "stdout")
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("ocr timeout after %s", timeout)
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("ocr failed: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("ocr failed: %v", err)
	}
	parts := strings.Split(string(out), "\f")
	if len(parts) < pageCount {
		tmp := make([]string, pageCount)
		copy(tmp, parts)
		parts = tmp
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts, nil
}
