package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type input struct {
	Doc struct {
		Title       string `json:"title"`
		URL         string `json:"url"`
		PublishedAt string `json:"published_at"`
	} `json:"doc"`
	Archive struct {
		Wayback bool `json:"wayback"`
	} `json:"archive"`
}

type output struct {
	Title      string `json:"title,omitempty"`
	URL        string `json:"url"`
	Host       string `json:"host"`
	AccessedAt string `json:"accessed_at"`
	ArchiveURL string `json:"archive_url,omitempty"`
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
	if strings.TrimSpace(in.Doc.URL) == "" {
		return errors.New("doc.url is required")
	}
	u, err := url.Parse(in.Doc.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("doc.url must be a valid http/https URL")
	}
	out := output{
		Title:      strings.TrimSpace(in.Doc.Title),
		URL:        in.Doc.URL,
		Host:       u.Hostname(),
		AccessedAt: time.Now().UTC().Format(time.RFC3339),
	}

	archived := false
	start := time.Now()
	if in.Archive.Wayback {
		archiveURL, aerr := waybackLookup(in.Doc.URL)
		if aerr != nil {
			return aerr
		}
		if archiveURL != "" {
			out.ArchiveURL = archiveURL
			archived = true
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	_ = appendAudit(map[string]any{ //nolint:errcheck
		"ts":       time.Now().UTC().Format(time.RFC3339Nano),
		"tool":     "citation_pack",
		"url_host": out.Host,
		"archived": archived,
		"ms":       time.Since(start).Milliseconds(),
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

// waybackLookup performs a lookup against the Wayback Machine compatible endpoint.
// It respects WAYBACK_BASE_URL if set, otherwise defaults to https://web.archive.org.
// Enforces a 3s timeout and SSRF guard on the base URL.
func waybackLookup(targetURL string) (string, error) {
	base := strings.TrimSpace(os.Getenv("WAYBACK_BASE_URL"))
	if base == "" {
		base = "https://web.archive.org"
	}
	baseURL, err := url.Parse(base)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") {
		return "", errors.New("WAYBACK_BASE_URL must be a valid http/https URL")
	}
	if err := ssrfGuard(baseURL); err != nil {
		return "", err
	}
	reqURL, err := url.Parse(baseURL.String())
	if err != nil {
		return "", err
	}
	reqURL.Path = strings.TrimRight(reqURL.Path, "/") + "/available"
	q := reqURL.Query()
	q.Set("url", targetURL)
	reqURL.RawQuery = q.Encode()
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(reqURL.String())
	if err != nil {
		return "", fmt.Errorf("http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck
	var raw struct {
		ArchivedSnapshots struct {
			Closest struct {
				Available bool   `json:"available"`
				URL       string `json:"url"`
				Timestamp string `json:"timestamp"`
			} `json:"closest"`
		} `json:"archived_snapshots"`
	}
	if err := json.NewDecoder(bufio.NewReader(resp.Body)).Decode(&raw); err != nil {
		return "", fmt.Errorf("decode json: %w", err)
	}
	if raw.ArchivedSnapshots.Closest.Available {
		return raw.ArchivedSnapshots.Closest.URL, nil
	}
	return "", nil
}

// ssrfGuard similar to other networked tools; can be bypassed in tests via CITATION_PACK_ALLOW_LOCAL=1
func ssrfGuard(u *url.URL) error {
	host := u.Hostname()
	if host == "" {
		return errors.New("invalid host")
	}
	if strings.HasSuffix(strings.ToLower(host), ".onion") {
		return errors.New("SSRF blocked: onion domains are not allowed")
	}
	if os.Getenv("CITATION_PACK_ALLOW_LOCAL") == "1" {
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return errors.New("SSRF blocked: cannot resolve host")
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return errors.New("SSRF blocked: private or loopback address")
		}
	}
	return nil
}

func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
		if v4[0] == 10 {
			return true
		}
		if v4[0] == 172 && v4[1]&0xf0 == 16 {
			return true
		}
		if v4[0] == 192 && v4[1] == 168 {
			return true
		}
		if v4[0] == 169 && v4[1] == 254 {
			return true
		}
		if v4[0] == 127 {
			return true
		}
		return false
	}
	if ip.Equal(net.ParseIP("::1")) {
		return true
	}
	if ip[0] == 0xfe && (ip[1]&0xc0) == 0x80 {
		return true
	}
	if ip[0]&0xfe == 0xfc {
		return true
	}
	return false
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
