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
	"strconv"
	"strings"
	"time"
)

type input struct {
	Q          string   `json:"q"`
	TimeRange  string   `json:"time_range"`
	Categories []string `json:"categories"`
	Engines    []string `json:"engines"`
	Language   string   `json:"language"`
	Page       int      `json:"page"`
	Size       int      `json:"size"`
}

type result struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Snippet     string `json:"snippet"`
	Engine      string `json:"engine"`
	PublishedAt string `json:"published_at,omitempty"`
}

type output struct {
	Query   string   `json:"query"`
	Results []result `json:"results"`
}

func main() {
	if err := run(); err != nil {
		msg := strings.ReplaceAll(err.Error(), "\n", " ")
		// Optional hint exposure
		var he *hintedError
		if errors.As(err, &he) && he.hint != "" {
			fmt.Fprintf(os.Stderr, "{\"error\":%q,\"hint\":%q}\n", he.err.Error(), he.hint)
		} else {
			fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", msg)
		}
		os.Exit(1)
	}
}

// run orchestrates input parsing, request building, HTTP execution with retries,
// and audit emission. Keep this wrapper thin to satisfy lint complexity.
func run() error {
	in, err := decodeInput()
	if err != nil {
		return err
	}
	if strings.TrimSpace(in.Q) == "" {
		return errors.New("q is required")
	}
	baseURL, reqURL, err := prepareURLs(in)
	if err != nil {
		return err
	}
	timeout := resolveTimeout()
	client := newHTTPClient(timeout)
	start := time.Now()
	raw, lastStatus, retries, err := fetchWithRetries(client, baseURL, reqURL)
	if err != nil {
		return err
	}
	out := output{Query: in.Q, Results: parseResults(raw.Results)}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	entry := makeAudit(baseURL, in.Q, lastStatus, time.Since(start).Milliseconds(), retries)
	_ = appendAudit(entry) //nolint:errcheck
	return nil
}

func prepareURLs(in input) (*url.URL, *url.URL, error) {
	base := strings.TrimSpace(os.Getenv("SEARXNG_BASE_URL"))
	if base == "" {
		return nil, nil, hinted(fmt.Errorf("SEARXNG_BASE_URL is required"), "export SEARXNG_BASE_URL=http://localhost:8888")
	}
	baseURL, err := url.Parse(base)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") {
		return nil, nil, errors.New("SEARXNG_BASE_URL must be a valid http/https URL")
	}
	if err := ssrfGuard(baseURL); err != nil {
		return nil, nil, err
	}
	reqURL, err := url.Parse(baseURL.String())
	if err != nil {
		return nil, nil, err
	}
	reqURL.Path = strings.TrimRight(reqURL.Path, "/") + "/search"
	q := reqURL.Query()
	q.Set("format", "json")
	q.Set("q", in.Q)
	if in.TimeRange != "" {
		switch in.TimeRange {
		case "day", "week", "month", "year":
			q.Set("time_range", in.TimeRange)
		default:
			return nil, nil, errors.New("time_range must be one of: day, week, month, year")
		}
	}
	if len(in.Categories) > 0 {
		q.Set("categories", strings.Join(in.Categories, ","))
	}
	if len(in.Engines) > 0 {
		q.Set("engines", strings.Join(in.Engines, ","))
	}
	if in.Language != "" {
		q.Set("language", in.Language)
	}
	if in.Page > 0 {
		q.Set("pageno", strconv.Itoa(in.Page))
	}
	if in.Size > 0 {
		if in.Size > 50 {
			return nil, nil, errors.New("size must be <= 50")
		}
		q.Set("results", strconv.Itoa(in.Size))
	}
	reqURL.RawQuery = q.Encode()
	return baseURL, reqURL, nil
}

type searxRaw struct {
	Query   string           `json:"query"`
	Results []map[string]any `json:"results"`
}

