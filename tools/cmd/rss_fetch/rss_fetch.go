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
    // Stub implementation to make tests compile; will implement in next step.
    enc := json.NewEncoder(os.Stdout)
    enc.SetEscapeHTML(false)
    return enc.Encode(output{Feed: map[string]string{"title": ""}, Items: []map[string]any{}})
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
