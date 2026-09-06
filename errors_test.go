package llms

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

func TestAPIError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *APIError
		contains string
	}{
		{
			name:     "basic error",
			err:      &APIError{StatusCode: 400, Message: "Bad Request"},
			contains: "status 400",
		},
		{
			name:     "with type",
			err:      &APIError{StatusCode: 401, Message: "Unauthorized", Type: "auth_error"},
			contains: "type auth_error",
		},
		{
			name:     "with type and code",
			err:      &APIError{StatusCode: 429, Message: "Rate limited", Type: "rate_limit_error", Code: "rate_limit_exceeded"},
			contains: "code rate_limit_exceeded",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.err.Error()
			if !contains(result, tc.contains) {
				t.Errorf("expected error to contain %q, got %q", tc.contains, result)
			}
		})
	}
}

func TestAPIError_Is(t *testing.T) {
	tests := []struct {
		name     string
		err      *APIError
		target   error
		expected bool
	}{
		{
			name:     "401 is ErrAuthenticationFailed",
			err:      &APIError{StatusCode: 401},
			target:   ErrAuthenticationFailed,
			expected: true,
		},
		{
			name:     "403 is ErrPermissionDenied",
			err:      &APIError{StatusCode: 403},
			target:   ErrPermissionDenied,
			expected: true,
		},
		{
			name:     "404 is ErrModelNotFound",
			err:      &APIError{StatusCode: 404},
			target:   ErrModelNotFound,
			expected: true,
		},
		{
			name:     "429 is ErrRateLimited",
			err:      &APIError{StatusCode: 429},
			target:   ErrRateLimited,
			expected: true,
		},
		{
			name:     "500 is ErrServerError",
			err:      &APIError{StatusCode: 500},
			target:   ErrServerError,
			expected: true,
		},
		{
			name:     "502 is ErrServiceUnavailable",
			err:      &APIError{StatusCode: 502},
			target:   ErrServiceUnavailable,
			expected: true,
		},
		{
			name:     "504 is ErrServiceUnavailable",
			err:      &APIError{StatusCode: 504},
			target:   ErrServiceUnavailable,
			expected: true,
		},
		{
			name:     "529 is ErrServiceUnavailable",
			err:      &APIError{StatusCode: 529},
			target:   ErrServiceUnavailable,
			expected: true,
		},
		{
			name:     "context_length_exceeded code",
			err:      &APIError{StatusCode: 400, Code: "context_length_exceeded"},
			target:   ErrContextLengthExceeded,
			expected: true,
		},
		{
			name:     "rate_limit_error type",
			err:      &APIError{StatusCode: 400, Type: "rate_limit_error"},
			target:   ErrRateLimited,
			expected: true,
		},
		{
			name:     "200 is not ErrServerError",
			err:      &APIError{StatusCode: 200},
			target:   ErrServerError,
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := errors.Is(tc.err, tc.target)
			if result != tc.expected {
				t.Errorf("errors.Is returned %v, expected %v", result, tc.expected)
			}
		})
	}
}

func TestAPIError_StatusClassification(t *testing.T) {
	tests := []struct {
		status    int
		wantErr   error
		retryable bool
	}{
		{401, ErrAuthenticationFailed, false},
		{402, ErrPlanRequired, false},
		{403, ErrPermissionDenied, false},
		{404, ErrModelNotFound, false},
		{408, ErrTimeout, true},
		{429, ErrRateLimited, true},
		{500, ErrServerError, true},
		{502, ErrServiceUnavailable, true},
		{503, ErrServiceUnavailable, true},
		{504, ErrServiceUnavailable, true},
		{529, ErrServiceUnavailable, true},
	}

	if len(apiStatusClassifications) != len(tests) {
		t.Fatalf("apiStatusClassifications has %d statuses, want %d", len(apiStatusClassifications), len(tests))
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.status), func(t *testing.T) {
			classification, ok := apiStatusClassifications[tt.status]
			if !ok {
				t.Fatalf("missing status classification for %d", tt.status)
			}
			if !errors.Is(classification.err, tt.wantErr) {
				t.Fatalf("classification error = %v, want %v", classification.err, tt.wantErr)
			}
			if classification.retryable != tt.retryable {
				t.Fatalf("classification retryable = %v, want %v", classification.retryable, tt.retryable)
			}

			err := &APIError{StatusCode: tt.status}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("errors.Is(APIError{%d}, %v) = false", tt.status, tt.wantErr)
			}
			if got := err.IsRetryable(); got != tt.retryable {
				t.Fatalf("IsRetryable(%d) = %v, want %v", tt.status, got, tt.retryable)
			}
			if !errors.Is(err.underlyingError(), classification.err) {
				t.Fatalf("underlyingError(%d) = %v, want classification error %v", tt.status, err.underlyingError(), classification.err)
			}
		})
	}
}

