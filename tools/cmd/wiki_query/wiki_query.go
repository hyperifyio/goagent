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
	"sort"
	"strings"
	"time"
)

type input struct {
	Titles   string `json:"titles,omitempty"`
	Search   string `json:"search,omitempty"`
	Language string `json:"language,omitempty"`
}

type page struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Extract string `json:"extract"`
}

type output struct {
	Pages []page `json:"pages"`
}

func main() {
	if err := run(); err != nil {
		encErr(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	in, err := readInput()
	if err != nil {
		return err
	}
	// Enforce mutual exclusivity between 'titles' and 'search'
	if (in.Titles == "" && in.Search == "") || (in.Titles != "" && in.Search != "") {
		return errors.New("provide exactly one of 'titles' or 'search'")
	}
	base := os.Getenv("MEDIAWIKI_BASE_URL")
	if base == "" {
		base = fmt.Sprintf("https://%s.wikipedia.org", langOrDefault(in.Language))
	}
	if err := ssrfGuard(base); err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	var pages []page
	if in.Titles != "" {
		pages, err = fetchExtracts(client, base, langOrDefault(in.Language), in.Titles)
	} else {
		pages, err = fetchOpenSearch(client, base, in.Search)
	}
	if err != nil {
		return err
	}
	out := output{Pages: pages}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}

func readInput() (input, error) {
	var in input
	s := bufio.NewScanner(os.Stdin)
	s.Buffer(make([]byte, 0, 64*1024), 5*1024*1024)
	var b strings.Builder
	for s.Scan() {
		b.Write(s.Bytes())
	}
	if err := s.Err(); err != nil {
		return in, err
	}
	if err := json.Unmarshal([]byte(b.String()), &in); err != nil {
		return in, err
	}
	return in, nil
}

func langOrDefault(l string) string {
	if l == "" {
		return "en"
	}
	return l
}

func ssrfGuard(base string) error {
	if os.Getenv("WIKI_QUERY_ALLOW_LOCAL") == "1" {
		return nil
	}
	u, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("MEDIAWIKI_BASE_URL invalid: %w", err)
	}
	host := u.Hostname()
	if host == "" {
		host = u.Host
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("DNS error: %w", err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return errors.New("SSRF blocked: private address")
		}
	}
	return nil
}

func fetchExtracts(c *http.Client, base, lang, titles string) ([]page, error) {
	q := url.Values{}
	q.Set("action", "query")
	q.Set("format", "json")
	q.Set("prop", "extracts")
	q.Set("exintro", "1")
	q.Set("explaintext", "1")
	q.Set("redirects", "1")
	q.Set("titles", titles)
	q.Set("uselang", lang)
	reqURL := strings.TrimRight(base, "/") + "/w/api.php?" + q.Encode()
	resp, err := c.Get(reqURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck
	var raw struct {
		Query struct {
			Pages map[string]struct {
				Title   string `json:"title"`
				Extract string `json:"extract"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	var pages []page
	for _, v := range raw.Query.Pages {
		pages = append(pages, page{
			Title:   v.Title,
			URL:     fmt.Sprintf("https://%s.wikipedia.org/wiki/%s", lang, url.PathEscape(strings.ReplaceAll(v.Title, " ", "_"))),
			Extract: v.Extract,
		})
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].Title < pages[j].Title })
	return pages, nil
}

func fetchOpenSearch(c *http.Client, base, query string) ([]page, error) {
	v := url.Values{}
	v.Set("action", "opensearch")
	v.Set("format", "json")
	v.Set("search", query)
	v.Set("limit", "10")
	reqURL := strings.TrimRight(base, "/") + "/w/api.php?" + v.Encode()
	resp, err := c.Get(reqURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck
	var arr []any
	if err := json.NewDecoder(resp.Body).Decode(&arr); err != nil {
		return nil, err
	}
	if len(arr) < 4 {
		return nil, errors.New("unexpected opensearch response")
	}
	titlesAny, snippetsAny, urlsAny := arr[1], arr[2], arr[3]
	titles, _ := toStringSlice(titlesAny)
	snippets, _ := toStringSlice(snippetsAny)
	urls, _ := toStringSlice(urlsAny)
	n := min(len(titles), min(len(snippets), len(urls)))
	pages := make([]page, 0, n)
	for i := 0; i < n; i++ {
		pages = append(pages, page{Title: titles[i], URL: urls[i], Extract: snippets[i]})
	}
	return pages, nil
}

func toStringSlice(v any) ([]string, bool) {
	i, ok := v.([]any)
	if !ok {
		return nil, false
	}
	s := make([]string, 0, len(i))
	for _, e := range i {
		if str, ok := e.(string); ok {
			s = append(s, str)
		}
	}
	return s, true
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func encErr(w *os.File, err error) {
	if e := json.NewEncoder(w).Encode(map[string]string{"error": err.Error()}); e != nil {
		// best-effort stderr encoding
		_, _ = w.Write([]byte("{\"error\":\"internal encode error\"}\n")) //nolint:errcheck
	}
}
