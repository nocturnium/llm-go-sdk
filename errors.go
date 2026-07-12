package llms

import (
	"errors"
	"fmt"
	"time"
)

const (
	apiErrorCodeContentFilter          = "content_filter"
	apiErrorCodeContentPolicyViolation = "content_policy_violation"
	apiErrorCodeContextLengthExceeded  = "context_length_exceeded"
	apiErrorCodeInsufficientQuota      = "insufficient_quota"
	apiErrorCodeModelNotFound          = "model_not_found"
	apiErrorCodeQuotaExceeded          = "quota_exceeded"
	apiErrorCodeRateLimitExceeded      = "rate_limit_exceeded"
	apiErrorTypeAuthentication         = "authentication_error"
	apiErrorTypeInvalidRequest         = "invalid_request_error"
	apiErrorTypePermission             = "permission_error"
	apiErrorTypeRateLimit              = "rate_limit_error"
	apiErrorTypeServer                 = "server_error"
)

// Base errors for common failure scenarios.
var (
	// Configuration errors.
	ErrProviderNotSupported = errors.New("provider not supported")
	ErrMissingAPIKey        = errors.New("API key is required")
	ErrInvalidBaseURL       = errors.New("invalid base URL")

	// Parameter validation errors.
	ErrInvalidParameters = errors.New("invalid parameters")
	ErrEmptyMessages     = errors.New("messages cannot be empty")

	// API errors.
	ErrRateLimited           = errors.New("rate limited")
	ErrQuotaExceeded         = errors.New("quota exceeded")
	ErrAuthenticationFailed  = errors.New("authentication failed")
	ErrPermissionDenied      = errors.New("permission denied")
	ErrModelNotFound         = errors.New("model not found")
	ErrContextLengthExceeded = errors.New("context length exceeded")
	ErrContentFiltered       = errors.New("content filtered")
	ErrServerError           = errors.New("server error")
	ErrServiceUnavailable    = errors.New("service unavailable")

	// Streaming errors.
	ErrStreamInterrupted  = errors.New("stream interrupted")
	ErrIncompleteResponse = errors.New("incomplete response")
	ErrStreamTimeout      = errors.New("stream send timeout: consumer not reading")

	// Tool/function calling errors.
	ErrToolCallFailed      = errors.New("tool call failed")
	ErrInvalidToolResponse = errors.New("invalid tool response")
	ErrToolNotFound        = errors.New("tool not found")

	// Network errors.
	ErrTimeout          = errors.New("request timeout")
	ErrConnectionFailed = errors.New("connection failed")
)

// APIError represents an error from an LLM API with detailed information.
// This is the unified error type used across the SDK - both the core package
// and internal HTTP clients use this same type for consistency.
type APIError struct {
	// StatusCode is the HTTP status code
	StatusCode int

	// Message is the human-readable error message
	Message string

	// Type is the error type from the API (e.g., apiErrorTypeInvalidRequest)
	Type string

	// Code is the error code from the API (e.g., apiErrorCodeContextLengthExceeded)
	Code string

	// Param is the parameter that caused the error (if applicable)
	Param string

	// RequestID is the unique request identifier for debugging
	RequestID string

	// RetryAfter indicates when to retry (for rate limiting)
	RetryAfter time.Duration

	// Provider is the LLM provider that returned the error
	Provider Provider

	// RequestURL is the URL that was called (for debugging)
	RequestURL string

	// RequestMethod is the HTTP method used (for debugging)
	RequestMethod string
}

type apiStatusClassification struct {
	err       error
	retryable bool
}

var apiStatusClassifications = map[int]apiStatusClassification{
	401: {err: ErrAuthenticationFailed},
	403: {err: ErrPermissionDenied},
	404: {err: ErrModelNotFound},
	408: {err: ErrTimeout, retryable: true},
	429: {err: ErrRateLimited, retryable: true},
	500: {err: ErrServerError, retryable: true},
	502: {err: ErrServiceUnavailable, retryable: true},
	503: {err: ErrServiceUnavailable, retryable: true},
	504: {err: ErrServiceUnavailable, retryable: true},
	529: {err: ErrServiceUnavailable, retryable: true},
}

