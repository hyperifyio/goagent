package oai

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "net/http/httptrace"
    "strings"
    "time"
)

type Client struct {
    baseURL    string
    apiKey     string
    httpClient *http.Client
    retry      RetryPolicy
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

// nolint:gocyclo // Orchestrates retries and timing; complexity acceptable and tested.
func (c *Client) CreateChatCompletion(ctx context.Context, req ChatCompletionsRequest) (ChatCompletionsResponse, error) {
	// Encoder guard: omit temperature entirely for models that do not support it.
	// This complements higher-level callers which may or may not set the field.
	if !SupportsTemperature(req.Model) {
		req.Temperature = nil
	}
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
	// Allow a single parameter-recovery retry without consuming the normal retry budget
	recoveryGranted := false
	// Emit a meta audit entry capturing observability fields derived from the request
	emitChatMetaAudit(req)
	// Generate a stable Idempotency-Key used across all attempts
	idemKey := generateIdempotencyKey()
	// Capture any stage label from context for audit enrichment
	stage := auditStageFromContext(ctx)
	for attempt := 0; attempt < attempts; attempt++ {
		// Per-attempt timing capture using httptrace
		attemptStart := time.Now()
		var (
			dnsStart, connStart  time.Time
			dnsDur, connDur      time.Duration
			wroteAt, firstByteAt time.Time
		)
		trace := &httptrace.ClientTrace{
			DNSStart: func(info httptrace.DNSStartInfo) { dnsStart = time.Now() },
			DNSDone: func(info httptrace.DNSDoneInfo) {
				if !dnsStart.IsZero() {
					dnsDur += time.Since(dnsStart)
				}
			},
			ConnectStart: func(network, addr string) { connStart = time.Now() },
			ConnectDone: func(network, addr string, err error) {
				if !connStart.IsZero() {
					connDur += time.Since(connStart)
				}
			},
			GotConn:              func(info httptrace.GotConnInfo) {},
			WroteRequest:         func(info httptrace.WroteRequestInfo) { wroteAt = time.Now() },
			GotFirstResponseByte: func() { firstByteAt = time.Now() },
		}
		// Fallback for TLS duration using httptrace hooks available: emulate by measuring from TLSHandshakeStart/Done via GotConn workaround.
		// Since httptrace.TLSHandshakeDone requires crypto/tls type, replicate using any to avoid import on older Go.
		// Note: we will compute tlsDur as zero unless supported; acceptable for audit purposes.

		httpReq, nerr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if nerr != nil {
			return zero, fmt.Errorf("new request: %w", nerr)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if c.apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		httpReq.Header.Set("Idempotency-Key", idemKey)
		httpReq = httpReq.WithContext(httptrace.WithClientTrace(httpReq.Context(), trace))

		resp, derr := c.httpClient.Do(httpReq)
		if derr != nil {
			lastErr = derr
			// Log attempt with error
			logHTTPAttempt(stage, idemKey, attempt+1, attempts, 0, 0, endpoint, derr.Error())
			// Emit timing audit for error case
			logHTTPTiming(stage, idemKey, attempt+1, endpoint, 0, attemptStart, dnsDur, connDur, 0, wroteAt, firstByteAt, time.Now(), classifyHTTPCause(ctx, derr), userHintForCause(ctx, derr))
			if attempt < attempts-1 && isRetryableError(derr) {
				// compute backoff (with jitter) for audit then sleep
				back := backoffWithJitter(c.retry.Backoff, attempt, c.retry.JitterFraction, c.retry.Rand)
				logHTTPAttempt(stage, idemKey, attempt+1, attempts, 0, back.Milliseconds(), endpoint, derr.Error())
				sleepFunc(back)
				continue
			}
			// Upgrade error with base URL, configured timeout, and actionable hint
			hint := userHintForCause(ctx, derr)
			// c.httpClient.Timeout reflects configured HTTP timeout
			tmo := c.httpClient.Timeout
			if hint != "" {
				return zero, fmt.Errorf("chat POST failed: %v (base=%s, http-timeout=%s). Hint: %s", derr, c.baseURL, tmo, hint)
			}
			return zero, fmt.Errorf("chat POST failed: %v (base=%s, http-timeout=%s)", derr, c.baseURL, tmo)
		}

		// When streaming is requested, the server should respond with SSE. We do not
		// support streaming in this method. Return 400 guidance to call StreamChat.
		if req.Stream {
			// Streaming is not supported in this method; close body and return guidance.
			_ = resp.Body.Close() //nolint:errcheck // best-effort close
			return zero, fmt.Errorf("stream=true not supported in CreateChatCompletion; use StreamChat")
		}
		respBody, readErr := io.ReadAll(resp.Body)
		if cerr := resp.Body.Close(); cerr != nil {
			// best-effort: record close error as lastErr if none
			if lastErr == nil {
				lastErr = cerr
			}
		}
		if readErr != nil {
			lastErr = readErr
			// Log attempt with read error
			logHTTPAttempt(stage, idemKey, attempt+1, attempts, resp.StatusCode, 0, endpoint, readErr.Error())
			// Emit timing audit including read duration up to error
			logHTTPTiming(stage, idemKey, attempt+1, endpoint, resp.StatusCode, attemptStart, dnsDur, connDur, 0, wroteAt, firstByteAt, time.Now(), classifyHTTPCause(ctx, readErr), userHintForCause(ctx, readErr))
			if attempt < attempts-1 && isRetryableError(readErr) {
				back := backoffWithJitter(c.retry.Backoff, attempt, c.retry.JitterFraction, c.retry.Rand)
				logHTTPAttempt(stage, idemKey, attempt+1, attempts, resp.StatusCode, back.Milliseconds(), endpoint, readErr.Error())
				sleepFunc(back)
				continue
			}
			return zero, fmt.Errorf("read response body: %w", readErr)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			// Parameter-recovery: if 400 mentions invalid/unsupported temperature and
			// the request included temperature, remove it and retry once immediately.
			if resp.StatusCode == http.StatusBadRequest {
				// Capture body string for inspection and logs
				bodyStr := string(respBody)
				if !recoveryGranted && includesTemperature(req) && mentionsUnsupportedTemperature(bodyStr) {
					// Log recovery attempt with a structured audit entry
					logHTTPAttempt(stage, idemKey, attempt+1, attempts, resp.StatusCode, 0, endpoint, "param_recovery: temperature")
					// Clear temperature and re-marshal request for a one-time recovery retry
					req.Temperature = nil
					nb, merr := json.Marshal(req)
					if merr == nil {
						body = nb
						// Grant exactly one extra attempt for recovery
						recoveryGranted = true
						attempts++
						// Emit timing audit for the failed attempt before retrying
						logHTTPTiming(stage, idemKey, attempt+1, endpoint, resp.StatusCode, attemptStart, dnsDur, connDur, 0, wroteAt, firstByteAt, time.Now(), "http_status", "param_recovery_temperature")
						// Perform immediate recovery retry without consuming a normal retry slot
						continue
					}
					// If marshal fails, fall through to normal error handling
				}
			}
			// Retry on 429 and 5xx; otherwise return immediately
			if attempt < attempts-1 && (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) {
				// Respect Retry-After when present; otherwise use exponential backoff
				if ra, ok := retryAfterDuration(resp.Header.Get("Retry-After"), time.Now()); ok {
					// Log with Retry-After derived backoff
					logHTTPAttempt(stage, idemKey, attempt+1, attempts, resp.StatusCode, ra.Milliseconds(), endpoint, "")
					sleepFunc(ra)
				} else {
					back := backoffWithJitter(c.retry.Backoff, attempt, c.retry.JitterFraction, c.retry.Rand)
					logHTTPAttempt(stage, idemKey, attempt+1, attempts, resp.StatusCode, back.Milliseconds(), endpoint, "")
					sleepFunc(back)
				}
				// Emit timing audit for non-2xx attempt
				logHTTPTiming(stage, idemKey, attempt+1, endpoint, resp.StatusCode, attemptStart, dnsDur, connDur, 0, wroteAt, firstByteAt, time.Now(), "http_status", "")
				continue
			}
			// Final non-retryable failure: log attempt (no backoff) and return
			logHTTPAttempt(stage, idemKey, attempt+1, attempts, resp.StatusCode, 0, endpoint, truncate(string(respBody), 2000))
			logHTTPTiming(stage, idemKey, attempt+1, endpoint, resp.StatusCode, attemptStart, dnsDur, connDur, 0, wroteAt, firstByteAt, time.Now(), "http_status", "")
			return zero, fmt.Errorf("chat API %s: %d: %s", endpoint, resp.StatusCode, truncate(string(respBody), 2000))
		}
		if err := json.Unmarshal(respBody, &zero); err != nil {
			return ChatCompletionsResponse{}, fmt.Errorf("decode response: %w; body: %s", err, truncate(string(respBody), 1000))
		}
		// Success: log attempt with status and no backoff
		logHTTPAttempt(stage, idemKey, attempt+1, attempts, resp.StatusCode, 0, endpoint, "")
		logHTTPTiming(stage, idemKey, attempt+1, endpoint, resp.StatusCode, attemptStart, dnsDur, connDur, 0, wroteAt, firstByteAt, time.Now(), "success", "")
		return zero, nil
	}
	if lastErr != nil {
		return zero, lastErr
	}
	return zero, fmt.Errorf("chat request failed without a specific error")
}

// StreamChat performs a streaming chat completion request (SSE) and delivers
// parsed chunks to the provided callback as they arrive. The callback should be
// fast and non-blocking. The function returns when the stream completes or an
// error occurs. Retries are not applied in streaming mode.
func (c *Client) StreamChat(ctx context.Context, req ChatCompletionsRequest, onChunk func(StreamChunk) error) error {
	// Encoder guard: omit temperature when unsupported
	if !SupportsTemperature(req.Model) {
		req.Temperature = nil
	}
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	endpoint := c.baseURL + "/chat/completions"
	httpReq, nerr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if nerr != nil {
		return fmt.Errorf("new request: %w", nerr)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	// Idempotency not relevant for streaming; still set for consistency
	httpReq.Header.Set("Idempotency-Key", generateIdempotencyKey())

	resp, derr := c.httpClient.Do(httpReq)
	if derr != nil {
		return derr
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck // best-effort close
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, rerr := io.ReadAll(resp.Body)
		if rerr != nil {
			return fmt.Errorf("chat API %s: %d: <read error>", endpoint, resp.StatusCode)
		}
		return fmt.Errorf("chat API %s: %d: %s", endpoint, resp.StatusCode, truncate(string(b), 2000))
	}
	// Require SSE content type for streaming
	ct := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if !strings.Contains(ct, "text/event-stream") {
		// Not a streaming response; signal caller to fallback
		_, _ = io.ReadAll(resp.Body) //nolint:errcheck // ignore read error; fallback remains informative
		return fmt.Errorf("server does not support streaming (content-type=%q)", ct)
	}
	// Simple SSE parser: read lines; handle "data: ..." and [DONE]
	dec := newLineReader(resp.Body)
	for {
		line, err := dec()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("stream read: %w", err)
		}
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(s, "data:"))
			if payload == "[DONE]" {
				return nil
			}
			var chunk StreamChunk
			if jerr := json.Unmarshal([]byte(payload), &chunk); jerr != nil {
				// Skip malformed chunk
				continue
			}
			if onChunk != nil {
				if err := onChunk(chunk); err != nil {
					return err
				}
			}
		}
	}
}

