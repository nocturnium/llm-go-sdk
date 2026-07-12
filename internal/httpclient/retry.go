package httpclient

import (
	"math"
	"time"
)

// RetryPolicy defines the retry behavior for HTTP requests.
type RetryPolicy struct {
	// MaxRetries is the maximum number of retry attempts
	MaxRetries int

	// InitialDelay is the delay before the first retry
	InitialDelay time.Duration

	// MaxDelay is the maximum delay between retries
	MaxDelay time.Duration

	// Multiplier is the factor by which the delay increases after each retry
	Multiplier float64

	// RetryableStatusCodes are the HTTP status codes that trigger a retry
	RetryableStatusCodes []int
}

// DefaultRetryPolicy returns the default retry policy.
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxRetries:   3,
		InitialDelay: 1 * time.Second,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		RetryableStatusCodes: []int{
			408, // Request Timeout
			429, // Too Many Requests
			500, // Internal Server Error
			502, // Bad Gateway
			503, // Service Unavailable
			504, // Gateway Timeout
			529, // Site Overloaded (Anthropic)
		},
	}
}

// NoRetryPolicy returns a policy that never retries.
func NoRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxRetries: 0,
	}
}

// ShouldRetry returns true if the given status code should trigger a retry.
func (p *RetryPolicy) ShouldRetry(statusCode int) bool {
	for _, code := range p.RetryableStatusCodes {
		if code == statusCode {
			return true
		}
	}
	return false
}

// GetDelay calculates the delay for the given attempt number (1-based).
//
// Safeguards:
//   - If Multiplier is <= 0, defaults to 2.0 to ensure exponential backoff
//   - Handles overflow from math.Pow by capping at MaxDelay
//   - Negative delays are prevented by returning 0 for attempt <= 0
func (p *RetryPolicy) GetDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}

	// Ensure multiplier is valid - use 2.0 as sensible default
	multiplier := p.Multiplier
	if multiplier <= 0 {
		multiplier = 2.0
	}

	delay := float64(p.InitialDelay) * math.Pow(multiplier, float64(attempt-1))

	// Handle overflow or negative results from math.Pow
	// math.IsInf catches overflow, math.IsNaN catches undefined operations
	if math.IsInf(delay, 0) || math.IsNaN(delay) || delay > float64(p.MaxDelay) {
		delay = float64(p.MaxDelay)
	}

	// Ensure we don't return a negative duration
	if delay < 0 {
		delay = float64(p.InitialDelay)
	}

	return time.Duration(delay)
}
