package httpclient

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDefaultRetryPolicy(t *testing.T) {
	policy := DefaultRetryPolicy()

	if policy.MaxRetries != 3 {
		t.Errorf("expected MaxRetries=3, got %d", policy.MaxRetries)
	}
	if policy.InitialDelay != 1*time.Second {
		t.Errorf("expected InitialDelay=1s, got %v", policy.InitialDelay)
	}
	if policy.MaxDelay != 30*time.Second {
		t.Errorf("expected MaxDelay=30s, got %v", policy.MaxDelay)
	}
	if policy.Multiplier != 2.0 {
		t.Errorf("expected Multiplier=2.0, got %v", policy.Multiplier)
	}
}

func TestNoRetryPolicy(t *testing.T) {
	policy := NoRetryPolicy()

	if policy.MaxRetries != 0 {
		t.Errorf("expected MaxRetries=0, got %d", policy.MaxRetries)
	}
}

func TestRetryPolicy_ShouldRetry(t *testing.T) {
	policy := DefaultRetryPolicy()

	tests := []struct {
		statusCode  int
		shouldRetry bool
	}{
		{200, false},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{429, true}, // Rate limit
		{500, true}, // Internal Server Error
		{502, true}, // Bad Gateway
		{503, true}, // Service Unavailable
		{504, true}, // Gateway Timeout
	}

	for _, tc := range tests {
		result := policy.ShouldRetry(tc.statusCode)
		if result != tc.shouldRetry {
			t.Errorf("ShouldRetry(%d) = %v, expected %v", tc.statusCode, result, tc.shouldRetry)
		}
	}
}

func TestRetryPolicy_GetDelay(t *testing.T) {
	policy := &RetryPolicy{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
	}

	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 0},
		{1, 100 * time.Millisecond},
		{2, 200 * time.Millisecond},
		{3, 400 * time.Millisecond},
		{4, 800 * time.Millisecond},
		{5, 1 * time.Second}, // Capped at MaxDelay
		{6, 1 * time.Second}, // Still capped
	}

	for _, tc := range tests {
		result := policy.GetDelay(tc.attempt)
		if result != tc.expected {
			t.Errorf("GetDelay(%d) = %v, expected %v", tc.attempt, result, tc.expected)
		}
	}
}

func TestRetryPolicy_GetDelay_NoMaxDelay(t *testing.T) {
	policy := &RetryPolicy{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     0, // No max delay
		Multiplier:   2.0,
	}

	// When MaxDelay is 0, delays should still be capped at 0
	result := policy.GetDelay(5)
	if result != 0 {
		t.Errorf("GetDelay with MaxDelay=0 should return 0, got %v", result)
	}
}

func TestRetryPolicy_CustomCodes(t *testing.T) {
	policy := &RetryPolicy{
		RetryableStatusCodes: []int{418, 503}, // Custom codes
	}

	if !policy.ShouldRetry(418) {
		t.Error("expected 418 to be retryable")
	}
	if !policy.ShouldRetry(503) {
		t.Error("expected 503 to be retryable")
	}
	if policy.ShouldRetry(429) {
		t.Error("expected 429 to NOT be retryable with custom codes")
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		err        error
		want       bool
	}{
		{name: "408 request timeout", statusCode: 408, want: true},
		{name: "429 too many requests", statusCode: 429, want: true},
		{name: "500 internal server error", statusCode: 500, want: true},
		{name: "502 bad gateway", statusCode: 502, want: true},
		{name: "503 service unavailable", statusCode: 503, want: true},
		{name: "504 gateway timeout", statusCode: 504, want: true},
		{name: "400 bad request", statusCode: 400, want: false},
		{name: "401 unauthorized", statusCode: 401, want: false},
		{name: "403 forbidden", statusCode: 403, want: false},
		{name: "404 not found", statusCode: 404, want: false},
		{name: "422 unprocessable entity", statusCode: 422, want: false},
		{name: "generic network error", err: errors.New("temporary network error"), want: true},
		{name: "context canceled", err: context.Canceled, want: false},
		{name: "context deadline exceeded", err: context.DeadlineExceeded, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsRetryable(tc.statusCode, tc.err)
			if got != tc.want {
				t.Fatalf("IsRetryable(%d, %v) = %v, want %v", tc.statusCode, tc.err, got, tc.want)
			}
		})
	}
}
