package image

import (
	"net/http"
	"time"
)

// RetryPolicy controls retry behavior for image HTTP calls.
// MaxRetries specifies the number of retries after the initial attempt.
// Backoff specifies the base backoff duration between attempts.
type RetryPolicy struct {
	MaxRetries int
	Backoff    time.Duration
}

// Client is a minimal HTTP client wrapper for image requests that carries
// the resolved timeout and retry policy.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	retry      RetryPolicy
}

// NewClient constructs a Client with the provided configuration.
// The httpTimeout applies to the underlying http.Client Timeout.
// Retries and backoff are stored in a simple RetryPolicy.
func NewClient(baseURL, apiKey string, httpTimeout time.Duration, retries int, backoff time.Duration) *Client {
	if httpTimeout <= 0 {
		httpTimeout = 90 * time.Second
	}
	if retries < 0 {
		retries = 0
	}
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: httpTimeout,
		},
		retry: RetryPolicy{MaxRetries: retries, Backoff: backoff},
	}
}

// HTTPTimeout returns the configured HTTP timeout.
func (c *Client) HTTPTimeout() time.Duration { return c.httpClient.Timeout }

// Retry returns the configured RetryPolicy.
func (c *Client) Retry() RetryPolicy { return c.retry }