func TestAPIError_IsRetryable(t *testing.T) {
	tests := []struct {
		statusCode int
		expected   bool
	}{
		{200, false},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{408, true},
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{504, true},
		{529, true},
	}

	for _, tc := range tests {
		err := &APIError{StatusCode: tc.statusCode}
		if err.IsRetryable() != tc.expected {
			t.Errorf("IsRetryable for %d = %v, expected %v", tc.statusCode, err.IsRetryable(), tc.expected)
		}
	}
}

// TestAPIError_QuotaClassification pins that an out-of-credits error (429 with an
// insufficient_quota / quota_exceeded code) classifies as the permanent
// ErrQuotaExceeded and is non-retryable, while a plain 429 rate limit stays a
// retryable ErrRateLimited. The status map is consulted before the code switch,
// so the quota codes must be given precedence or ErrQuotaExceeded is unreachable.
func TestAPIError_QuotaClassification(t *testing.T) {
	tests := []struct {
		name            string
		err             *APIError
		wantSentinel    error
		wantRateLimited bool
		retryable       bool
	}{
		{
			name:         "429 insufficient_quota is permanent quota, not a rate limit",
			err:          &APIError{StatusCode: 429, Code: "insufficient_quota"},
			wantSentinel: ErrQuotaExceeded,
			retryable:    false,
		},
		{
			name:         "429 quota_exceeded is permanent quota",
			err:          &APIError{StatusCode: 429, Code: "quota_exceeded"},
			wantSentinel: ErrQuotaExceeded,
			retryable:    false,
		},
		{
			name:            "plain 429 (no quota code) stays a retryable rate limit",
			err:             &APIError{StatusCode: 429},
			wantSentinel:    ErrRateLimited,
			wantRateLimited: true,
			retryable:       true,
		},
		{
			name:            "429 rate_limit_exceeded stays a retryable rate limit",
			err:             &APIError{StatusCode: 429, Code: "rate_limit_exceeded"},
			wantSentinel:    ErrRateLimited,
			wantRateLimited: true,
			retryable:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.err, tc.wantSentinel) {
				t.Errorf("errors.Is(err, %v) = false, want true", tc.wantSentinel)
			}
			if got := errors.Is(tc.err, ErrRateLimited); got != tc.wantRateLimited {
				t.Errorf("errors.Is(err, ErrRateLimited) = %v, want %v", got, tc.wantRateLimited)
			}
			if got := tc.err.IsRetryable(); got != tc.retryable {
				t.Errorf("IsRetryable() = %v, want %v", got, tc.retryable)
			}
			if got := IsTemporary(tc.err); got != tc.retryable {
				t.Errorf("IsTemporary() = %v, want %v", got, tc.retryable)
			}
		})
	}
}

