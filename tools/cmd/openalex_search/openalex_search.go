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

// input defines the expected stdin JSON for the tool.
type input struct {
	Q       string `json:"q"`
	From    string `json:"from"`
	To      string `json:"to"`
	PerPage int    `json:"per_page"`
}

// outputResult is the normalized result row produced by this tool.
type outputResult struct {
	Title           string `json:"title"`
	DOI             string `json:"doi,omitempty"`
	PublicationYear int    `json:"publication_year"`
	OpenAccessURL   string `json:"open_access_url,omitempty"`
	// Authorships carries through as an opaque list to avoid schema churn.
	Authorships  []any `json:"authorships"`
	CitedByCount int   `json:"cited_by_count"`
}

// output is the stdout JSON envelope produced by the tool.
type output struct {
	Results    []outputResult `json:"results"`
	NextCursor string         `json:"next_cursor,omitempty"`
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
	if strings.TrimSpace(in.Q) == "" {
		return errors.New("q is required")
	}
	baseURL, reqURL, err := prepareURLs(in)
	if err != nil {
		return err
	}
	client := newHTTPClient(resolveTimeout())
	start := time.Now()
	raw, status, retries, err := fetchWithRetry(client, baseURL, reqURL)
	if err != nil {
		return err
	}
	out := output{Results: mapResults(raw.Results)}
	if v := strings.TrimSpace(raw.NextCursor); v != "" {
		out.NextCursor = v
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	// Best-effort audit; ignore errors.
	_ = appendAudit(map[string]any{ //nolint:errcheck
		"ts":       time.Now().UTC().Format(time.RFC3339Nano),
		"tool":     "openalex_search",
		"url_host": baseURL.Hostname(),
		"status":   status,
		"ms":       time.Since(start).Milliseconds(),
		"retries":  retries,
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

func prepareURLs(in input) (*url.URL, *url.URL, error) {
	base := strings.TrimSpace(os.Getenv("OPENALEX_BASE_URL"))
	if base == "" {
		base = "https://api.openalex.org"
	}
	baseURL, err := url.Parse(base)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") {
		return nil, nil, errors.New("OPENALEX_BASE_URL must be a valid http/https URL")
	}
	if err := ssrfGuard(baseURL); err != nil {
		return nil, nil, err
	}
	reqURL, err := url.Parse(baseURL.String())
	if err != nil {
		return nil, nil, err
	}
	// Build: /works?search=...&per-page=...&from_publication_date=...&to_publication_date=...
	reqURL.Path = strings.TrimRight(reqURL.Path, "/") + "/works"
	q := reqURL.Query()
	// The OpenAlex API supports "search" as a generic text search; stick to that.
	q.Set("search", in.Q)
	if in.PerPage > 0 {
		if in.PerPage > 50 {
			// OpenAlex allows up to 200, but keep a conservative cap here
			in.PerPage = 50
		}
		q.Set("per-page", strconv.Itoa(in.PerPage))
	} else {
		q.Set("per-page", "10")
	}
	if strings.TrimSpace(in.From) != "" {
		q.Set("from_publication_date", in.From)
	}
	if strings.TrimSpace(in.To) != "" {
		q.Set("to_publication_date", in.To)
	}
	reqURL.RawQuery = q.Encode()
	return baseURL, reqURL, nil
}

type openalexResponse struct {
	Results    []map[string]any `json:"results"`
	NextCursor string           `json:"next_cursor"`
}

func fetchWithRetry(client *http.Client, baseURL *url.URL, reqURL *url.URL) (openalexResponse, int, int, error) {
	var out openalexResponse
	var lastStatus int
	var retries int
	for attempt := 0; attempt < 2; attempt++ {
		if err := ssrfGuard(baseURL); err != nil {
			return openalexResponse{}, 0, retries, err
		}
		req, err := http.NewRequest(http.MethodGet, reqURL.String(), nil)
		if err != nil {
			return openalexResponse{}, 0, retries, fmt.Errorf("new request: %w", err)
		}
		req.Header.Set("User-Agent", "agentcli-openalex/0.1")
		resp, err := client.Do(req)
		if err != nil {
			if isTimeout(err) && attempt == 0 {
				retries++
				backoffSleep(0, attempt)
				continue
			}
			return openalexResponse{}, 0, retries, fmt.Errorf("http: %w", err)
		}
		lastStatus = resp.StatusCode
		dec := json.NewDecoder(bufio.NewReader(resp.Body))
		if resp.StatusCode >= 500 && attempt == 0 {
			_ = resp.Body.Close() //nolint:errcheck
			retries++
			backoffSleep(0, attempt)
			continue
		}
		if err := dec.Decode(&out); err != nil {
			_ = resp.Body.Close() //nolint:errcheck
			if resp.StatusCode >= 500 && attempt == 0 {
				retries++
				backoffSleep(0, attempt)
				continue
			}
			return openalexResponse{}, lastStatus, retries, fmt.Errorf("decode json: %w", err)
		}
		_ = resp.Body.Close() //nolint:errcheck
		break
	}
	return out, lastStatus, retries, nil
}

func mapResults(rows []map[string]any) []outputResult {
	out := make([]outputResult, 0, len(rows))
	for _, r := range rows {
		var res outputResult
		if v, ok := r["display_name"].(string); ok {
			res.Title = v
		}
		if v, ok := r["title"].(string); ok && res.Title == "" {
			res.Title = v
		}
		if v, ok := r["doi"].(string); ok {
			res.DOI = v
		}
		if v, ok := r["publication_year"].(float64); ok {
			res.PublicationYear = int(v)
		} else if v, ok := r["publication_year"].(int); ok {
			res.PublicationYear = v
		}
		if oa, ok := r["open_access"].(map[string]any); ok {
			if v, ok := oa["oa_url"].(string); ok {
				res.OpenAccessURL = v
			}
		}
		if v, ok := r["authorships"].([]any); ok {
			res.Authorships = v
		}
		if v, ok := r["cited_by_count"].(float64); ok {
			res.CitedByCount = int(v)
		} else if v, ok := r["cited_by_count"].(int); ok {
			res.CitedByCount = v
		}
		out = append(out, res)
	}
	return out
}

func resolveTimeout() time.Duration {
	// 8s default per spec, can be overridden via HTTP_TIMEOUT_MS
	if v := strings.TrimSpace(os.Getenv("HTTP_TIMEOUT_MS")); v != "" {
		if ms, err := time.ParseDuration(v + "ms"); err == nil && ms > 0 {
			return ms
		}
	}
	return 8 * time.Second
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

func backoffSleep(_ int64, attempt int) {
	time.Sleep(time.Duration(100*(attempt+1)) * time.Millisecond)
}

// ssrfGuard blocks loopback, RFC1918, link-local, ULA, and .onion unless OPENALEX_ALLOW_LOCAL=1
func ssrfGuard(u *url.URL) error {
	host := u.Hostname()
	if host == "" {
		return errors.New("invalid host")
	}
	if strings.HasSuffix(strings.ToLower(host), ".onion") {
		return errors.New("SSRF blocked: onion domains are not allowed")
	}
	if os.Getenv("OPENALEX_ALLOW_LOCAL") == "1" {
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
