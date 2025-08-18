package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type input struct {
	URL       string `json:"url"`
	UserAgent string `json:"user_agent,omitempty"`
}

type output struct {
	Allowed      bool     `json:"allowed"`
	CrawlDelayMS int      `json:"crawl_delay_ms,omitempty"`
	GroupRules   []string `json:"group_rules"`
}

type robotsGroup struct {
	userAgents    []string
	lines         []string
	allowPaths    []string
	disallowPaths []string
	crawlDelayMS  int
}

func main() {
	var in input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		errorJSON(fmt.Errorf("invalid input: %w", err))
		os.Exit(2)
	}
	if in.URL == "" {
		errorJSON(errors.New("missing url"))
		os.Exit(2)
	}
	if in.UserAgent == "" {
		in.UserAgent = "agentcli"
	}

	u, err := url.Parse(in.URL)
	if err != nil {
		errorJSON(fmt.Errorf("bad url: %v", err))
		os.Exit(2)
	}
	if blocked, reason := ssrfBlocked(u.Host); blocked {
		errorJSON(fmt.Errorf("SSRF blocked: %s", reason))
		os.Exit(1)
	}

	resp, err := fetchRobots(u)
	if err != nil {
		errorJSON(err)
		os.Exit(1)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			// surface close error
			_, _ = os.Stderr.WriteString(fmt.Sprintf("{\"error\":\"close body: %v\"}", cerr)) //nolint:errcheck
		}
	}()

	groups, err := parseRobots(resp.Body)
	if err != nil {
		errorJSON(err)
		os.Exit(1)
	}
	grp, ok := matchGroup(groups, in.UserAgent)
	if !ok {
		emitOrExit(output{Allowed: true})
		return
	}
	allowed := evaluateAllowed(grp, u.EscapedPath())
	out := output{Allowed: allowed, GroupRules: append([]string(nil), grp.lines...)}
	if grp.crawlDelayMS > 0 {
		out.CrawlDelayMS = grp.crawlDelayMS
	}
	emitOrExit(out)
}

func fetchRobots(u *url.URL) (*http.Response, error) {
	origin := (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 0 {
			if req.URL.Host != u.Host || req.URL.Scheme != u.Scheme {
				return http.ErrUseLastResponse
			}
		}
		return nil
	}}
	req, err := http.NewRequest(http.MethodGet, origin+"/robots.txt", nil)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	req.Header.Set("User-Agent", "agentcli-robots-check/0.1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch robots.txt: %w", err)
	}
	if resp.StatusCode >= 400 {
		// ensure body drained before close
		if _, cerr := io.Copy(io.Discard, resp.Body); cerr != nil {
			_, _ = os.Stderr.WriteString("{\"error\":\"drain body\"}") //nolint:errcheck
		}
		if cerr := resp.Body.Close(); cerr != nil {
			_, _ = os.Stderr.WriteString("{\"error\":\"close body\"}") //nolint:errcheck
		}
		return nil, fmt.Errorf("robots.txt status %d", resp.StatusCode)
	}
	return resp, nil
}

func parseRobots(r io.Reader) ([]robotsGroup, error) {
	scanner := bufio.NewScanner(r)
	var groups []robotsGroup
	cur := robotsGroup{}
	flush := func() {
		if len(cur.userAgents) > 0 {
			groups = append(groups, cur)
		}
		cur = robotsGroup{}
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "user-agent:"):
			val := strings.TrimSpace(line[len("User-agent:"):])
			if len(cur.userAgents) > 0 && (len(cur.allowPaths) > 0 || len(cur.disallowPaths) > 0 || cur.crawlDelayMS > 0) {
				flush()
			}
			cur.userAgents = append(cur.userAgents, strings.ToLower(val))
			cur.lines = append(cur.lines, line)
		case strings.HasPrefix(lower, "allow:"):
			val := strings.TrimSpace(line[len("Allow:"):])
			cur.allowPaths = append(cur.allowPaths, val)
			cur.lines = append(cur.lines, line)
		case strings.HasPrefix(lower, "disallow:"):
			val := strings.TrimSpace(line[len("Disallow:"):])
			cur.disallowPaths = append(cur.disallowPaths, val)
			cur.lines = append(cur.lines, line)
		case strings.HasPrefix(lower, "crawl-delay:"):
			val := strings.TrimSpace(line[len("Crawl-delay:"):])
			if d, err := time.ParseDuration(val + "s"); err == nil {
				cur.crawlDelayMS = int(d / time.Millisecond)
			}
			cur.lines = append(cur.lines, line)
		default:
			// ignore others
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read robots.txt: %w", err)
	}
	return groups, nil
}

func matchGroup(groups []robotsGroup, userAgent string) (robotsGroup, bool) {
	uaLower := strings.ToLower(userAgent)
	for _, g := range groups {
		for _, ua := range g.userAgents {
			if ua == uaLower {
				return g, true
			}
		}
	}
	for _, g := range groups {
		for _, ua := range g.userAgents {
			if ua == "*" {
				return g, true
			}
		}
	}
	return robotsGroup{}, false
}

func evaluateAllowed(g robotsGroup, path string) bool {
	allowed := true
	for _, d := range g.disallowPaths {
		if d == "" {
			continue
		}
		if strings.HasPrefix(path, d) {
			allowed = false
		}
	}
	for _, a := range g.allowPaths {
		if a == "" {
			continue
		}
		if strings.HasPrefix(path, a) {
			allowed = true
		}
	}
	return allowed
}

func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func emitOrExit(v any) {
	if err := emitJSON(v); err != nil {
		// best-effort stderr message; ignore write error explicitly
		_, _ = os.Stderr.WriteString(fmt.Sprintf("{\"error\":\"encode output: %v\"}", err)) //nolint:errcheck
		os.Exit(1)
	}
}

func errorJSON(err error) {
	b, jerr := json.Marshal(map[string]string{"error": err.Error()})
	if jerr != nil {
		_, _ = os.Stderr.WriteString("{\"error\":\"internal error\"}") //nolint:errcheck
		return
	}
	_, _ = os.Stderr.Write(b) //nolint:errcheck
}

func ssrfBlocked(hostport string) (bool, string) {
	// Allow override for tests
	if os.Getenv("ROBOTS_CHECK_ALLOW_LOCAL") == "1" {
		return false, "allow local for tests"
	}
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	ip := net.ParseIP(host)
	if ip != nil {
		if ip.IsLoopback() || ip.IsUnspecified() {
			return true, "loopback"
		}
		if isPrivateIP(ip) {
			return true, "private address"
		}
		return false, ""
	}
	addrs, err := net.LookupIP(host)
	if err != nil {
		return true, "dns lookup failed"
	}
	for _, a := range addrs {
		if a.IsLoopback() || a.IsUnspecified() || isPrivateIP(a) {
			return true, "resolved to private/loopback"
		}
	}
	return false, ""
}

var privateBlocks []*net.IPNet

func init() {
	cidrs := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	}
	for _, cidr := range cidrs {
		_, block, err := net.ParseCIDR(cidr)
		if err != nil {
			panic("invalid CIDR literal: " + cidr)
		}
		privateBlocks = append(privateBlocks, block)
	}
}

func isPrivateIP(ip net.IP) bool {
	for _, block := range privateBlocks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}