// Error implements the error interface.
// The error message includes status code, type, code, message, and optionally
// request context (URL, method) and request ID for debugging.
func (e *APIError) Error() string {
	var result string

	// Build base error message
	result = fmt.Sprintf("API error (status %d", e.StatusCode)
	if e.Type != "" {
		result += ", type " + e.Type
	}
	if e.Code != "" {
		result += ", code " + e.Code
	}
	result += "): " + e.Message

	// Add request context if available
	if e.RequestMethod != "" || e.RequestURL != "" {
		result += " ["
		if e.RequestMethod != "" {
			result += e.RequestMethod + " "
		}
		if e.RequestURL != "" {
			result += e.RequestURL
		}
		result += "]"
	}

	// Add request ID if available
	if e.RequestID != "" {
		result += " (request_id: " + e.RequestID + ")"
	}

	return result
}

// Unwrap returns the underlying error based on status code
func (e *APIError) Unwrap() error {
	return e.underlyingError()
}

// Is reports whether the error matches a target error
func (e *APIError) Is(target error) bool {
	return errors.Is(e.underlyingError(), target)
}

// underlyingError maps status codes and error types to sentinel errors
func (e *APIError) underlyingError() error {
	// Quota exhaustion is a permanent billing failure even though providers return
	// it with a 429 status. Classify it by code before the status map — otherwise
	// it masquerades as a retryable rate limit and the exported ErrQuotaExceeded
	// sentinel is unreachable.
	switch e.Code {
	case apiErrorCodeQuotaExceeded, apiErrorCodeInsufficientQuota:
		return ErrQuotaExceeded
	}

	// Check by status code first
	if classification, ok := apiStatusClassifications[e.StatusCode]; ok {
		return classification.err
	}

	// Check by error code
	switch e.Code {
	case apiErrorCodeContextLengthExceeded:
		return ErrContextLengthExceeded
	case apiErrorCodeModelNotFound:
		return ErrModelNotFound
	case apiErrorCodeContentFilter, apiErrorCodeContentPolicyViolation:
		return ErrContentFiltered
	case apiErrorCodeRateLimitExceeded:
		return ErrRateLimited
		// Quota codes are handled by the precedence switch at the top of this
		// method (they must win over the 429 status classification).
	}

	// Check by error type
	switch e.Type {
	case apiErrorTypeAuthentication:
		return ErrAuthenticationFailed
	case apiErrorTypePermission:
		return ErrPermissionDenied
	case apiErrorTypeRateLimit:
		return ErrRateLimited
	case apiErrorTypeInvalidRequest:
		return ErrInvalidParameters
	case apiErrorTypeServer:
		return ErrServerError
	}

	return nil
}

// IsRetryable returns true if the error is likely transient and the request can be retried
func (e *APIError) IsRetryable() bool {
	// Quota exhaustion is a permanent billing failure regardless of the (often
	// 429) status code; never report it as retryable.
	switch e.Code {
	case apiErrorCodeQuotaExceeded, apiErrorCodeInsufficientQuota:
		return false
	}
	if classification, ok := apiStatusClassifications[e.StatusCode]; ok {
		return classification.retryable
	}
	// StatusCode is absent or unrecognized (e.g. a streaming error carrying only
	// Type/Code with StatusCode 0). Fall back to the sentinel classification so a
	// mid-stream rate limit, server error, or timeout is still retryable.
	underlying := e.underlyingError()
	return errors.Is(underlying, ErrRateLimited) ||
		errors.Is(underlying, ErrServerError) ||
		errors.Is(underlying, ErrServiceUnavailable) ||
		errors.Is(underlying, ErrTimeout)
}

// ValidationError represents a parameter validation error
type ValidationError struct {
	Field   string
	Value   any
	Message string
}

// Error implements the error interface.
func (e ValidationError) Error() string {
	return fmt.Sprintf("validation error for %s: %s (got %v)", e.Field, e.Message, e.Value)
}

