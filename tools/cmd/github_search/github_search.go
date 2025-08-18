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
	Q       string `json:"q"`
	Type    string `json:"type"`
	PerPage int    `json:"per_page"`
}

type rateInfo struct {
	Remaining int `json:"remaining"`
	Reset     int `json:"reset"`
}

type output struct {
	Results []map[string]any `json:"results"`
	Rate    rateInfo         `json:"rate"`
}

func main() {
	if err := run(); err != nil {
		var he *hintedError
		if errors.As(err, &he) {
			msg := strings.ReplaceAll(he.err.Error(), "\n", " ")
			hint := strings.ReplaceAll(he.hint, "\n", " ")
			if hint != "" {
				fmt.Fprintf(os.Stderr, "{\"error\":%q,\"hint\":%q}\n", msg, hint)
			} else {
				fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", msg)
			}
		} else {
			msg := strings.ReplaceAll(err.Error(), "\n", " ")
			fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", msg)
		}
		os.Exit(1)
	}
}

func run() error {
	in, err := decodeInput()
	if err != nil {
		return err
	}
	t := strings.ToLower(strings.TrimSpace(in.Type))
	switch t {
	case "repositories", "code", "issues", "commits":
	default:
		return errors.New("type must be one of: repositories, code, issues, commits")
	}
	if strings.TrimSpace(in.Q) == "" {
		return errors.New("q is required")
	}

	baseURL, reqURL, err := prepareURLs(t, in.Q, in.PerPage)
	if err != nil {
		return err
	}

	client := newHTTPClient(resolveTimeout())
	var lastStatus int
	var retries int
	start := time.Now()
	var body map[string]any
	for attempt := 0; attempt < 2; attempt++ {
		if err := ssrfGuard(baseURL); err != nil {
			return err
		}
		req, err := http.NewRequest(http.MethodGet, reqURL.String(), nil)
		if err != nil {
			return fmt.Errorf("new request: %w", err)
		}
		req.Header.Set("User-Agent", "agentcli-github-search/0.1")
		if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := client.Do(req)
		if err != nil {
			if isTimeout(err) && attempt == 0 {
				retries++
				backoffSleep(0, attempt)
				continue
			}
			return fmt.Errorf("http: %w", err)
		}
		lastStatus = resp.StatusCode

		rate := parseRate(resp.Header)

		dec := json.NewDecoder(bufio.NewReader(resp.Body))
		if resp.StatusCode >= 500 && attempt == 0 {
			_ = resp.Body.Close() //nolint:errcheck
			retries++
			backoffSleep(0, attempt)
			continue
		}
		if err := dec.Decode(&body); err != nil {
			_ = resp.Body.Close() //nolint:errcheck
			if resp.StatusCode >= 500 && attempt == 0 {
				retries++
				backoffSleep(0, attempt)
				continue
			}
			return fmt.Errorf("decode json: %w", err)
		}
		_ = resp.Body.Close() //nolint:errcheck

		if rate.Remaining == 0 {
			return hinted(errors.New("RATE_LIMITED"), "use GITHUB_TOKEN")
		}

		var items []any
		if v, ok := body["items"].([]any); ok {
			items = v
		}
		results := mapResults(t, items)
		out := output{Results: results, Rate: rate}
		if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
			return fmt.Errorf("encode json: %w", err)
		}
		_ = appendAudit(map[string]any{ //nolint:errcheck
			"ts":       time.Now().UTC().Format(time.RFC3339Nano),
			"tool":     "github_search",
			"url_host": baseURL.Hostname(),
			"type":     t,
			"status":   lastStatus,
			"ms":       time.Since(start).Milliseconds(),
			"retries":  retries,
			"query":    truncateQuery(in.Q),
		})
		return nil
	}
	return fmt.Errorf("unexpected retry exhaustion; last status %d", lastStatus)
}

func decodeInput() (input, error) {
	var in input
	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	if err := dec.Decode(&in); err != nil {
		return in, fmt.Errorf("parse json: %w", err)
	}
	return in, nil
}

