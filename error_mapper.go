package llms

import (
	"strings"
	"sync"
)

// ErrorMapping defines how to map a provider-specific error to a standard error.
type ErrorMapping struct {
	// HTTPStatus matches on HTTP status code (0 means don't match on status)
	HTTPStatus int

	// ErrorCode matches on the error code string from the API
	ErrorCode string

	// ErrorType matches on the error type string from the API
	ErrorType string

	// MessageContains matches if the error message contains this substring
	MessageContains string

	// Target is the standard error to return when matched
	Target error
}

// ErrorMapper maps provider-specific API errors to standard error types.
// This ensures consistent error handling across providers.
type ErrorMapper struct {
	provider Provider
	mappings []ErrorMapping
}

// NewErrorMapper creates a new error mapper for a provider.
func NewErrorMapper(provider Provider) *ErrorMapper {
	return &ErrorMapper{
		provider: provider,
		mappings: []ErrorMapping{},
	}
}

// AddMapping adds a new error mapping.
func (m *ErrorMapper) AddMapping(mapping ErrorMapping) *ErrorMapper {
	m.mappings = append(m.mappings, mapping)
	return m
}

// AddStatusMapping adds a mapping for an HTTP status code.
func (m *ErrorMapper) AddStatusMapping(status int, target error) *ErrorMapper {
	return m.AddMapping(ErrorMapping{HTTPStatus: status, Target: target})
}

// AddCodeMapping adds a mapping for an error code string.
func (m *ErrorMapper) AddCodeMapping(code string, target error) *ErrorMapper {
	return m.AddMapping(ErrorMapping{ErrorCode: code, Target: target})
}

// AddTypeMapping adds a mapping for an error type string.
func (m *ErrorMapper) AddTypeMapping(errorType string, target error) *ErrorMapper {
	return m.AddMapping(ErrorMapping{ErrorType: errorType, Target: target})
}

// AddMessageMapping adds a mapping for error message substring matching.
func (m *ErrorMapper) AddMessageMapping(substring string, target error) *ErrorMapper {
	return m.AddMapping(ErrorMapping{MessageContains: substring, Target: target})
}

// Map attempts to map an API error to a standard error.
// Returns the target error if a mapping matches, nil otherwise.
func (m *ErrorMapper) Map(statusCode int, errorCode, errorType, message string) error {
	for _, mapping := range m.mappings {
		if m.matches(mapping, statusCode, errorCode, errorType, message) {
			return mapping.Target
		}
	}
	return nil
}

// MapAPIError maps an APIError to a standard error.
// This is a convenience method that extracts fields from APIError.
func (m *ErrorMapper) MapAPIError(err *APIError) error {
	if err == nil {
		return nil
	}
	return m.Map(err.StatusCode, err.Code, err.Type, err.Message)
}

func (m *ErrorMapper) matches(mapping ErrorMapping, statusCode int, errorCode, errorType, message string) bool {
	// All specified conditions must match
	if mapping.HTTPStatus != 0 && mapping.HTTPStatus != statusCode {
		return false
	}
	if mapping.ErrorCode != "" && !strings.EqualFold(mapping.ErrorCode, errorCode) {
		return false
	}
	if mapping.ErrorType != "" && !strings.EqualFold(mapping.ErrorType, errorType) {
		return false
	}
	if mapping.MessageContains != "" && !strings.Contains(strings.ToLower(message), strings.ToLower(mapping.MessageContains)) {
		return false
	}

	// At least one condition must be specified
	return mapping.HTTPStatus != 0 || mapping.ErrorCode != "" || mapping.ErrorType != "" || mapping.MessageContains != ""
}

// ErrorMapperRegistry stores error mappers for each provider.
type ErrorMapperRegistry struct {
	mu      sync.RWMutex
	mappers map[Provider]*ErrorMapper
}

var globalErrorMapperRegistry = NewErrorMapperRegistry()

// NewErrorMapperRegistry creates a new registry.
func NewErrorMapperRegistry() *ErrorMapperRegistry {
	r := &ErrorMapperRegistry{
		mappers: make(map[Provider]*ErrorMapper),
	}
	r.registerDefaults()
	return r
}

// DefaultErrorMapperRegistry returns the global error mapper registry.
func DefaultErrorMapperRegistry() *ErrorMapperRegistry {
	return globalErrorMapperRegistry
}

// Register adds or replaces an error mapper for a provider.
func (r *ErrorMapperRegistry) Register(mapper *ErrorMapper) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mappers[mapper.provider] = mapper
}

// Get retrieves the error mapper for a provider.
// Returns nil if no mapper is registered.
func (r *ErrorMapperRegistry) Get(provider Provider) *ErrorMapper {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.mappers[provider]
}

// MapError maps an API error using the appropriate provider mapper.
// Falls back to generic mapping if no provider-specific mapper exists.
func (r *ErrorMapperRegistry) MapError(provider Provider, statusCode int, errorCode, errorType, message string) error {
	r.mu.RLock()
	mapper := r.mappers[provider]
	r.mu.RUnlock()

	if mapper != nil {
		if err := mapper.Map(statusCode, errorCode, errorType, message); err != nil {
			return err
		}
	}

	// Fall back to generic status code mapping
	return mapGenericStatus(statusCode)
}

// mapGenericStatus provides fallback mapping for common HTTP status codes.
func mapGenericStatus(statusCode int) error {
	switch statusCode {
	case 400:
		return ErrInvalidParameters
	case 401:
		return ErrAuthenticationFailed
	case 403:
		return ErrPermissionDenied
	case 404:
		return ErrModelNotFound
	case 429:
		return ErrRateLimited
	case 500:
		return ErrServerError
	case 502, 503, 504:
		return ErrServiceUnavailable
	default:
		return nil
	}
}