// Is reports whether the error matches ErrInvalidParameters
func (e ValidationError) Is(target error) bool {
	return errors.Is(ErrInvalidParameters, target)
}

// ValidationErrors is a collection of validation errors
type ValidationErrors []ValidationError

// Error implements the error interface.
func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return "no validation errors"
	}
	if len(e) == 1 {
		return e[0].Error()
	}

	// Show all errors up to a reasonable limit
	const maxDisplay = 5
	var result string
	if len(e) <= maxDisplay {
		result = fmt.Sprintf("%d validation errors:\n", len(e))
		for i, err := range e {
			result += fmt.Sprintf("  %d. %s\n", i+1, err.Error())
		}
	} else {
		result = fmt.Sprintf("%d validation errors (showing first %d):\n", len(e), maxDisplay)
		for i := 0; i < maxDisplay; i++ {
			result += fmt.Sprintf("  %d. %s\n", i+1, e[i].Error())
		}
		result += fmt.Sprintf("  ... and %d more errors\n", len(e)-maxDisplay)
	}
	return result
}

// Is reports whether any contained error matches the target
func (e ValidationErrors) Is(target error) bool {
	for _, err := range e {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// Unwrap returns ErrInvalidParameters for errors.Is compatibility
func (e ValidationErrors) Unwrap() error {
	return ErrInvalidParameters
}

// StreamError represents an error that occurred during streaming
type StreamError struct {
	Cause       error
	BytesRead   int64
	ChunksRead  int
	LastContent string
}

// Error implements the error interface.
func (e *StreamError) Error() string {
	return fmt.Sprintf("stream error after %d chunks (%d bytes): %v", e.ChunksRead, e.BytesRead, e.Cause)
}

// Unwrap returns the underlying cause
func (e *StreamError) Unwrap() error {
	return e.Cause
}

// Is reports whether the error matches common stream errors
func (e *StreamError) Is(target error) bool {
	if errors.Is(ErrStreamInterrupted, target) {
		return true
	}
	return errors.Is(e.Cause, target)
}

// ProviderError wraps an error with provider context for consistent error handling
// across all LLM providers. This is useful for wrapping network errors, parsing
// errors, and other non-API errors that don't have a status code.
//
// Example usage in providers:
//
//	if err != nil {
//	    return nil, llms.WrapProviderError(llms.ProviderOpenAI, "failed to create message", err)
//	}
type ProviderError struct {
	// Provider is the LLM provider that caused the error
	Provider Provider

	// Operation describes what operation was being performed
	Operation string

	// Err is the underlying error
	Err error
}

// Error implements the error interface.
func (e *ProviderError) Error() string {
	if e.Operation != "" {
		return fmt.Sprintf("%s: %s: %v", e.Provider, e.Operation, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Provider, e.Err)
}

// Unwrap returns the underlying error for errors.Is/As compatibility
func (e *ProviderError) Unwrap() error {
	return e.Err
}

// WrapProviderError creates a new ProviderError wrapping the given error.
// Returns nil if err is nil.
func WrapProviderError(provider Provider, operation string, err error) error {
	if err == nil {
		return nil
	}
	return &ProviderError{
		Provider:  provider,
		Operation: operation,
		Err:       err,
	}
}

// ProviderFromError extracts the provider from an error if it's a ProviderError or APIError.
// Returns empty string if the error doesn't contain provider information.
func ProviderFromError(err error) Provider {
	var provErr *ProviderError
	if errors.As(err, &provErr) {
		return provErr.Provider
	}

	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Provider
	}

	return ""
}

// IsTemporary returns true if the error is likely transient and can be retried.
// This includes rate limits, server errors, and network issues.
func IsTemporary(err error) bool {
	if err == nil {
		return false
	}

	// Check APIError
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.IsRetryable()
	}

	// Check common transient errors
	if errors.Is(err, ErrRateLimited) ||
		errors.Is(err, ErrServerError) ||
		errors.Is(err, ErrServiceUnavailable) ||
		errors.Is(err, ErrTimeout) ||
		errors.Is(err, ErrConnectionFailed) ||
		errors.Is(err, ErrStreamInterrupted) {
		return true
	}

	return false
}

// ErrorDetails contains extracted information from an error for logging/debugging
type ErrorDetails struct {
	// Provider that caused the error (if known)
	Provider Provider

	// StatusCode from API response (0 if not an API error)
	StatusCode int

	// IsRetryable indicates if the error might succeed on retry
	IsRetryable bool

	// RetryAfter suggests when to retry (zero if not applicable)
	RetryAfter time.Duration

	// RequestID for correlating with provider logs
	RequestID string

	// RootCause is the innermost unwrapped error
	RootCause error
}

// GetErrorDetails extracts structured information from an error.
// Useful for logging, metrics, and debugging.
func GetErrorDetails(err error) ErrorDetails {
	if err == nil {
		return ErrorDetails{}
	}

	details := ErrorDetails{
		IsRetryable: IsTemporary(err),
		RootCause:   errors.Unwrap(err),
	}

	// If no wrapped error, root cause is the error itself
	if details.RootCause == nil {
		details.RootCause = err
	} else {
		// Keep unwrapping to find the root
		for {
			next := errors.Unwrap(details.RootCause)
			if next == nil {
				break
			}
			details.RootCause = next
		}
	}

	// Extract provider
	details.Provider = ProviderFromError(err)

	// Extract API error details
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		details.StatusCode = apiErr.StatusCode
		details.RetryAfter = apiErr.RetryAfter
		details.RequestID = apiErr.RequestID
	}

	return details
}

