package llms

import (
	"errors"
	"testing"
	"time"
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

func TestAPIError_IsRetryable(t *testing.T) {
	tests := []struct {
		statusCode int
		expected   bool
	}{
		{200, false},
		{400, false},
		{401, false},
		{403, false},
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{504, true},
	}

	for _, tc := range tests {
		err := &APIError{StatusCode: tc.statusCode}
		if err.IsRetryable() != tc.expected {
			t.Errorf("IsRetryable for %d = %v, expected %v", tc.statusCode, err.IsRetryable(), tc.expected)
		}
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
