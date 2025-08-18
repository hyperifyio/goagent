package main

import (
    "bufio"
    "encoding/json"
    "encoding/xml"
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
    URL             string `json:"url"`
    IfModifiedSince string `json:"if_modified_since"`
}

type output struct {
    Feed  map[string]string `json:"feed"`
    Items []map[string]any  `json:"items"`
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
    if err := json.NewDecoder(bufio.NewReader(os.Stdin)).Decode(&in); err != nil {
        return fmt.Errorf("parse json: %w", err)
    }
    if strings.TrimSpace(in.URL) == "" {
        return errors.New("url is required")
    }
    u, err := url.Parse(in.URL)
    if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
        return errors.New("only http/https are allowed")
    }
    if err := ssrfGuard(u); err != nil {
        return err
    }
    client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
        if len(via) >= 5 {
            return errors.New("too many redirects")
        }
        return ssrfGuard(req.URL)
    }}
    req, err := http.NewRequest(http.MethodGet, in.URL, nil)
    if err != nil {
        return fmt.Errorf("new request: %w", err)
    }
    req.Header.Set("User-Agent", "agentcli-rss-fetch/0.1")
    if strings.TrimSpace(in.IfModifiedSince) != "" {
        req.Header.Set("If-Modified-Since", in.IfModifiedSince)
    }
    resp, err := client.Do(req)
    if err != nil {
        return fmt.Errorf("http: %w", err)
    }
    defer func() { _ = resp.Body.Close() }()
    if resp.StatusCode == http.StatusNotModified {
        // Emit empty items on 304 (caller may infer not modified)
        return emit(output{Feed: map[string]string{"title": "", "link": ""}, Items: []map[string]any{}})
    }
    if resp.StatusCode >= 400 {
        // Drain then error
        _, _ = io.Copy(io.Discard, resp.Body)
        return fmt.Errorf("status %d", resp.StatusCode)
    }
    data, err := io.ReadAll(resp.Body)
    if err != nil {
        return fmt.Errorf("read body: %w", err)
    }
    // Try RSS first
    if out, ok := parseRSS(data); ok {
        return emit(out)
    }
    if out, ok := parseAtom(data); ok {
        return emit(out)
    }
    return errors.New("unsupported feed format")
}

func emit(v any) error {
    enc := json.NewEncoder(os.Stdout)
    enc.SetEscapeHTML(false)
    return enc.Encode(v)
}

// Minimal RSS 2.0 representation
type rssDoc struct {
    XMLName xml.Name   `xml:"rss"`
    Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
    Title string    `xml:"title"`
    Link  string    `xml:"link"`
    Items []rssItem `xml:"item"`
}

type rssItem struct {
    Title   string `xml:"title"`
    Link    string `xml:"link"`
    PubDate string `xml:"pubDate"`
    Desc    string `xml:"description"`
}

func parseRSS(data []byte) (output, bool) {
    var doc rssDoc
    if err := xml.Unmarshal(data, &doc); err != nil || doc.XMLName.Local != "rss" {
        return output{}, false
    }
    out := output{Feed: map[string]string{"title": doc.Channel.Title, "link": doc.Channel.Link}, Items: make([]map[string]any, 0, len(doc.Channel.Items))}
    for _, it := range doc.Channel.Items {
        item := map[string]any{"title": it.Title, "url": it.Link}
        if it.PubDate != "" {
            item["published_at"] = it.PubDate
        }
        if it.Desc != "" {
            item["summary"] = it.Desc
        }
        out.Items = append(out.Items, item)
    }
    return out, true
}

// Minimal Atom 1.0 representation
type atomLink struct{ Href string `xml:"href,attr"` }
type atomDoc struct {
    XMLName xml.Name `xml:"http://www.w3.org/2005/Atom feed"`
    Title   string   `xml:"title"`
    Link    atomLink `xml:"link"`
    Entries []struct {
        Title   string   `xml:"title"`
        Link    atomLink `xml:"link"`
        Updated string   `xml:"updated"`
        Published string `xml:"published"`
        Summary string   `xml:"summary"`
    } `xml:"entry"`
}

func parseAtom(data []byte) (output, bool) {
    var doc atomDoc
    if err := xml.Unmarshal(data, &doc); err != nil || doc.XMLName.Local != "feed" {
        return output{}, false
    }
    link := doc.Link.Href
    out := output{Feed: map[string]string{"title": doc.Title, "link": link}}
    for _, e := range doc.Entries {
        item := map[string]any{"title": e.Title, "url": e.Link.Href}
        if e.Updated != "" {
            item["published_at"] = e.Updated
        } else if e.Published != "" {
            item["published_at"] = e.Published
        }
        if e.Summary != "" {
            item["summary"] = e.Summary
        }
        out.Items = append(out.Items, item)
    }
    return out, true
}

func ssrfGuard(u *url.URL) error {
    host := u.Hostname()
    if host == "" {
        return errors.New("invalid host")
    }
    if strings.HasSuffix(strings.ToLower(host), ".onion") {
        return errors.New("SSRF blocked: onion domains are not allowed")
    }
    if os.Getenv("RSS_FETCH_ALLOW_LOCAL") == "1" {
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
        if v4[0] == 10 || (v4[0] == 172 && v4[1]&0xf0 == 16) || (v4[0] == 192 && v4[1] == 168) || (v4[0] == 169 && v4[1] == 254) || v4[0] == 127 {
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
