package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	readability "github.com/go-shiori/go-readability"
)

type input struct {
	HTML    string `json:"html"`
	BaseURL string `json:"base_url"`
}

type output struct {
	Title       string `json:"title"`
	Byline      string `json:"byline,omitempty"`
	Text        string `json:"text"`
	ContentHTML string `json:"content_html"`
	Length      int    `json:"length"`
}

const maxHTMLBytes = 5 << 20 // 5 MiB

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
	if strings.TrimSpace(in.HTML) == "" {
		return errors.New("html is required")
	}
	if strings.TrimSpace(in.BaseURL) == "" {
		return errors.New("base_url is required")
	}
	// Parse base URL to the type expected by go-readability
	parsedBase, perr := url.Parse(in.BaseURL)
	if perr != nil || parsedBase.Scheme == "" || parsedBase.Host == "" {
		return errors.New("base_url must be an absolute URL")
	}
	if len(in.HTML) > maxHTMLBytes {
		return fmt.Errorf("html too large: limit %d bytes", maxHTMLBytes)
	}

	start := time.Now()
	art, err := readability.FromReader(strings.NewReader(in.HTML), parsedBase)
	if err != nil {
		return fmt.Errorf("readability extract: %w", err)
	}

	out := output{
		Title:       art.Title,
		Byline:      art.Byline,
		Text:        art.TextContent,
		ContentHTML: art.Content,
		Length:      art.Length,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	_ = appendAudit(map[string]any{ //nolint:errcheck
		"ts":     time.Now().UTC().Format(time.RFC3339Nano),
		"tool":   "readability_extract",
		"length": art.Length,
		"ms":     time.Since(start).Milliseconds(),
	})
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

// appendAudit writes an NDJSON line under .goagent/audit/YYYYMMDD.log at the repo root.
func appendAudit(entry any) error {
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	root := moduleRoot()
	dir := filepath.Join(root, ".goagent", "audit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	fname := time.Now().UTC().Format("20060102") + ".log"
	path := filepath.Join(dir, fname)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() //nolint:errcheck
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// moduleRoot walks upward from CWD to the directory containing go.mod; falls back to CWD.
func moduleRoot() string {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		return "."
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd
		}
		dir = parent
	}
}
