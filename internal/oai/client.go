package oai

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand"
	"net"
	"net/http"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	retry      RetryPolicy
}

// audit context keys are unexported to avoid collisions. Use helper to set.
type auditCtxKey string

const (
	auditCtxKeyStage auditCtxKey = "audit_stage"
)

// WithAuditStage returns a child context that carries an audit stage label
// (e.g., "prep") that will be included in HTTP audit entries.
func WithAuditStage(parent context.Context, stage string) context.Context {
	stage = strings.TrimSpace(stage)
	if stage == "" {
		return parent
	}
	return context.WithValue(parent, auditCtxKeyStage, stage)
}

func auditStageFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v := ctx.Value(auditCtxKeyStage); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// RetryPolicy controls HTTP retry behavior for transient failures.
// MaxRetries specifies the number of retries after the initial attempt.
// Backoff specifies the base delay between attempts; exponential backoff is applied.
// JitterFraction specifies the +/- fractional jitter applied to each computed backoff.
// When Rand is non-nil, it is used to sample jitter for deterministic tests.
type RetryPolicy struct {
	MaxRetries     int
	Backoff        time.Duration
	JitterFraction float64
	Rand           *mathrand.Rand
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

// newLineReader returns a closure that reads one line (terminated by \n) from r each call.
func newLineReader(r io.Reader) func() (string, error) {
	br := bufio.NewReader(r)
	return func() (string, error) {
		b, err := br.ReadBytes('\n')
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
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
		if ne.Timeout() { // ne.Temporary is deprecated; avoid
			return true
		}
	}
	// *url.Error often wraps retryable errors; fall back to string contains of "timeout"
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "timeout")
}

// sleepBackoff retained for backward compatibility; not used.
// Deprecated: use backoffDuration + sleepFor instead.
// func sleepBackoff(base time.Duration, attempt int) { time.Sleep(backoffDuration(base, attempt)) }

// backoffDuration returns the duration that sleepBackoff would sleep for a given attempt.
func backoffDuration(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		base = 200 * time.Millisecond
	}
	d := base << attempt
	if d > 2*time.Second {
		d = 2 * time.Second
	}
	return d
}

