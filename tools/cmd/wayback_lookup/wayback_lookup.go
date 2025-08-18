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
	URL  string `json:"url"`
	Save bool   `json:"save"`
}

type output struct {
	ClosestURL string `json:"closest_url,omitempty"`
	Timestamp  string `json:"timestamp,omitempty"`
	Saved      bool   `json:"saved,omitempty"`
}

func main() {
	if err := run(); err != nil {
		msg := strings.ReplaceAll(err.Error(), "\n", " ")
		fmt.Fprintf(os.Stderr, "{\"error\":%q}\n", msg)
		os.Exit(1)
	}
}

func run() error {
	var in input
	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	if err := dec.Decode(&in); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}
	if strings.TrimSpace(in.URL) == "" {
		return errors.New("url is required")
	}
	base := strings.TrimSpace(os.Getenv("WAYBACK_BASE_URL"))
	if base == "" {
		return errors.New("WAYBACK_BASE_URL is required")
	}
	baseURL, err := url.Parse(base)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") {
		return errors.New("WAYBACK_BASE_URL must be a valid http/https URL")
	}
	if err := ssrfGuard(baseURL); err != nil {
		return err
	}

	// Build available URL: <base>/available?url=<in.URL>
	availURL, err := url.Parse(baseURL.String())
	if err != nil {
		return err
	}
	availURL.Path = strings.TrimRight(availURL.Path, "/") + "/available"
	q := availURL.Query()
	q.Set("url", in.URL)
	availURL.RawQuery = q.Encode()

    client := &http.Client{Timeout: 3 * time.Second}
    start := time.Now()
    resp, err := getWithRetry(client, availURL.String())
    if err != nil {
        return fmt.Errorf("http: %w", err)
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
		return fmt.Errorf("decode json: %w", err)
	}

	out := output{}
	saved := false
	if raw.ArchivedSnapshots.Closest.Available {
		out.ClosestURL = raw.ArchivedSnapshots.Closest.URL
		out.Timestamp = raw.ArchivedSnapshots.Closest.Timestamp
	} else if in.Save {
		// Trigger save
		saveURL, perr := url.Parse(baseURL.String())
		if perr != nil {
			return perr
		}
		saveURL.Path = strings.TrimRight(saveURL.Path, "/") + "/save/"
		qs := saveURL.Query()
		qs.Set("url", in.URL)
		saveURL.RawQuery = qs.Encode()
		// Re-guard
		if err := ssrfGuard(saveURL); err != nil {
			return err
		}
        resp2, herr := getWithRetry(client, saveURL.String())
		if herr == nil {
			if resp2.StatusCode >= 200 && resp2.StatusCode < 300 {
				saved = true
				out.Saved = true
			}
			_ = resp2.Body.Close() //nolint:errcheck
		}
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		return fmt.Errorf("encode json: %w", err)
	}
	_ = appendAudit(map[string]any{ //nolint:errcheck
		"ts":    time.Now().UTC().Format(time.RFC3339Nano),
		"tool":  "wayback_lookup",
		"ms":    time.Since(start).Milliseconds(),
		"saved": saved,
	})
	return nil
}

// ssrfGuard blocks loopback, RFC1918, link-local, and ULA unless WAYBACK_ALLOW_LOCAL=1
func ssrfGuard(u *url.URL) error {
	host := u.Hostname()
	if host == "" {
		return errors.New("invalid host")
	}
	if strings.HasSuffix(strings.ToLower(host), ".onion") {
		return errors.New("SSRF blocked: onion domains are not allowed")
	}
	if os.Getenv("WAYBACK_ALLOW_LOCAL") == "1" {
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

// getWithRetry performs a GET with one retry on 5xx using a small backoff.
func getWithRetry(client *http.Client, url string) (*http.Response, error) {
    resp, err := client.Get(url)
    if err != nil {
        return nil, err
    }
    if resp.StatusCode >= 500 {
        _ = resp.Body.Close() //nolint:errcheck
        time.Sleep(150 * time.Millisecond)
        return client.Get(url)
    }
    return resp, nil
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
