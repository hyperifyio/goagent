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
	origin := (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()

	if blocked, reason := ssrfBlocked(u.Host); blocked {
		errorJSON(fmt.Errorf("SSRF blocked: %s", reason))
		os.Exit(1)
	}

	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		// Do not follow off-origin redirects
		if len(via) > 0 {
			if req.URL.Host != u.Host || req.URL.Scheme != u.Scheme {
				return http.ErrUseLastResponse
			}
		}
		return nil
	}}
	req, err := http.NewRequest(http.MethodGet, origin+"/robots.txt", nil)
	if err != nil {
		errorJSON(fmt.Errorf("request: %v", err))
		os.Exit(1)
	}
	// Identify our UA but evaluation uses input.UserAgent
	req.Header.Set("User-Agent", "agentcli-robots-check/0.1")
	resp, err := client.Do(req)
	if err != nil {
		errorJSON(fmt.Errorf("fetch robots.txt: %v", err))
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		errorJSON(fmt.Errorf("robots.txt status %d", resp.StatusCode))
		os.Exit(1)
	}

	scanner := bufio.NewScanner(resp.Body)
	type group struct{
		ua []string
		lines []string
		allow []string
		disallow []string
		crawlDelayMS int
	}
	var groups []group
	cur := group{}
	flush := func(){
		if len(cur.ua) > 0 {
			groups = append(groups, cur)
		}
		cur = group{}
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if i := strings.IndexByte(line, '#'); i >= 0 { line = strings.TrimSpace(line[:i]) }
		if line == "" { continue }
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "user-agent:"):
			val := strings.TrimSpace(line[len("User-agent:"):])
			if len(cur.ua) > 0 && (len(cur.allow)>0 || len(cur.disallow)>0 || cur.crawlDelayMS>0) {
				flush()
			}
			cur.ua = append(cur.ua, strings.ToLower(val))
			cur.lines = append(cur.lines, line)
		case strings.HasPrefix(lower, "allow:"):
			val := strings.TrimSpace(line[len("Allow:"):])
			cur.allow = append(cur.allow, val)
			cur.lines = append(cur.lines, line)
		case strings.HasPrefix(lower, "disallow:"):
			val := strings.TrimSpace(line[len("Disallow:"):])
			cur.disallow = append(cur.disallow, val)
			cur.lines = append(cur.lines, line)
		case strings.HasPrefix(lower, "crawl-delay:"):
			val := strings.TrimSpace(line[len("Crawl-delay:"):])
			if d, err := time.ParseDuration(val+"s"); err == nil {
				cur.crawlDelayMS = int(d / time.Millisecond)
			}
			cur.lines = append(cur.lines, line)
		default:
			// ignore other directives for now
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		errorJSON(fmt.Errorf("read robots.txt: %v", err))
		os.Exit(1)
	}

	matchIdx := -1
	uaLower := strings.ToLower(in.UserAgent)
	for i, g := range groups {
		for _, ua := range g.ua {
			if ua == uaLower { matchIdx = i; break }
		}
		if matchIdx >= 0 { break }
	}
	if matchIdx < 0 {
		for i, g := range groups {
			for _, ua := range g.ua {
				if ua == "*" { matchIdx = i; break }
			}
			if matchIdx >= 0 { break }
		}
	}

	allowed := true
	var rules []string
	if matchIdx >= 0 {
		g := groups[matchIdx]
		rules = append(rules, g.lines...)
		path := u.EscapedPath()
		// Simple prefix matching per groups
		for _, d := range g.disallow {
			if d == "" { continue }
			if strings.HasPrefix(path, d) { allowed = false }
		}
		for _, a := range g.allow {
			if a == "" { continue }
			if strings.HasPrefix(path, a) { allowed = true }
		}
		out := output{Allowed: allowed, GroupRules: rules}
		if g.crawlDelayMS > 0 { out.CrawlDelayMS = g.crawlDelayMS }
		emitJSON(out)
		return
	}
	// No robots groups -> allowed by default
	emitJSON(output{Allowed: true, GroupRules: nil})
}

func emitJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func errorJSON(err error) {
	m := map[string]string{"error": err.Error()}
	b, _ := json.Marshal(m)
	_, _ = os.Stderr.Write(b)
}

func ssrfBlocked(hostport string) (bool, string) {
	// Allow override for tests
	if os.Getenv("ROBOTS_CHECK_ALLOW_LOCAL") == "1" {
		return false, "allow local for tests"
	}
	host, _, err := net.SplitHostPort(hostport)
	if err != nil { host = hostport }
	ip := net.ParseIP(host)
	if ip != nil {
		if ip.IsLoopback() || ip.IsUnspecified() { return true, "loopback" }
		if isPrivateIP(ip) { return true, "private address" }
		return false, ""
	}
	addrs, err := net.LookupIP(host)
	if err != nil { return true, "dns lookup failed" }
	for _, a := range addrs {
		if a.IsLoopback() || a.IsUnspecified() || isPrivateIP(a) { return true, "resolved to private/loopback" }
	}
	return false, ""
}

func isPrivateIP(ip net.IP) bool {
	privateBlocks := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	}
	for _, cidr := range privateBlocks {
		_, block, _ := net.ParseCIDR(cidr)
		if block.Contains(ip) { return true }
	}
	return false
}
