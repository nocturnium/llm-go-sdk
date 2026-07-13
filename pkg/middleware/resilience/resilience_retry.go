package resilience

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"syscall"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v5"
)

// RetryConfig configures retry behavior
type RetryConfig struct {
	MaxAttempts   int           // Maximum number of attempts (including first)
	InitialDelay  time.Duration // Initial delay before first retry
	MaxDelay      time.Duration // Maximum delay between retries
	BackoffFactor float64       // Multiplier for exponential backoff
	Jitter        float64       // Random jitter factor (0-1)

	// ShouldRetry determines if an error is retryable
	ShouldRetry func(error) bool
}

// DefaultRetryConfig returns a sensible default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxAttempts:   3,
		InitialDelay:  1 * time.Second,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        0.1,
		ShouldRetry:   DefaultShouldRetry,
	}
}

// DefaultShouldRetry returns true for errors that are typically transient and
// worth retrying. It shares a single classification with isProviderUnhealthy (see
// isTransientProviderError), so the retry decision and the circuit-breaker health
// decision can never disagree — a transient error is always both retried and
// counted, and a terminal/client-side error is neither.
func DefaultShouldRetry(err error) bool {
	return isTransientProviderError(err)
}

// isProviderUnhealthy reports whether an error indicates the upstream provider is
// unhealthy — the kind of transient failure the circuit breaker exists to protect
// against. It shares one classification with DefaultShouldRetry.
func isProviderUnhealthy(err error) bool {
	return isTransientProviderError(err)
}

// isTransientProviderError is the single source of truth for "is this a transient
// upstream failure worth retrying and counting against the breaker". It is true
// for retryable HTTP statuses (408, 429, and 5xx including 529, plus streaming
// Type/Code equivalents) and transient transport errors (EOF, connection
// reset/refused, socket timeouts). It is deliberately false for caller-side
// signals — context cancellation/deadline, circuit-open, terminal 4xx (including
// quota), and client-side configuration errors such as DNS failures — so a burst
// of bad requests or a mistyped host neither retries pointlessly nor trips the
// breaker on an otherwise healthy provider.
func isTransientProviderError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ErrCircuitOpen) {
		return false
	}

	// Upstream HTTP errors: defer to the errors-package classifier (the same one
	// behind APIError.IsRetryable / IsTemporary) so 408/429/5xx/529 and streaming
	// Type/Code equivalents are transient while quota and terminal 4xx are not.
	var apiErr *llms.APIError
	if errors.As(err, &apiErr) {
		return apiErr.IsRetryable()
	}

	// Transient transport failures from the provider connection.
	if errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) {
		return true
	}

	// A DNS resolution failure is a client-side/config error, not a provider
	// health signal; retrying it will not help and it must not trip the breaker.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return false
	}
	// A genuine socket timeout to the provider is transient; other non-timeout
	// net errors are treated as client-side and excluded.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

// calculateDelay calculates the retry delay with jitter
func calculateDelay(baseDelay time.Duration, jitter float64) time.Duration {
	if jitter <= 0 {
		return baseDelay
	}
	if jitter > 1 {
		jitter = 1
	}

	// Add random jitter: delay * (1 + random(-jitter, +jitter))
	// Using math/rand/v2 for proper randomness to avoid thundering herd
	// Note: math/rand is acceptable here for jitter (not cryptographic use)
	jitterRange := float64(baseDelay) * jitter
	jitterValue := (rand.Float64() - 0.5) * 2 * jitterRange // #nosec G404 -- non-cryptographic jitter for thundering-herd spread; math/rand/v2 is intentional
	return time.Duration(float64(baseDelay) + jitterValue)
}
