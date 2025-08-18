package main

import (
    "bufio"
    "encoding/base64"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "os"
    "strings"
    "time"
)

type input struct {
    URL        string `json:"url"`
    Method     string `json:"method"`
    MaxBytes   int    `json:"max_bytes"`
    TimeoutMs  int    `json:"timeout_ms"`
    Decompress bool   `json:"decompress"`
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
    var in input
    dec := json.NewDecoder(bufio.NewReader(os.Stdin))
    if err := dec.Decode(&in); err != nil {
        return fmt.Errorf("parse json: %w", err)
    }
    if strings.TrimSpace(in.URL) == "" {
        return errors.New("url is required")
    }
    u, err := url.Parse(in.URL)
    if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
        return errors.New("only http/https are allowed")
    }
    method := strings.ToUpper(strings.TrimSpace(in.Method))
    if method == "" {
        method = http.MethodGet
    }
    if method != http.MethodGet && method != http.MethodHead {
        return errors.New("method must be GET or HEAD")
    }
    maxBytes := in.MaxBytes
    if maxBytes <= 0 {
        maxBytes = 1 << 20 // default 1 MiB
    }
    timeout := time.Duration(in.TimeoutMs) * time.Millisecond
    if timeout <= 0 {
        if v := strings.TrimSpace(os.Getenv("HTTP_TIMEOUT_MS")); v != "" {
            if ms, perr := time.ParseDuration(v + "ms"); perr == nil {
                timeout = ms
            }
        }
        if timeout <= 0 {
            timeout = 10 * time.Second
        }
    }

    client := &http.Client{Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
        if len(via) >= 5 {
            return errors.New("too many redirects")
        }
        return nil
    }}

    req, err := http.NewRequest(method, in.URL, nil)
    if err != nil {
        return fmt.Errorf("new request: %w", err)
    }
    req.Header.Set("User-Agent", "agentcli-http-fetch/0.1")

    resp, err := client.Do(req)
    if err != nil {
        return fmt.Errorf("http: %w", err)
    }
    defer resp.Body.Close() //nolint:errcheck

    headers := map[string]string{}
    for k, v := range resp.Header {
        if len(v) > 0 {
            headers[k] = v[0]
        } else {
            headers[k] = ""
        }
    }

    var bodyB64 string
    truncated := false
    if method != http.MethodHead {
        limited := io.LimitedReader{R: resp.Body, N: int64(maxBytes) + 1}
        data, rerr := io.ReadAll(&limited)
        if rerr != nil {
            return fmt.Errorf("read body: %w", rerr)
        }
        if int64(len(data)) > int64(maxBytes) {
            truncated = true
            data = data[:maxBytes]
        }
        bodyB64 = base64.StdEncoding.EncodeToString(data)
    }

    out := output{Status: resp.StatusCode, Headers: headers, BodyBase64: bodyB64, Truncated: truncated}
    enc := json.NewEncoder(os.Stdout)
    if err := enc.Encode(out); err != nil {
        return fmt.Errorf("encode json: %w", err)
    }
    return nil
}
