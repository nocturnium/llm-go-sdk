package resilience

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net"
	"syscall"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v3"
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

// DefaultShouldRetry returns true for errors that are typically retryable
func DefaultShouldRetry(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific error types
	var apiErr *llms.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 429: // Rate limited
			return true
		case 500, 502, 503, 504: // Server errors
			return true
		}
	}

	// Check for context errors (not retryable)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Check for circuit breaker (not retryable - breaker handles its own timing)
	if errors.Is(err, ErrCircuitOpen) {
		return false
	}

	return false
}

// isProviderUnhealthy reports whether an error indicates the upstream provider
// is unhealthy, i.e. the kind of transient failure the circuit breaker exists to
// protect against. It deliberately returns false for context cancellation,
// context deadlines, 4xx client errors other than 429, and circuit-open errors.
func isProviderUnhealthy(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ErrCircuitOpen) {
		return false
	}

	var apiErr *llms.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 429, 500, 502, 503, 504:
			return true
		}
	}

	if errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) {
		return true
	}

	var netErr net.Error
	return errors.As(err, &netErr)
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
