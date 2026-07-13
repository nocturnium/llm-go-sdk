package resilience

import (
	"context"
	"io"
	"net"
	"syscall"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v5"
)

// mockNetError is a net.Error with a configurable timeout flag, for classifier tests.
type mockNetError struct{ timeout bool }

func (m mockNetError) Error() string   { return "mock net error" }
func (m mockNetError) Timeout() bool   { return m.timeout }
func (m mockNetError) Temporary() bool { return false }

// TestClassifierAgreement is the guardrail that keeps the retry decision and the
// circuit-breaker health decision from ever diverging: DefaultShouldRetry and
// isProviderUnhealthy must return the SAME verdict for every error, and that
// verdict must match the single source of truth — transient upstream failures
// (408/429/5xx/529, streaming Type equivalents, transport blips, socket timeouts)
// are both retried and counted; terminal and client-side errors are neither.
func TestClassifierAgreement(t *testing.T) {
	dnsErr := &net.DNSError{Err: "no such host", Name: "bad.example", IsNotFound: true}
	tests := []struct {
		name      string
		err       error
		transient bool
	}{
		{"nil", nil, false},
		{"429 rate limit", &llms.APIError{StatusCode: 429}, true},
		{"408 request timeout", &llms.APIError{StatusCode: 408}, true},
		{"500", &llms.APIError{StatusCode: 500}, true},
		{"502", &llms.APIError{StatusCode: 502}, true},
		{"503", &llms.APIError{StatusCode: 503}, true},
		{"504", &llms.APIError{StatusCode: 504}, true},
		{"529 overloaded", &llms.APIError{StatusCode: 529}, true},
		{"streaming rate_limit_error (status 0)", &llms.APIError{Type: "rate_limit_error"}, true},
		{"429 insufficient_quota (terminal)", &llms.APIError{StatusCode: 429, Code: "insufficient_quota"}, false},
		{"400 bad request", &llms.APIError{StatusCode: 400}, false},
		{"401 auth", &llms.APIError{StatusCode: 401}, false},
		{"404 not found", &llms.APIError{StatusCode: 404}, false},
		{"io.EOF", io.EOF, true},
		{"io.ErrUnexpectedEOF", io.ErrUnexpectedEOF, true},
		{"ECONNRESET", syscall.ECONNRESET, true},
		{"ECONNREFUSED", syscall.ECONNREFUSED, true},
		{"net socket timeout", mockNetError{timeout: true}, true},
		{"net non-timeout (client-side)", mockNetError{timeout: false}, false},
		{"DNS failure (client-side)", dnsErr, false},
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"circuit open", ErrCircuitOpen, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			retry := DefaultShouldRetry(tc.err)
			health := isProviderUnhealthy(tc.err)
			if retry != health {
				t.Fatalf("classifiers disagree: DefaultShouldRetry=%v isProviderUnhealthy=%v", retry, health)
			}
			if retry != tc.transient {
				t.Errorf("classification = %v, want %v", retry, tc.transient)
			}
		})
	}
}