// backoffWithJitter returns an exponential backoff adjusted by +/- jitter fraction.
// When jitterFraction <= 0, this falls back to backoffDuration. When r is nil,
// a time-seeded RNG is used for production randomness.
func backoffWithJitter(base time.Duration, attempt int, jitterFraction float64, r *mathrand.Rand) time.Duration {
	d := backoffDuration(base, attempt)
	if jitterFraction <= 0 {
		return d
	}
	if jitterFraction > 0.9 { // prevent extreme factors
		jitterFraction = 0.9
	}
	if r == nil {
		// Seed with current time for production; tests can pass a custom Rand
		r = mathrand.New(mathrand.NewSource(time.Now().UnixNano()))
	}
	// factor in [1 - f, 1 + f]
	minF := 1.0 - jitterFraction
	maxF := 1.0 + jitterFraction
	factor := minF + r.Float64()*(maxF-minF)
	// Guard against rounding to zero
	jittered := time.Duration(float64(d) * factor)
	if jittered < time.Millisecond {
		return time.Millisecond
	}
	return jittered
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
// sleepFunc allows tests to intercept sleeps deterministically.
var sleepFunc = sleepFor

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

// logHTTPAttempt appends an NDJSON line describing an HTTP attempt and planned backoff.
func logHTTPAttempt(stage, idemKey string, attempt, maxAttempts, status int, backoffMs int64, endpoint, errStr string) {
	type audit struct {
		TS             string `json:"ts"`
		Event          string `json:"event"`
		Stage          string `json:"stage,omitempty"`
		IdempotencyKey string `json:"idempotency_key,omitempty"`
		Attempt        int    `json:"attempt"`
		Max            int    `json:"max"`
		Status         int    `json:"status"`
		BackoffMs      int64  `json:"backoffMs"`
		Endpoint       string `json:"endpoint"`
		Error          string `json:"error,omitempty"`
	}
	entry := audit{
		TS:             time.Now().UTC().Format(time.RFC3339Nano),
		Event:          "http_attempt",
		Stage:          stage,
		IdempotencyKey: idemKey,
		Attempt:        attempt,
		Max:            maxAttempts,
		Status:         status,
		BackoffMs:      backoffMs,
		Endpoint:       endpoint,
		Error:          truncate(errStr, 500),
	}
	if err := appendAuditLog(entry); err != nil {
		_ = err
	}
}

// logHTTPTiming appends detailed HTTP timing metrics to the audit log.
func logHTTPTiming(stage, idemKey string, attempt int, endpoint string, status int, start time.Time, dnsDur, connDur, tlsDur time.Duration, wroteAt, firstByteAt, end time.Time, cause, hint string) {
	type timing struct {
		TS             string `json:"ts"`
		Event          string `json:"event"`
		Stage          string `json:"stage,omitempty"`
		IdempotencyKey string `json:"idempotency_key,omitempty"`
		Attempt        int    `json:"attempt"`
		Endpoint       string `json:"endpoint"`
		Status         int    `json:"status"`
		DNSMs          int64  `json:"dnsMs"`
		ConnectMs      int64  `json:"connectMs"`
		TLSMs          int64  `json:"tlsMs"`
		WroteMs        int64  `json:"wroteMs"`
		TTFBMs         int64  `json:"ttfbMs"`
		ReadMs         int64  `json:"readMs"`
		TotalMs        int64  `json:"totalMs"`
		Cause          string `json:"cause"`
		Hint           string `json:"hint,omitempty"`
	}
	var wroteMs, ttfbMs, readMs int64
	if !wroteAt.IsZero() {
		wroteMs = wroteAt.Sub(start).Milliseconds()
	}
	if !firstByteAt.IsZero() {
		if !wroteAt.IsZero() && firstByteAt.After(wroteAt) {
			ttfbMs = firstByteAt.Sub(wroteAt).Milliseconds()
		} else {
			ttfbMs = firstByteAt.Sub(start).Milliseconds()
		}
		if end.After(firstByteAt) {
			readMs = end.Sub(firstByteAt).Milliseconds()
		}
	}
	entry := timing{
		TS:             time.Now().UTC().Format(time.RFC3339Nano),
		Event:          "http_timing",
		Stage:          stage,
		IdempotencyKey: idemKey,
		Attempt:        attempt,
		Endpoint:       endpoint,
		Status:         status,
		DNSMs:          dnsDur.Milliseconds(),
		ConnectMs:      connDur.Milliseconds(),
		TLSMs:          tlsDur.Milliseconds(),
		WroteMs:        wroteMs,
		TTFBMs:         ttfbMs,
		ReadMs:         readMs,
		TotalMs:        end.Sub(start).Milliseconds(),
		Cause:          cause,
		Hint:           hint,
	}
	if err := appendAuditLog(entry); err != nil {
		_ = err
	}
}

// LogLengthBackoff emits a structured NDJSON audit entry describing a
// length_backoff event triggered by finish_reason=="length". Callers should
// pass the model identifier, the previous and new completion caps, the
// effective model context window, and the estimated prompt token count.
func LogLengthBackoff(model string, prevCap, newCap, window, estimatedPromptTokens int) {
	type audit struct {
		TS                    string `json:"ts"`
		Event                 string `json:"event"`
		Model                 string `json:"model"`
		PrevCap               int    `json:"prev_cap"`
		NewCap                int    `json:"new_cap"`
		Window                int    `json:"window"`
		EstimatedPromptTokens int    `json:"estimated_prompt_tokens"`
	}
	entry := audit{
		TS:                    time.Now().UTC().Format(time.RFC3339Nano),
		Event:                 "length_backoff",
		Model:                 model,
		PrevCap:               prevCap,
		NewCap:                newCap,
		Window:                window,
		EstimatedPromptTokens: estimatedPromptTokens,
	}
	if err := appendAuditLog(entry); err != nil {
		_ = err
	}
}

// emitChatMetaAudit writes a one-line NDJSON entry describing request-level
// observability fields such as the effective temperature and whether the
// temperature parameter is included in the payload for the target model.
func emitChatMetaAudit(req ChatCompletionsRequest) {
	// Compute effective temperature based on model support and clamp rules.
	effectiveTemp, supported := EffectiveTemperatureForModel(req.Model, valueOrDefault(req.Temperature, 1.0))
	type meta struct {
		TS                   string  `json:"ts"`
		Event                string  `json:"event"`
		Model                string  `json:"model"`
		TemperatureEffective float64 `json:"temperature_effective"`
		TemperatureInPayload bool    `json:"temperature_in_payload"`
	}
	entry := meta{
		TS:                   time.Now().UTC().Format(time.RFC3339Nano),
		Event:                "chat_meta",
		Model:                req.Model,
		TemperatureEffective: effectiveTemp,
		TemperatureInPayload: supported && req.Temperature != nil,
	}
	if err := appendAuditLog(entry); err != nil {
		_ = err
	}
}

func valueOrDefault(ptr *float64, def float64) float64 {
	if ptr == nil {
		return def
	}
	return *ptr
}

// classifyHTTPCause returns a short cause label for audit based on error/context.
func classifyHTTPCause(ctx context.Context, err error) string {
	if err == nil {
		return "success"
	}
	if errors.Is(err, context.DeadlineExceeded) || (ctx != nil && ctx.Err() == context.DeadlineExceeded) {
		return "context_deadline"
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "server closed") || strings.Contains(s, "connection reset") || strings.Contains(s, "broken pipe"):
		return "server_closed"
	case strings.Contains(s, "timeout"):
		return "timeout"
	default:
		return "error"
	}
}

// userHintForCause returns a short actionable hint for common failure causes.
func userHintForCause(ctx context.Context, err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) || (ctx != nil && ctx.Err() == context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout") {
		return "increase -http-timeout or reduce prompt/model latency"
	}
	return ""
}

// appendAuditLog writes an NDJSON audit line to .goagent/audit/YYYYMMDD.log (same location used by tool runner).
func appendAuditLog(entry any) error {
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
	defer func() {
		if cerr := f.Close(); cerr != nil {
			_ = cerr
		}
	}()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// moduleRoot walks upward from the current working directory to locate the directory
// containing go.mod. If none is found, it returns the current working directory.
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
			// Reached filesystem root; fallback to original cwd
			return cwd
		}
		dir = parent
	}
}