// FormatError returns a consistent, human-readable error message.
// For simple errors, returns the error message.
// For complex errors, formats with context like provider and status code.
func FormatError(err error) string {
	if err == nil {
		return ""
	}

	// Check for APIError - use its formatted message
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Error()
	}

	// Check for ProviderError
	var provErr *ProviderError
	if errors.As(err, &provErr) {
		return provErr.Error()
	}

	// Check for ValidationErrors
	var valErrs ValidationErrors
	if errors.As(err, &valErrs) {
		return valErrs.Error()
	}

	// Check for StreamError
	var streamErr *StreamError
	if errors.As(err, &streamErr) {
		return streamErr.Error()
	}

	// Default: return the error message
	return err.Error()
}

// HTTPAPIError provides fields for creating an APIError from HTTP client errors.
// This is used by internal packages to convert their error types to the unified APIError.
type HTTPAPIError struct {
	StatusCode    int
	Message       string
	Type          string
	Code          string
	Param         string
	RequestID     string
	RetryAfter    time.Duration
	RequestURL    string
	RequestMethod string
}

// NewAPIErrorFromHTTP creates an APIError from HTTP client error fields.
// This is the recommended way for internal packages to create APIErrors,
// ensuring all fields are properly populated.
func NewAPIErrorFromHTTP(provider Provider, httpErr HTTPAPIError) *APIError {
	return &APIError{
		StatusCode:    httpErr.StatusCode,
		Message:       httpErr.Message,
		Type:          httpErr.Type,
		Code:          httpErr.Code,
		Param:         httpErr.Param,
		RequestID:     httpErr.RequestID,
		RetryAfter:    httpErr.RetryAfter,
		Provider:      provider,
		RequestURL:    httpErr.RequestURL,
		RequestMethod: httpErr.RequestMethod,
	}
}

// WrapHTTPError wraps an error that may be an HTTP API error, converting it
// to a properly typed APIError if possible. If the error is already an APIError,
// it adds the provider field if missing. For other errors, it wraps them in a ProviderError.
func WrapHTTPError(provider Provider, operation string, err error) error {
	if err == nil {
		return nil
	}

	// Check if it's already an APIError and add provider if missing
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if apiErr.Provider == "" {
			apiErr.Provider = provider
		}
		return apiErr
	}

	// For other errors, wrap with provider context
	return WrapProviderError(provider, operation, err)
}