// registerDefaults sets up error mappers for known providers.
func (r *ErrorMapperRegistry) registerDefaults() {
	// OpenAI error mappings
	openai := NewErrorMapper(ProviderOpenAI).
		AddStatusMapping(401, ErrAuthenticationFailed).
		AddStatusMapping(403, ErrPermissionDenied).
		AddStatusMapping(429, ErrRateLimited).
		AddStatusMapping(500, ErrServerError).
		AddStatusMapping(503, ErrServiceUnavailable).
		AddCodeMapping("context_length_exceeded", ErrContextLengthExceeded).
		AddCodeMapping("model_not_found", ErrModelNotFound).
		AddCodeMapping("invalid_api_key", ErrAuthenticationFailed).
		AddCodeMapping("rate_limit_exceeded", ErrRateLimited).
		AddCodeMapping("insufficient_quota", ErrQuotaExceeded).
		AddCodeMapping("content_policy_violation", ErrContentFiltered).
		AddTypeMapping("invalid_request_error", ErrInvalidParameters).
		AddTypeMapping("authentication_error", ErrAuthenticationFailed)
	r.mappers[ProviderOpenAI] = openai

	// Anthropic error mappings
	anthropic := NewErrorMapper(ProviderAnthropic).
		AddStatusMapping(401, ErrAuthenticationFailed).
		AddStatusMapping(403, ErrPermissionDenied).
		AddStatusMapping(429, ErrRateLimited).
		AddStatusMapping(500, ErrServerError).
		AddStatusMapping(529, ErrServiceUnavailable). // Anthropic overloaded
		AddTypeMapping("authentication_error", ErrAuthenticationFailed).
		AddTypeMapping("permission_error", ErrPermissionDenied).
		AddTypeMapping("rate_limit_error", ErrRateLimited).
		AddTypeMapping("overloaded_error", ErrServiceUnavailable).
		AddTypeMapping("invalid_request_error", ErrInvalidParameters).
		AddMessageMapping("context length", ErrContextLengthExceeded).
		AddMessageMapping("content policy", ErrContentFiltered)
	r.mappers[ProviderAnthropic] = anthropic

	// Gemini error mappings
	gemini := NewErrorMapper(ProviderGemini).
		AddStatusMapping(400, ErrInvalidParameters).
		AddStatusMapping(401, ErrAuthenticationFailed).
		AddStatusMapping(403, ErrPermissionDenied).
		AddStatusMapping(404, ErrModelNotFound).
		AddStatusMapping(429, ErrRateLimited).
		AddStatusMapping(500, ErrServerError).
		AddStatusMapping(503, ErrServiceUnavailable).
		AddMessageMapping("API key not valid", ErrAuthenticationFailed).
		AddMessageMapping("quota", ErrQuotaExceeded).
		AddMessageMapping("safety", ErrContentFiltered).
		AddMessageMapping("blocked", ErrContentFiltered)
	r.mappers[ProviderGemini] = gemini

	// Groq error mappings (OpenAI-compatible with some differences)
	groq := NewErrorMapper(ProviderGroq).
		AddStatusMapping(401, ErrAuthenticationFailed).
		AddStatusMapping(429, ErrRateLimited).
		AddStatusMapping(500, ErrServerError).
		AddStatusMapping(503, ErrServiceUnavailable).
		AddCodeMapping("rate_limit_exceeded", ErrRateLimited).
		AddCodeMapping("context_length_exceeded", ErrContextLengthExceeded)
	r.mappers[ProviderGroq] = groq

	// TogetherAI error mappings
	togetherai := NewErrorMapper(ProviderTogetherAI).
		AddStatusMapping(401, ErrAuthenticationFailed).
		AddStatusMapping(429, ErrRateLimited).
		AddStatusMapping(500, ErrServerError).
		AddStatusMapping(503, ErrServiceUnavailable).
		AddMessageMapping("rate limit", ErrRateLimited).
		AddMessageMapping("context length", ErrContextLengthExceeded)
	r.mappers[ProviderTogetherAI] = togetherai

	// DeepSeek error mappings
	deepseek := NewErrorMapper(ProviderDeepSeek).
		AddStatusMapping(401, ErrAuthenticationFailed).
		AddStatusMapping(429, ErrRateLimited).
		AddStatusMapping(500, ErrServerError).
		AddCodeMapping("context_length_exceeded", ErrContextLengthExceeded)
	r.mappers[ProviderDeepSeek] = deepseek

	// Mistral error mappings
	mistral := NewErrorMapper(ProviderMistral).
		AddStatusMapping(401, ErrAuthenticationFailed).
		AddStatusMapping(429, ErrRateLimited).
		AddStatusMapping(500, ErrServerError).
		AddCodeMapping("context_length_exceeded", ErrContextLengthExceeded)
	r.mappers[ProviderMistral] = mistral
}

// MapProviderError is a convenience function to map an error using the global registry.
func MapProviderError(provider Provider, statusCode int, errorCode, errorType, message string) error {
	return globalErrorMapperRegistry.MapError(provider, statusCode, errorCode, errorType, message)
}

// RegisterErrorMapper registers a custom error mapper in the global registry.
func RegisterErrorMapper(mapper *ErrorMapper) {
	globalErrorMapperRegistry.Register(mapper)
}