func fetchWithRetries(client *http.Client, baseURL *url.URL, reqURL *url.URL) (searxRaw, int, int, error) {
	var retries int
	var lastStatus int
	var raw searxRaw
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			retries++
		}
		if err := ssrfGuard(baseURL); err != nil {
			return searxRaw{}, 0, retries, err
		}
		req, err := http.NewRequest(http.MethodGet, reqURL.String(), nil)
		if err != nil {
			return searxRaw{}, 0, retries, fmt.Errorf("new request: %w", err)
		}
		req.Header.Set("User-Agent", "agentcli-searxng/0.1")
		resp, err := client.Do(req)
		if err != nil {
			if isTimeout(err) && attempt < 2 {
				backoffSleep(0, attempt)
				continue
			}
			return searxRaw{}, 0, retries, fmt.Errorf("http: %w", err)
		}
		lastStatus = resp.StatusCode
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			if attempt < 2 {
				sleepMs := retryAfterMs(resp.Header.Get("Retry-After"))
				_ = resp.Body.Close() //nolint:errcheck
				backoffSleep(sleepMs, attempt)
				continue
			}
		}
		dec := json.NewDecoder(bufio.NewReader(resp.Body))
		if err := dec.Decode(&raw); err != nil {
			_ = resp.Body.Close() //nolint:errcheck
			if resp.StatusCode >= 500 && attempt < 2 {
				backoffSleep(0, attempt)
				continue
			}
			return searxRaw{}, lastStatus, retries, hinted(fmt.Errorf("decode json: %w", err), "verify SEARXNG_BASE_URL and that /search?format=json is reachable")
		}
		_ = resp.Body.Close() //nolint:errcheck
		break
	}
	return raw, lastStatus, retries, nil
}

func parseResults(rows []map[string]any) []result {
	var out []result
	for _, r := range rows {
		res := result{}
		if v, ok := r["title"].(string); ok {
			res.Title = v
		}
		if v, ok := r["url"].(string); ok {
			res.URL = v
		}
		if v, ok := r["content"].(string); ok {
			res.Snippet = v
		}
		if v, ok := r["snippet"].(string); ok && res.Snippet == "" {
			res.Snippet = v
		}
		if v, ok := r["engine"].(string); ok {
			res.Engine = v
		}
		if v, ok := r["publishedDate"].(string); ok {
			res.PublishedAt = v
		}
		if v, ok := r["published_at"].(string); ok && res.PublishedAt == "" {
			res.PublishedAt = v
		}
		out = append(out, res)
	}
	return out
}

func makeAudit(baseURL *url.URL, q string, status int, ms int64, retries int) map[string]any {
	entry := map[string]any{
		"ts":       time.Now().UTC().Format(time.RFC3339Nano),
		"tool":     "searxng_search",
		"url_host": baseURL.Hostname(),
		"status":   status,
		"ms":       ms,
		"retries":  retries,
	}
	if len(q) <= 256 {
		entry["query"] = q
	} else {
		entry["query"] = q[:256]
		entry["query_truncated"] = true
	}
	return entry
}

func decodeInput() (input, error) {
	var in input
	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	if err := dec.Decode(&in); err != nil {
		return in, fmt.Errorf("parse json: %w", err)
	}
	return in, nil
}

func resolveTimeout() time.Duration {
	if v := strings.TrimSpace(os.Getenv("HTTP_TIMEOUT_MS")); v != "" {
		if ms, err := time.ParseDuration(v + "ms"); err == nil && ms > 0 {
			return ms
		}
	}
	return 10 * time.Second
}

func newHTTPClient(timeout time.Duration) *http.Client {
	tr := &http.Transport{}
	return &http.Client{Timeout: timeout, Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return ssrfGuard(req.URL)
	}}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func retryAfterMs(h string) int64 {
	if h == "" {
		return 0
	}
	// Try integer seconds first
	if n, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && n >= 0 {
		return int64(n) * 1000
	}
	// Try HTTP-date
	if t, err := http.ParseTime(h); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d.Milliseconds()
		}
	}
	return 0
}

func backoffSleep(retryAfterMs int64, attempt int) {
	// jittered backoff: base 100ms * (attempt+1)
	d := time.Duration(100*(attempt+1)) * time.Millisecond
	if retryAfterMs > 0 {
		d = time.Duration(retryAfterMs) * time.Millisecond
	}
	time.Sleep(d)
}

// SSRF guard similar to http_fetch, with opt-out for local during tests via SEARXNG_ALLOW_LOCAL=1
func ssrfGuard(u *url.URL) error {
	host := u.Hostname()
	if host == "" {
		return errors.New("invalid host")
	}
	if strings.HasSuffix(strings.ToLower(host), ".onion") {
		return errors.New("SSRF blocked: onion domains are not allowed")
	}
	if os.Getenv("SEARXNG_ALLOW_LOCAL") == "1" {
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

type hintedError struct {
	err  error
	hint string
}

func (h *hintedError) Error() string { return h.err.Error() }

func hinted(err error, hint string) error { return &hintedError{err: err, hint: hint} }