// TestAPIError_IsRetryable_TypeFallback pins that when StatusCode is absent
// (e.g. a streaming error carrying only Type/Code, StatusCode 0), IsRetryable
// falls back to the sentinel classification — so a mid-stream rate limit or
// server error is still recognized as retryable instead of defaulting to false.
func TestAPIError_IsRetryable_TypeFallback(t *testing.T) {
	tests := []struct {
		name string
		err  *APIError
		want bool
	}{
		// Anthropic streaming builds APIError{Type:"rate_limit_error"} with no status.
		{"streaming rate_limit_error", &APIError{Type: "rate_limit_error"}, true},
		{"streaming server_error", &APIError{Type: "server_error"}, true},
		{"streaming invalid_request_error", &APIError{Type: "invalid_request_error"}, false},
		{"bare error, no status/type/code", &APIError{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.IsRetryable(); got != tc.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tc.want)
			}
			if got := IsTemporary(tc.err); got != tc.want {
				t.Errorf("IsTemporary() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAPIError_RetryAfter(t *testing.T) {
	err := &APIError{
		StatusCode: 429,
		Message:    "Rate limited",
		RetryAfter: 30 * time.Second,
	}

	if err.RetryAfter != 30*time.Second {
		t.Errorf("expected RetryAfter=30s, got %v", err.RetryAfter)
	}
}

func TestValidationError_Error(t *testing.T) {
	err := ValidationError{
		Field:   "temperature",
		Value:   3.0,
		Message: "must be between 0 and 2",
	}

	result := err.Error()
	if !contains(result, "temperature") || !contains(result, "3") {
		t.Errorf("unexpected error message: %s", result)
	}
}

func TestValidationError_Is(t *testing.T) {
	err := ValidationError{Field: "test", Message: "invalid"}

	if !errors.Is(err, ErrInvalidParameters) {
		t.Error("ValidationError should match ErrInvalidParameters")
	}

	if !errors.Is(fmt.Errorf("wrapped: %w", err), ErrInvalidParameters) {
		t.Error("wrapped ValidationError should match ErrInvalidParameters")
	}
}

func TestValidationErrors_Error(t *testing.T) {
	tests := []struct {
		name     string
		errs     ValidationErrors
		contains []string
	}{
		{
			name:     "empty errors",
			errs:     ValidationErrors{},
			contains: []string{"no validation errors"},
		},
		{
			name:     "single error",
			errs:     ValidationErrors{{Field: "temperature", Message: "too high", Value: 3.0}},
			contains: []string{"temperature", "too high"},
		},
		{
			name: "two errors",
			errs: ValidationErrors{
				{Field: "temperature", Message: "too high"},
				{Field: "max_tokens", Message: "too low"},
			},
			contains: []string{"2 validation errors", "1.", "2.", "temperature", "max_tokens"},
		},
		{
			name: "many errors truncated",
			errs: ValidationErrors{
				{Field: "field1", Message: "error1"},
				{Field: "field2", Message: "error2"},
				{Field: "field3", Message: "error3"},
				{Field: "field4", Message: "error4"},
				{Field: "field5", Message: "error5"},
				{Field: "field6", Message: "error6"},
				{Field: "field7", Message: "error7"},
			},
			contains: []string{"7 validation errors", "showing first 5", "... and 2 more"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.errs.Error()
			for _, substr := range tc.contains {
				if !contains(result, substr) {
					t.Errorf("expected error to contain %q, got: %s", substr, result)
				}
			}
		})
	}
}

func TestValidationErrors_Is(t *testing.T) {
	errs := ValidationErrors{
		{Field: "test", Message: "invalid"},
	}

	if !errors.Is(errs, ErrInvalidParameters) {
		t.Error("ValidationErrors should match ErrInvalidParameters")
	}
}

func TestValidationErrors_Unwrap(t *testing.T) {
	errs := ValidationErrors{{Field: "test"}}

	if !errors.Is(errs.Unwrap(), ErrInvalidParameters) {
		t.Error("Unwrap should return ErrInvalidParameters")
	}
}

func TestStreamError_Error(t *testing.T) {
	err := &StreamError{
		Cause:      errors.New("connection reset"),
		BytesRead:  1024,
		ChunksRead: 5,
	}

	result := err.Error()
	if !contains(result, "5 chunks") || !contains(result, "1024 bytes") {
		t.Errorf("unexpected error message: %s", result)
	}
}

func TestStreamError_Is(t *testing.T) {
	err := &StreamError{Cause: errors.New("test")}

	if !errors.Is(err, ErrStreamInterrupted) {
		t.Error("StreamError should match ErrStreamInterrupted")
	}

	if !errors.Is(fmt.Errorf("wrapped: %w", err), ErrStreamInterrupted) {
		t.Error("wrapped StreamError should match ErrStreamInterrupted")
	}
}

func TestStreamError_Unwrap(t *testing.T) {
	cause := errors.New("original error")
	err := &StreamError{Cause: cause}

	if !errors.Is(err, cause) {
		t.Error("Unwrap should allow matching the cause")
	}
}

func TestProviderError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *ProviderError
		contains []string
	}{
		{
			name: "with operation",
			err: &ProviderError{
				Provider:  ProviderOpenAI,
				Operation: "create message",
				Err:       errors.New("network timeout"),
			},
			contains: []string{"openai", "create message", "network timeout"},
		},
		{
			name: "without operation",
			err: &ProviderError{
				Provider: ProviderAnthropic,
				Err:      errors.New("connection refused"),
			},
			contains: []string{"anthropic", "connection refused"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.err.Error()
			for _, substr := range tc.contains {
				if !contains(result, substr) {
					t.Errorf("expected error to contain %q, got: %s", substr, result)
				}
			}
		})
	}
}

func TestProviderError_Unwrap(t *testing.T) {
	cause := errors.New("original error")
	err := &ProviderError{Provider: ProviderOpenAI, Err: cause}

	if !errors.Is(err, cause) {
		t.Error("ProviderError should unwrap to the original cause")
	}
}

func TestWrapProviderError(t *testing.T) {
	// nil error returns nil
	if WrapProviderError(ProviderOpenAI, "test", nil) != nil {
		t.Error("WrapProviderError should return nil for nil error")
	}

	// non-nil error gets wrapped
	cause := errors.New("test error")
	wrapped := WrapProviderError(ProviderOpenAI, "operation", cause)
	if wrapped == nil {
		t.Fatal("WrapProviderError should not return nil for non-nil error")
	}

	var provErr *ProviderError
	if !errors.As(wrapped, &provErr) {
		t.Error("wrapped error should be a ProviderError")
	}
	if provErr.Provider != ProviderOpenAI {
		t.Errorf("expected provider openai, got %s", provErr.Provider)
	}
}

func TestProviderFromError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected Provider
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: "",
		},
		{
			name:     "plain error",
			err:      errors.New("test"),
			expected: "",
		},
		{
			name: "ProviderError",
			err: &ProviderError{
				Provider: ProviderGemini,
				Err:      errors.New("test"),
			},
			expected: ProviderGemini,
		},
		{
			name: "APIError",
			err: &APIError{
				StatusCode: 500,
				Provider:   ProviderAnthropic,
			},
			expected: ProviderAnthropic,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ProviderFromError(tc.err)
			if result != tc.expected {
				t.Errorf("expected provider %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestIsTemporary(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "ErrRateLimited",
			err:      ErrRateLimited,
			expected: true,
		},
		{
			name:     "ErrServerError",
			err:      ErrServerError,
			expected: true,
		},
		{
			name:     "ErrServiceUnavailable",
			err:      ErrServiceUnavailable,
			expected: true,
		},
		{
			name:     "ErrTimeout",
			err:      ErrTimeout,
			expected: true,
		},
		{
			name:     "ErrConnectionFailed",
			err:      ErrConnectionFailed,
			expected: true,
		},
		{
			name:     "ErrStreamInterrupted",
			err:      ErrStreamInterrupted,
			expected: true,
		},
		{
			name:     "ErrAuthenticationFailed",
			err:      ErrAuthenticationFailed,
			expected: false,
		},
		{
			name:     "ErrInvalidParameters",
			err:      ErrInvalidParameters,
			expected: false,
		},
		{
			name: "APIError 429",
			err: &APIError{
				StatusCode: 429,
				Message:    "rate limited",
			},
			expected: true,
		},
		{
			name: "APIError 500",
			err: &APIError{
				StatusCode: 500,
				Message:    "server error",
			},
			expected: true,
		},
		{
			name: "APIError 400",
			err: &APIError{
				StatusCode: 400,
				Message:    "bad request",
			},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsTemporary(tc.err)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestGetErrorDetails(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		details := GetErrorDetails(nil)
		if details.Provider != "" || details.StatusCode != 0 || details.IsRetryable {
			t.Error("nil error should return empty details")
		}
	})

	t.Run("APIError with details", func(t *testing.T) {
		err := &APIError{
			StatusCode: 429,
			Message:    "rate limited",
			Provider:   ProviderOpenAI,
			RequestID:  "req_123",
			RetryAfter: 30 * time.Second,
		}

		details := GetErrorDetails(err)
		if details.Provider != ProviderOpenAI {
			t.Errorf("expected provider openai, got %s", details.Provider)
		}
		if details.StatusCode != 429 {
			t.Errorf("expected status 429, got %d", details.StatusCode)
		}
		if !details.IsRetryable {
			t.Error("429 should be retryable")
		}
		if details.RetryAfter != 30*time.Second {
			t.Errorf("expected RetryAfter 30s, got %v", details.RetryAfter)
		}
		if details.RequestID != "req_123" {
			t.Errorf("expected RequestID req_123, got %s", details.RequestID)
		}
	})

	t.Run("wrapped error finds root cause", func(t *testing.T) {
		rootCause := errors.New("root cause")
		wrapped := &ProviderError{
			Provider: ProviderGemini,
			Err:      rootCause,
		}

		details := GetErrorDetails(wrapped)
		if !errors.Is(details.RootCause, rootCause) {
			t.Errorf("expected root cause to be original error")
		}
	})
}

func TestFormatError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		contains string
	}{
		{
			name:     "nil error",
			err:      nil,
			contains: "",
		},
		{
			name:     "plain error",
			err:      errors.New("simple error"),
			contains: "simple error",
		},
		{
			name: "APIError",
			err: &APIError{
				StatusCode: 401,
				Message:    "unauthorized",
			},
			contains: "status 401",
		},
		{
			name: "ProviderError",
			err: &ProviderError{
				Provider:  ProviderOpenAI,
				Operation: "generate",
				Err:       errors.New("failed"),
			},
			contains: "openai",
		},
		{
			name: "ValidationErrors",
			err: ValidationErrors{
				{Field: "temp", Message: "invalid"},
			},
			contains: "temp",
		},
		{
			name: "StreamError",
			err: &StreamError{
				Cause:      errors.New("disconnect"),
				ChunksRead: 10,
			},
			contains: "10 chunks",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := FormatError(tc.err)
			if tc.contains == "" {
				if result != "" {
					t.Errorf("expected empty string, got %q", result)
				}
			} else if !contains(result, tc.contains) {
				t.Errorf("expected result to contain %q, got %q", tc.contains, result)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestNewAPIErrorFromHTTP_ModelNotFound preserves the transport-to-root
// classification assertion here so httpclient's own tests do not import llms.
func TestNewAPIErrorFromHTTP_ModelNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"model not found\ntry another","type":"invalid_request_error","code":"model_not_found","param":"model"}}`))
	}))
	defer server.Close()
	client := httpclient.NewClient(httpclient.WithAllowPrivateIPs(true), httpclient.WithAllowHTTP(true))
	err := client.DoJSON(context.Background(), httpclient.Request{Method: http.MethodPost, URL: server.URL}, nil)
	var apiErr *httpclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *httpclient.APIError, got %v", err)
	}
	unified := NewAPIErrorFromHTTP(ProviderOpenAI, HTTPAPIError{
		StatusCode: apiErr.StatusCode,
		Message:    apiErr.Message,
		Type:       apiErr.Type,
		Code:       apiErr.Code,
		Param:      apiErr.Param,
	})
	if !errors.Is(unified, ErrModelNotFound) {
		t.Fatalf("errors.Is(unified, ErrModelNotFound) = false for %#v", unified)
	}
}
