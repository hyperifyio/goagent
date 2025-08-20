package oai

import (
    mathrand "math/rand"
    "net/http"
    "strings"
    "time"
)

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
