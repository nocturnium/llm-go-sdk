package httpclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestDoWithRetry_RetriesTransientStatuses asserts END-TO-END (not just policy
// list membership) that every transient status actually triggers retries through
// doWithRetry. This matters because retries are co-gated by IsRetryable(status)
// AND the policy list: 529 (Anthropic "Site Overloaded") has no net/http constant
// and must be present in BOTH, or a real 529 is returned with zero retries.
func TestDoWithRetry_RetriesTransientStatuses(t *testing.T) {
	for _, status := range []int{408, 429, 500, 502, 503, 504, 529} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			var attempts int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				atomic.AddInt32(&attempts, 1)
				w.WriteHeader(status)
			}))
			defer server.Close()

			client := NewClient(
				WithRetryPolicy(&RetryPolicy{
					MaxRetries:           2,
					InitialDelay:         0,
					MaxDelay:             0,
					Multiplier:           1,
					RetryableStatusCodes: DefaultRetryPolicy().RetryableStatusCodes,
				}),
				WithAllowPrivateIPs(true), WithAllowHTTP(true),
			)
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
			resp, err := client.doWithRetry(context.Background(), req)
			if err == nil {
				_ = resp.Body.Close()
			}
			if got := atomic.LoadInt32(&attempts); got != 3 {
				t.Errorf("status %d: server saw %d attempts, want 3 (1 initial + 2 retries)", status, got)
			}
		})
	}

	// Non-retryable statuses must be hit exactly once.
	for _, status := range []int{200, 400, 401, 404} {
		t.Run(fmt.Sprintf("no_retry_%d", status), func(t *testing.T) {
			var attempts int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				atomic.AddInt32(&attempts, 1)
				w.WriteHeader(status)
			}))
			defer server.Close()

			client := NewClient(
				WithRetryPolicy(&RetryPolicy{
					MaxRetries:           2,
					RetryableStatusCodes: DefaultRetryPolicy().RetryableStatusCodes,
				}),
				WithAllowPrivateIPs(true), WithAllowHTTP(true),
			)
			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
			resp, err := client.doWithRetry(context.Background(), req)
			if err == nil {
				_ = resp.Body.Close()
			}
			if got := atomic.LoadInt32(&attempts); got != 1 {
				t.Errorf("status %d: server saw %d attempts, want 1 (no retry)", status, got)
			}
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	future := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	tests := []struct {
		name string
		in   string
		min  time.Duration
		max  time.Duration
	}{
		{"empty", "", 0, 0},
		{"seconds", "5", 5 * time.Second, 5 * time.Second},
		{"zero", "0", 0, 0},
		{"negative", "-3", 0, 0},
		{"garbage", "soon", 0, 0},
		{"http-date future", future, 60 * time.Second, 95 * time.Second},
		{"http-date past", past, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseRetryAfter(tc.in)
			if got < tc.min || got > tc.max {
				t.Errorf("parseRetryAfter(%q) = %v, want in [%v, %v]", tc.in, got, tc.min, tc.max)
			}
		})
	}
}

func TestJitterDelay_Bounds(t *testing.T) {
	for _, d := range []time.Duration{0, 1, 100 * time.Millisecond, time.Second, 30 * time.Second} {
		for range 100 {
			got := jitterDelay(d)
			if d <= 0 {
				if got != d {
					t.Fatalf("jitterDelay(%v) = %v, want %v", d, got, d)
				}
				continue
			}
			if got < d/2 || got > d {
				t.Fatalf("jitterDelay(%v) = %v, want in [%v, %v]", d, got, d/2, d)
			}
		}
	}
}

// TestDoWithRetry_PreservesResponseOnExhaustion asserts that after exhausting
// retries on a retryable status, doWithRetry returns the final RESPONSE (so the
// caller can parse the provider's *APIError) rather than draining it into a
// generic "retries exhausted" error.
func TestDoWithRetry_PreservesResponseOnExhaustion(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"overloaded","type":"server_error"}}`))
	}))
	defer server.Close()

	client := NewClient(
		WithRetryPolicy(&RetryPolicy{MaxRetries: 2, InitialDelay: 0, MaxDelay: 0, Multiplier: 1, RetryableStatusCodes: []int{503}}),
		WithAllowPrivateIPs(true), WithAllowHTTP(true),
	)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	resp, err := client.doWithRetry(context.Background(), req)
	if err != nil {
		t.Fatalf("doWithRetry returned an error instead of the final response: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (response preserved)", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 { // 1 initial + 2 retries
		t.Errorf("server saw %d attempts, want 3", got)
	}
	// The caller can now parse a typed *APIError from the preserved response.
	var ae *APIError
	if err := client.handleErrorResponse(resp); !errors.As(err, &ae) || ae.StatusCode != 503 {
		t.Errorf("handleErrorResponse did not yield an *APIError{503}: %v", err)
	}
}

// TestDoWithRetry_HonorsRetryAfter asserts the retry waits for a server-provided
// Retry-After that exceeds the (tiny) computed backoff.
func TestDoWithRetry_HonorsRetryAfter(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("slow down"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(
		// Tiny backoff: without honoring Retry-After the retry would fire in ~1ms.
		WithRetryPolicy(&RetryPolicy{MaxRetries: 2, InitialDelay: time.Millisecond, MaxDelay: 10 * time.Second, Multiplier: 2, RetryableStatusCodes: []int{429}}),
		WithAllowPrivateIPs(true), WithAllowHTTP(true),
	)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	start := time.Now()
	resp, err := client.doWithRetry(context.Background(), req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("doWithRetry: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("retry fired after %v; expected to honor Retry-After: 1s", elapsed)
	}
}
