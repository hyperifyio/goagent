package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
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

	// Minimal stub: no parsing yet. Return empty pages with count 0 for now.
	out := output{PageCount: 0, Pages: []pageOut{}}
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
