package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type input struct {
	URL        string `json:"url"`
	Method     string `json:"method"`
	MaxBytes   int    `json:"max_bytes"`
	TimeoutMs  int    `json:"timeout_ms"`
	Decompress *bool  `json:"decompress"`
}

type output struct {
	Status     int               `json:"status"`
	Headers    map[string]string `json:"headers"`
	BodyBase64 string            `json:"body_base64,omitempty"`
	Truncated  bool              `json:"truncated"`
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
	method, u, maxBytes, timeout, decompress, err := prepareRequestParams(in)
	if err != nil {
		return err
	}
	client := newHTTPClient(timeout, decompress)
	req, err := http.NewRequest(method, in.URL, nil)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("User-Agent", "agentcli-http-fetch/0.1")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer func() {
		_ = resp.Body.Close() //nolint:errcheck
	}()

	headers := collectHeaders(resp.Header)
	bodyB64, truncated, bodyBytes, err := maybeReadBody(method, resp.Body, maxBytes)
	if err != nil {
		return err
	}

	out := output{Status: resp.StatusCode, Headers: headers, BodyBase64: bodyB64, Truncated: truncated}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	// Best-effort audit. Failures are ignored.
	_ = appendAudit(map[string]any{ //nolint:errcheck
		"ts":        time.Now().UTC().Format(time.RFC3339Nano),
		"tool":      "http_fetch",
		"url_host":  u.Hostname(),
		"status":    resp.StatusCode,
		"bytes":     bodyBytes,
		"truncated": truncated,
		"ms":        time.Since(start).Milliseconds(),
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

func prepareRequestParams(in input) (method string, u *url.URL, maxBytes int, timeout time.Duration, decompress bool, err error) {
	if strings.TrimSpace(in.URL) == "" {
		return "", nil, 0, 0, false, errors.New("url is required")
	}
	u, err = url.Parse(in.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", nil, 0, 0, false, errors.New("only http/https are allowed")
	}
	method = strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet && method != http.MethodHead {
		return "", nil, 0, 0, false, errors.New("method must be GET or HEAD")
	}
	maxBytes = in.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 1 << 20 // default 1 MiB
	}
	timeout = resolveTimeout(in.TimeoutMs)
	// Enforce SSRF guard before any request and on every redirect target.
	if err = ssrfGuard(u); err != nil {
		return "", nil, 0, 0, false, err
	}
	decompress = true
	if in.Decompress != nil {
		decompress = *in.Decompress
	}
	return method, u, maxBytes, timeout, decompress, nil
}

func resolveTimeout(timeoutMs int) time.Duration {
	timeout := time.Duration(timeoutMs) * time.Millisecond
	if timeout > 0 {
		return timeout
	}
	if v := strings.TrimSpace(os.Getenv("HTTP_TIMEOUT_MS")); v != "" {
		if ms, perr := time.ParseDuration(v + "ms"); perr == nil {
			timeout = ms
		}
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return timeout
}

func newHTTPClient(timeout time.Duration, decompress bool) *http.Client {
	tr := &http.Transport{DisableCompression: !decompress}
	return &http.Client{Timeout: timeout, Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return ssrfGuard(req.URL)
	}}
}

func collectHeaders(h http.Header) map[string]string {
	headers := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			headers[k] = v[0]
		} else {
			headers[k] = ""
		}
	}
	return headers
}

func maybeReadBody(method string, r io.Reader, maxBytes int) (bodyB64 string, truncated bool, bodyBytes int, err error) {
	if method == http.MethodHead {
		return "", false, 0, nil
	}
	limited := io.LimitedReader{R: r, N: int64(maxBytes) + 1}
	data, rerr := io.ReadAll(&limited)
	if rerr != nil {
		return "", false, 0, fmt.Errorf("read body: %w", rerr)
	}
	if int64(len(data)) > int64(maxBytes) {
		truncated = true
		data = data[:maxBytes]
	}
	bodyBytes = len(data)
	bodyB64 = base64.StdEncoding.EncodeToString(data)
	return bodyB64, truncated, bodyBytes, nil
}

// ssrfGuard blocks requests to loopback, RFC1918, link-local, and ULA addresses,
// unless HTTP_FETCH_ALLOW_LOCAL=1 is set (only used in tests).
func ssrfGuard(u *url.URL) error {
	host := u.Hostname()
	if host == "" {
		return errors.New("invalid host")
	}
	if strings.HasSuffix(strings.ToLower(host), ".onion") {
		return errors.New("SSRF blocked: onion domains are not allowed")
	}
	if os.Getenv("HTTP_FETCH_ALLOW_LOCAL") == "1" {
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		// If DNS fails, be conservative and block
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
	// Normalize to 16-byte form for IPv4
	if v4 := ip.To4(); v4 != nil {
		ip = v4
		// 10.0.0.0/8
		if v4[0] == 10 {
			return true
		}
		// 172.16.0.0/12
		if v4[0] == 172 && v4[1]&0xf0 == 16 {
			return true
		}
		// 192.168.0.0/16
		if v4[0] == 192 && v4[1] == 168 {
			return true
		}
		// 169.254.0.0/16 link-local
		if v4[0] == 169 && v4[1] == 254 {
			return true
		}
		// 127.0.0.0/8 loopback handled by IsLoopback but keep explicit
		if v4[0] == 127 {
			return true
		}
		return false
	}
	// IPv6 ranges: ::1 (loopback), fe80::/10 (link-local), fc00::/7 (ULA)
	if ip.Equal(net.ParseIP("::1")) {
		return true
	}
	// fe80::/10
	if ip[0] == 0xfe && (ip[1]&0xc0) == 0x80 {
		return true
	}
	// fc00::/7
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
