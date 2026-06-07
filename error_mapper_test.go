package llms

import (
	"errors"
	"testing"
)

func TestErrorMapper_Map(t *testing.T) {
	mapper := NewErrorMapper(ProviderOpenAI).
		AddStatusMapping(429, ErrRateLimited).
		AddCodeMapping("context_length_exceeded", ErrContextLengthExceeded).
		AddTypeMapping("authentication_error", ErrAuthenticationFailed).
		AddMessageMapping("quota exceeded", ErrQuotaExceeded)

	tests := []struct {
		name       string
		statusCode int
		errorCode  string
		errorType  string
		message    string
		wantErr    error
	}{
		{
			name:       "matches status code",
			statusCode: 429,
			wantErr:    ErrRateLimited,
		},
		{
			name:      "matches error code",
			errorCode: "context_length_exceeded",
			wantErr:   ErrContextLengthExceeded,
		},
		{
			name:      "matches error code case insensitive",
			errorCode: "CONTEXT_LENGTH_EXCEEDED",
			wantErr:   ErrContextLengthExceeded,
		},
		{
			name:      "matches error type",
			errorType: "authentication_error",
			wantErr:   ErrAuthenticationFailed,
		},
		{
			name:    "matches message substring",
			message: "Your quota exceeded the limit",
			wantErr: ErrQuotaExceeded,
		},
		{
			name:    "matches message case insensitive",
			message: "QUOTA EXCEEDED",
			wantErr: ErrQuotaExceeded,
		},
		{
			name:       "no match returns nil",
			statusCode: 200,
			errorCode:  "unknown",
			wantErr:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapper.Map(tt.statusCode, tt.errorCode, tt.errorType, tt.message)
			if !errors.Is(got, tt.wantErr) {
				t.Errorf("got %v, want %v", got, tt.wantErr)
			}
		})
	}
}

func TestErrorMapper_MapAPIError(t *testing.T) {
	mapper := NewErrorMapper(ProviderOpenAI).
		AddCodeMapping("model_not_found", ErrModelNotFound)

	t.Run("maps API error", func(t *testing.T) {
		apiErr := &APIError{
			StatusCode: 404,
			Code:       "model_not_found",
			Message:    "Model not found",
		}

		got := mapper.MapAPIError(apiErr)
		if !errors.Is(got, ErrModelNotFound) {
			t.Errorf("got %v, want ErrModelNotFound", got)
		}
	})

	t.Run("nil API error returns nil", func(t *testing.T) {
		got := mapper.MapAPIError(nil)
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})
}

func TestErrorMapper_CombinedConditions(t *testing.T) {
	mapper := NewErrorMapper(ProviderOpenAI).
		AddMapping(ErrorMapping{
			HTTPStatus: 400,
			ErrorCode:  "invalid_model",
			Target:     ErrModelNotFound,
		})

	t.Run("matches when all conditions met", func(t *testing.T) {
		got := mapper.Map(400, "invalid_model", "", "")
		if !errors.Is(got, ErrModelNotFound) {
			t.Errorf("got %v, want ErrModelNotFound", got)
		}
	})

	t.Run("no match when status differs", func(t *testing.T) {
		got := mapper.Map(404, "invalid_model", "", "")
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("no match when code differs", func(t *testing.T) {
		got := mapper.Map(400, "other_error", "", "")
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})
}

func TestErrorMapperRegistry(t *testing.T) {
	registry := NewErrorMapperRegistry()

	t.Run("returns registered mapper", func(t *testing.T) {
		mapper := registry.Get(ProviderOpenAI)
		if mapper == nil {
			t.Fatal("expected OpenAI mapper, got nil")
		}
	})

	t.Run("returns nil for unregistered provider", func(t *testing.T) {
		mapper := registry.Get("unknown-provider")
		if mapper != nil {
			t.Errorf("expected nil, got %v", mapper)
		}
	})

	t.Run("custom mapper can be registered", func(t *testing.T) {
		custom := NewErrorMapper("custom").
			AddCodeMapping("custom_error", ErrServerError)

		registry.Register(custom)

		got := registry.Get("custom")
		if got == nil {
			t.Fatal("expected custom mapper, got nil")
		}
	})
}

func TestErrorMapperRegistry_MapError(t *testing.T) {
	registry := NewErrorMapperRegistry()

	t.Run("uses provider-specific mapper", func(t *testing.T) {
		// OpenAI maps "insufficient_quota" to ErrQuotaExceeded
		got := registry.MapError(ProviderOpenAI, 0, "insufficient_quota", "", "")
		if !errors.Is(got, ErrQuotaExceeded) {
			t.Errorf("got %v, want ErrQuotaExceeded", got)
		}
	})

	t.Run("falls back to generic mapping", func(t *testing.T) {
		// Unknown provider with 429 status
		got := registry.MapError("unknown", 429, "", "", "")
		if !errors.Is(got, ErrRateLimited) {
			t.Errorf("got %v, want ErrRateLimited", got)
		}
	})

	t.Run("returns nil for unhandled error", func(t *testing.T) {
		got := registry.MapError("unknown", 0, "unknown_code", "", "")
		if got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})
}

func TestMapGenericStatus(t *testing.T) {
	tests := []struct {
		status  int
		wantErr error
	}{
		{400, ErrInvalidParameters},
		{401, ErrAuthenticationFailed},
		{403, ErrPermissionDenied},
		{404, ErrModelNotFound},
		{429, ErrRateLimited},
		{500, ErrServerError},
		{502, ErrServiceUnavailable},
		{503, ErrServiceUnavailable},
		{504, ErrServiceUnavailable},
		{200, nil}, // Success
		{418, nil}, // Unhandled
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := mapGenericStatus(tt.status)
			if !errors.Is(got, tt.wantErr) {
				t.Errorf("status %d: got %v, want %v", tt.status, got, tt.wantErr)
			}
		})
	}
}