func prepareURLs(t string, q string, perPage int) (*url.URL, *url.URL, error) {
	base := strings.TrimSpace(os.Getenv("GITHUB_BASE_URL"))
	if base == "" {
		base = "https://api.github.com"
	}
	baseURL, err := url.Parse(base)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") {
		return nil, nil, errors.New("GITHUB_BASE_URL must be a valid http/https URL")
	}
	if err := ssrfGuard(baseURL); err != nil {
		return nil, nil, err
	}
	reqURL, err := url.Parse(baseURL.String())
	if err != nil {
		return nil, nil, err
	}
	reqURL.Path = strings.TrimRight(reqURL.Path, "/") + "/search/" + t
	query := reqURL.Query()
	query.Set("q", q)
	if perPage <= 0 {
		perPage = 10
	}
	if perPage > 50 {
		perPage = 50
	}
	query.Set("per_page", strconv.Itoa(perPage))
	reqURL.RawQuery = query.Encode()
	return baseURL, reqURL, nil
}

func mapResults(t string, items []any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		switch t {
		case "repositories":
			out = append(out, mapRepoItem(m))
		case "code":
			out = append(out, mapCodeItem(m))
		case "issues":
			out = append(out, mapIssueItem(m))
		case "commits":
			out = append(out, mapCommitItem(m))
		}
	}
	return out
}

func mapRepoItem(m map[string]any) map[string]any {
	row := map[string]any{}
	if v, ok := m["full_name"].(string); ok {
		row["full_name"] = v
	}
	if v, ok := m["html_url"].(string); ok {
		row["url"] = v
	}
	if v, ok := m["description"].(string); ok {
		row["description"] = v
	}
	if v, ok := m["stargazers_count"].(float64); ok {
		row["stars"] = int(v)
	}
	return row
}

func mapCodeItem(m map[string]any) map[string]any {
	row := map[string]any{}
	if v, ok := m["name"].(string); ok {
		row["name"] = v
	}
	if v, ok := m["path"].(string); ok {
		row["path"] = v
	}
	if repo, ok := m["repository"].(map[string]any); ok {
		if fn, ok := repo["full_name"].(string); ok {
			row["repository"] = fn
		}
		if u, ok := repo["html_url"].(string); ok {
			row["repo_url"] = u
		}
	}
	if v, ok := m["html_url"].(string); ok {
		row["url"] = v
	}
	return row
}

func mapIssueItem(m map[string]any) map[string]any {
	row := map[string]any{}
	if v, ok := m["title"].(string); ok {
		row["title"] = v
	}
	if v, ok := m["html_url"].(string); ok {
		row["url"] = v
	}
	if v, ok := m["state"].(string); ok {
		row["state"] = v
	}
	return row
}

func mapCommitItem(m map[string]any) map[string]any {
	row := map[string]any{}
	if v, ok := m["sha"].(string); ok {
		row["sha"] = v
	}
	if v, ok := m["html_url"].(string); ok {
		row["url"] = v
	} else if v, ok := m["url"].(string); ok {
		row["url"] = v
	}
	if commit, ok := m["commit"].(map[string]any); ok {
		if msg, ok := commit["message"].(string); ok {
			row["message"] = msg
		}
	}
	return row
}

func parseRate(h http.Header) rateInfo {
	r := rateInfo{Remaining: -1, Reset: 0}
	if v := strings.TrimSpace(h.Get("X-RateLimit-Remaining")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			r.Remaining = n
		}
	}
	if v := strings.TrimSpace(h.Get("X-RateLimit-Reset")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			r.Reset = n
		}
	}
	return r
}

func resolveTimeout() time.Duration {
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

func ssrfGuard(u *url.URL) error {
	host := u.Hostname()
	if host == "" {
		return errors.New("invalid host")
	}
	if strings.HasSuffix(strings.ToLower(host), ".onion") {
		return errors.New("SSRF blocked: onion domains are not allowed")
	}
	if os.Getenv("GITHUB_ALLOW_LOCAL") == "1" {
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

func (h *hintedError) Error() string      { return h.err.Error() }
func hinted(err error, hint string) error { return &hintedError{err: err, hint: hint} }

func truncateQuery(q string) any {
	if len(q) <= 256 {
		return q
	}
	return map[string]any{"prefix": q[:256], "query_truncated": true}
}
