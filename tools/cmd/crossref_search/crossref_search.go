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
	Q    string `json:"q"`
	Rows int    `json:"rows"`
}

// outputResult is the normalized result row produced by this tool.
type outputResult struct {
	Title      string `json:"title"`
	DOI        string `json:"doi"`
	Issued     string `json:"issued"`
	Container  string `json:"container"`
	TitleShort string `json:"title_short,omitempty"`
}

// output is the stdout JSON envelope produced by the tool.
type output struct {
	Results []outputResult `json:"results"`
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
	mailto := strings.TrimSpace(os.Getenv("CROSSREF_MAILTO"))
	if mailto == "" {
		return errors.New("CROSSREF_MAILTO is required")
	}
	baseURL, reqURL, err := prepareURLs(in, mailto)
	if err != nil {
		return err
	}
	client := newHTTPClient(resolveTimeout())
	start := time.Now()
	status, body, err := doRequest(client, baseURL, reqURL, mailto)
	if err != nil {
		return err
	}
	rows, err := parseCrossref(body)
	if err != nil {
		return err
	}
	out := output{Results: mapResults(rows)}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	_ = appendAudit(map[string]any{ //nolint:errcheck
		"ts":       time.Now().UTC().Format(time.RFC3339Nano),
		"tool":     "crossref_search",
		"url_host": baseURL.Hostname(),
		"status":   status,
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

func prepareURLs(in input, mailto string) (*url.URL, *url.URL, error) {
	base := strings.TrimSpace(os.Getenv("CROSSREF_BASE_URL"))
	if base == "" {
		base = "https://api.crossref.org"
	}
	baseURL, err := url.Parse(base)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") {
		return nil, nil, errors.New("CROSSREF_BASE_URL must be a valid http/https URL")
	}
	if err := ssrfGuard(baseURL); err != nil {
		return nil, nil, err
	}
	reqURL, err := url.Parse(baseURL.String())
	if err != nil {
		return nil, nil, err
	}
	reqURL.Path = strings.TrimRight(reqURL.Path, "/") + "/works"
	q := reqURL.Query()
	q.Set("query", in.Q)
	if in.Rows > 0 {
		if in.Rows > 50 {
			in.Rows = 50
		}
		q.Set("rows", strconv.Itoa(in.Rows))
	} else {
		q.Set("rows", "10")
	}
	q.Set("mailto", mailto)
	reqURL.RawQuery = q.Encode()
	return baseURL, reqURL, nil
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

func resolveTimeout() time.Duration {
	if v := strings.TrimSpace(os.Getenv("HTTP_TIMEOUT_MS")); v != "" {
		if ms, err := time.ParseDuration(v + "ms"); err == nil && ms > 0 {
			return ms
		}
	}
	return 8 * time.Second
}

func doRequest(client *http.Client, baseURL *url.URL, reqURL *url.URL, mailto string) (int, []byte, error) {
	if err := ssrfGuard(baseURL); err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequest(http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return 0, nil, fmt.Errorf("new request: %w", err)
	}
	ua := "agentcli-crossref/0.1 (" + mailto + ")"
	req.Header.Set("User-Agent", ua)
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck
	if resp.StatusCode == http.StatusTooManyRequests {
		ra := strings.TrimSpace(resp.Header.Get("Retry-After"))
		if ra == "" {
			return resp.StatusCode, nil, errors.New("RATE_LIMITED: retry later")
		}
		return resp.StatusCode, nil, fmt.Errorf("RATE_LIMITED: retry after %s seconds", ra)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, nil, fmt.Errorf("http status: %d", resp.StatusCode)
	}
	data, err := ioReadAllLimit(resp.Body, 4*1024*1024)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, data, nil
}

// parseCrossref decodes the Crossref message.items array.
func parseCrossref(data []byte) ([]map[string]any, error) {
	var payload struct {
		Message struct {
			Items []map[string]any `json:"items"`
		} `json:"message"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}
	return payload.Message.Items, nil
}

func mapResults(rows []map[string]any) []outputResult {
	out := make([]outputResult, 0, len(rows))
	for _, r := range rows {
		var res outputResult
		res.Title = firstStringField(r, "title")
		if s, ok := r["DOI"].(string); ok {
			res.DOI = s
		}
		res.Container = firstStringField(r, "container-title")
		res.TitleShort = firstStringField(r, "short-title")
		res.Issued = parseIssuedField(r)
		out = append(out, res)
	}
	return out
}

// firstStringField returns the first string value for a field that may be a string or array of strings.
func firstStringField(m map[string]any, key string) string {
	if v, ok := m[key].([]any); ok && len(v) > 0 {
		if s, ok := v[0].(string); ok {
			return s
		}
	}
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

// parseIssuedField formats Crossref issued.date-parts into YYYY[-MM[-DD]].
func parseIssuedField(m map[string]any) string {
	issued, ok := m["issued"].(map[string]any)
	if !ok {
		return ""
	}
	dps, ok := issued["date-parts"].([]any)
	if !ok || len(dps) == 0 {
		return ""
	}
	first, ok := dps[0].([]any)
	if !ok || len(first) == 0 {
		return ""
	}
	parts := make([]string, 0, len(first))
	for i, p := range first {
		switch v := p.(type) {
		case float64:
			val := int(v)
			if i == 0 {
				parts = append(parts, strconv.Itoa(val))
			} else {
				parts = append(parts, fmt.Sprintf("%02d", val))
			}
		case int:
			if i == 0 {
				parts = append(parts, strconv.Itoa(v))
			} else {
				parts = append(parts, fmt.Sprintf("%02d", v))
			}
		}
	}
	return strings.Join(parts, "-")
}

// --- helpers borrowed to avoid extra deps ---

// ssrfGuard blocks loopback, RFC1918, link-local, ULA, and .onion unless CROSSREF_ALLOW_LOCAL=1
func ssrfGuard(u *url.URL) error {
	host := u.Hostname()
	if host == "" {
		return errors.New("invalid host")
	}
	if strings.HasSuffix(strings.ToLower(host), ".onion") {
		return errors.New("SSRF blocked: onion domains are not allowed")
	}
	if os.Getenv("CROSSREF_ALLOW_LOCAL") == "1" {
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

// ioReadAllLimit reads up to max bytes from r.
func ioReadAllLimit(r interface{ Read([]byte) (int, error) }, max int64) ([]byte, error) {
	const chunk = 32 * 1024
	buf := make([]byte, 0, 64*1024)
	var readTotal int64
	b := make([]byte, chunk)
	for {
		n, err := r.Read(b)
		if n > 0 {
			readTotal += int64(n)
			if readTotal > max {
				return nil, errors.New("response too large")
			}
			buf = append(buf, b[:n]...)
		}
		if err != nil {
			if errors.Is(err, ioEOF) {
				break
			}
			if strings.Contains(err.Error(), "EOF") { // fallback for stdlib EOF type
				break
			}
			return nil, err
		}
	}
	return buf, nil
}

var ioEOF = errors.New("EOF")
