package oai

import (
    "bytes"
    "context"
    "crypto/rand"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net"
    "net/http"
    "encoding/hex"
    "strings"
    "time"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
    retry      RetryPolicy
}

// RetryPolicy controls HTTP retry behavior for transient failures.
// MaxRetries specifies the number of retries after the initial attempt.
// Backoff specifies the base delay between attempts; exponential backoff is applied.
// Jitter is not applied in this minimal implementation to keep tests deterministic.
type RetryPolicy struct {
    MaxRetries int
    Backoff    time.Duration
}

// NewClient creates a client without retries (single attempt only).
func NewClient(baseURL, apiKey string, timeout time.Duration) *Client {
	trimmed := strings.TrimRight(baseURL, "/")
	return &Client{
		baseURL: trimmed,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
        retry: RetryPolicy{MaxRetries: 0, Backoff: 0},
	}
}

// NewClientWithRetry creates a client with a retry policy for transient failures.
func NewClientWithRetry(baseURL, apiKey string, timeout time.Duration, retry RetryPolicy) *Client {
    if retry.MaxRetries < 0 {
        retry.MaxRetries = 0
    }
    trimmed := strings.TrimRight(baseURL, "/")
    return &Client{
        baseURL: trimmed,
        apiKey:  apiKey,
        httpClient: &http.Client{
            Timeout: timeout,
        },
        retry: retry,
    }
}

func (c *Client) CreateChatCompletion(ctx context.Context, req ChatCompletionsRequest) (ChatCompletionsResponse, error) {
	var zero ChatCompletionsResponse
	body, err := json.Marshal(req)
	if err != nil {
		return zero, fmt.Errorf("marshal request: %w", err)
	}
	endpoint := c.baseURL + "/chat/completions"
    // Attempt loop with basic exponential backoff on transient failures.
    attempts := c.retry.MaxRetries + 1
    if attempts < 1 {
        attempts = 1
    }

    var lastErr error
    // Generate a stable Idempotency-Key used across all attempts
    idemKey := generateIdempotencyKey()
    for attempt := 0; attempt < attempts; attempt++ {
        httpReq, nerr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
        if nerr != nil {
            return zero, fmt.Errorf("new request: %w", nerr)
        }
        httpReq.Header.Set("Content-Type", "application/json")
        if c.apiKey != "" {
            httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
        }
        httpReq.Header.Set("Idempotency-Key", idemKey)

        resp, derr := c.httpClient.Do(httpReq)
        if derr != nil {
            lastErr = derr
            if attempt < attempts-1 && isRetryableError(derr) {
                sleepBackoff(c.retry.Backoff, attempt)
                continue
            }
            return zero, fmt.Errorf("http do: %w", derr)
        }

        respBody, readErr := io.ReadAll(resp.Body)
        _ = resp.Body.Close() // best-effort close
        if readErr != nil {
            lastErr = readErr
            if attempt < attempts-1 && isRetryableError(readErr) {
                sleepBackoff(c.retry.Backoff, attempt)
                continue
            }
            return zero, fmt.Errorf("read response body: %w", readErr)
        }
        if resp.StatusCode < 200 || resp.StatusCode >= 300 {
            // Retry on 429 and 5xx; otherwise return immediately
            if attempt < attempts-1 && (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) {
                // Respect Retry-After when present; otherwise use exponential backoff
                if ra, ok := retryAfterDuration(resp.Header.Get("Retry-After"), time.Now()); ok {
                    sleepFor(ra)
                } else {
                    sleepBackoff(c.retry.Backoff, attempt)
                }
                continue
            }
            return zero, fmt.Errorf("chat API %s: %d: %s", endpoint, resp.StatusCode, truncate(string(respBody), 2000))
        }
        if err := json.Unmarshal(respBody, &zero); err != nil {
            return ChatCompletionsResponse{}, fmt.Errorf("decode response: %w; body: %s", err, truncate(string(respBody), 1000))
        }
        return zero, nil
    }
    if lastErr != nil {
        return zero, lastErr
    }
    return zero, fmt.Errorf("chat request failed without a specific error")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// isRetryableError returns true for transient network/timeouts.
func isRetryableError(err error) bool {
    if err == nil {
        return false
    }
    // Context deadline exceeded from client timeout
    if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
        return true
    }
    var ne net.Error
    if errors.As(err, &ne) {
        if ne.Timeout() || ne.Temporary() {
            return true
        }
    }
    // *url.Error often wraps retryable errors; fall back to string contains of "timeout"
    s := strings.ToLower(err.Error())
    if strings.Contains(s, "timeout") {
        return true
    }
    return false
}

func sleepBackoff(base time.Duration, attempt int) {
    if base <= 0 {
        base = 200 * time.Millisecond
    }
    // exponential backoff: base * 2^attempt, capped to 2s to keep tests fast
    d := base << attempt
    if d > 2*time.Second {
        d = 2 * time.Second
    }
    time.Sleep(d)
}

// retryAfterDuration parses the Retry-After header which may be seconds or HTTP-date.
// Returns (duration, true) when valid; otherwise (0, false).
func retryAfterDuration(h string, now time.Time) (time.Duration, bool) {
    h = strings.TrimSpace(h)
    if h == "" {
        return 0, false
    }
    // Try integer seconds first
    if secs, err := time.ParseDuration(h + "s"); err == nil {
        if secs > 0 {
            return secs, true
        }
    }
    // Try HTTP-date formats per RFC 7231 (use http.TimeFormat)
    if t, err := time.Parse(http.TimeFormat, h); err == nil {
        if t.After(now) {
            return t.Sub(now), true
        }
    }
    return 0, false
}

// sleepFor sleeps for the provided duration; extracted for testability.
func sleepFor(d time.Duration) {
    if d <= 0 {
        return
    }
    time.Sleep(d)
}

// generateIdempotencyKey returns a random hex string suitable for Idempotency-Key.
func generateIdempotencyKey() string {
    var b [16]byte
    if _, err := rand.Read(b[:]); err != nil {
        // Fallback to timestamp-based key if crypto/rand fails; extremely unlikely
        return fmt.Sprintf("goagent-%d", time.Now().UnixNano())
    }
    return "goagent-" + hex.EncodeToString(b[:])
}
