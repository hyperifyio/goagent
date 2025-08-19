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
)

type input struct {
	HTML    string `json:"html"`
	BaseURL string `json:"base_url"`
}

type output struct {
	OpenGraph map[string]any `json:"opengraph"`
	Twitter   map[string]any `json:"twitter"`
	JSONLD    []any          `json:"jsonld"`
}

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
	if u, perr := url.Parse(in.BaseURL); perr != nil || u.Scheme == "" || u.Host == "" {
		return errors.New("base_url must be an absolute URL")
	}

	// Minimal implementation that scans meta tags and JSON-LD scripts.
	og, tw, ld := extractMetadata(in.HTML)

	out := output{OpenGraph: og, Twitter: tw, JSONLD: ld}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	_ = appendAudit(map[string]any{ //nolint:errcheck
		"ts":   time.Now().UTC().Format(time.RFC3339Nano),
		"tool": "metadata_extract",
		"ms":   0,
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

func extractMetadata(html string) (map[string]any, map[string]any, []any) {
	og := map[string]any{}
	tw := map[string]any{}
	var ld []any

	// Very basic parsing without external deps: regex-free naive scans.
	lower := strings.ToLower(html)
	// Extract <meta property="og:..." content="...">
	idx := 0
	for {
		i := strings.Index(lower[idx:], "<meta")
		if i < 0 {
			break
		}
		i += idx
		end := strings.Index(lower[i:], ">")
		if end < 0 {
			break
		}
		tag := html[i : i+end+1]
		p := attrValue(tag, "property")
		n := attrValue(tag, "name")
		c := attrValue(tag, "content")
		if strings.HasPrefix(strings.ToLower(p), "og:") && c != "" {
			og[p] = c
		}
		if strings.HasPrefix(strings.ToLower(n), "twitter:") && c != "" {
			tw[n] = c
		}
		idx = i + end + 1
	}
	// Extract <script type="application/ld+json"> ... </script>
	idx = 0
	for {
		i := strings.Index(lower[idx:], "<script")
		if i < 0 {
			break
		}
		i += idx
		closeTag := strings.Index(lower[i:], ">")
		if closeTag < 0 {
			break
		}
		tag := html[i : i+closeTag+1]
		t := attrValue(tag, "type")
		if strings.EqualFold(strings.TrimSpace(t), "application/ld+json") {
			// find </script>
			rest := html[i+closeTag+1:]
			end := strings.Index(strings.ToLower(rest), "</script>")
			if end >= 0 {
				payload := strings.TrimSpace(rest[:end])
				var v any
				if err := json.Unmarshal([]byte(payload), &v); err == nil {
					switch vv := v.(type) {
					case []any:
						ld = append(ld, vv...)
					default:
						ld = append(ld, vv)
					}
				}
				idx = i + closeTag + 1 + end + len("</script>")
				continue
			}
		}
		idx = i + closeTag + 1
	}

	return og, tw, ld
}

// naive attribute value extractor for patterns like key="value"
func attrValue(tag string, key string) string {
	// search case-insensitively for key=
	lower := strings.ToLower(tag)
	k := strings.ToLower(key) + "="
	j := strings.Index(lower, k)
	if j < 0 {
		return ""
	}
	// find quote type after =
	start := j + len(k)
	if start >= len(tag) {
		return ""
	}
	quote := tag[start]
	if quote != '"' && quote != '\'' {
		return ""
	}
	start++
	end := strings.IndexByte(tag[start:], byte(quote))
	if end < 0 {
		return ""
	}
	return tag[start : start+end]
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