func TestOpenAIErrorMappings(t *testing.T) {
	registry := DefaultErrorMapperRegistry()

	tests := []struct {
		name      string
		code      string
		errorType string
		message   string
		wantErr   error
	}{
		{"context length", "context_length_exceeded", "", "", ErrContextLengthExceeded},
		{"model not found", "model_not_found", "", "", ErrModelNotFound},
		{"invalid api key", "invalid_api_key", "", "", ErrAuthenticationFailed},
		{"rate limit", "rate_limit_exceeded", "", "", ErrRateLimited},
		{"quota", "insufficient_quota", "", "", ErrQuotaExceeded},
		{"content filter", "content_policy_violation", "", "", ErrContentFiltered},
		{"invalid request type", "", "invalid_request_error", "", ErrInvalidParameters},
		{"auth error type", "", "authentication_error", "", ErrAuthenticationFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := registry.MapError(ProviderOpenAI, 0, tt.code, tt.errorType, tt.message)
			if !errors.Is(got, tt.wantErr) {
				t.Errorf("got %v, want %v", got, tt.wantErr)
			}
		})
	}
}

func TestAnthropicErrorMappings(t *testing.T) {
	registry := DefaultErrorMapperRegistry()

	tests := []struct {
		name      string
		status    int
		errorType string
		message   string
		wantErr   error
	}{
		{"overloaded status", 529, "", "", ErrServiceUnavailable},
		{"rate limit type", 0, "rate_limit_error", "", ErrRateLimited},
		{"overloaded type", 0, "overloaded_error", "", ErrServiceUnavailable},
		{"context in message", 0, "", "Your context length is too long", ErrContextLengthExceeded},
		{"content policy in message", 0, "", "Violates content policy", ErrContentFiltered},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := registry.MapError(ProviderAnthropic, tt.status, "", tt.errorType, tt.message)
			if !errors.Is(got, tt.wantErr) {
				t.Errorf("got %v, want %v", got, tt.wantErr)
			}
		})
	}
}

func TestGeminiErrorMappings(t *testing.T) {
	registry := DefaultErrorMapperRegistry()

	tests := []struct {
		name    string
		status  int
		message string
		wantErr error
	}{
		{"api key invalid", 0, "API key not valid", ErrAuthenticationFailed},
		{"quota exceeded", 0, "You have exceeded your quota", ErrQuotaExceeded},
		{"safety blocked", 0, "Response blocked for safety reasons", ErrContentFiltered},
		{"content blocked", 0, "Content was blocked", ErrContentFiltered},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := registry.MapError(ProviderGemini, tt.status, "", "", tt.message)
			if !errors.Is(got, tt.wantErr) {
				t.Errorf("got %v, want %v", got, tt.wantErr)
			}
		})
	}
}

func TestMapProviderError(t *testing.T) {
	// Test global convenience function
	got := MapProviderError(ProviderOpenAI, 429, "", "", "")
	if !errors.Is(got, ErrRateLimited) {
		t.Errorf("got %v, want ErrRateLimited", got)
	}
}

func TestRegisterErrorMapper(t *testing.T) {
	// Test custom registration via global function
	custom := NewErrorMapper("test-provider").
		AddCodeMapping("test_error", ErrServerError)

	RegisterErrorMapper(custom)

	got := MapProviderError("test-provider", 0, "test_error", "", "")
	if !errors.Is(got, ErrServerError) {
		t.Errorf("got %v, want ErrServerError", got)
	}
}
