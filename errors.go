package llms

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
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
	// ErrClientNotConfigured reports a provider whose HTTP client was never
	// attached (a nil *Client passed to openaicompat.NewBaseProvider).
	ErrClientNotConfigured = errors.New("HTTP client not configured")

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
	402: {err: ErrPlanRequired},
	403: {err: ErrPermissionDenied},
	// 404 is mapped to ErrModelNotFound because the model path is the common
	// case, but a mistyped base URL, a deleted batch job or a missing file
	// reach it too; callers matching on it should confirm with the message.
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
	if e == nil {
		return ""
	}
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
			result += sanitizeErrorURL(e.RequestURL)
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
	if e == nil {
		return nil
	}
	return e.underlyingError()
}

// Is reports whether the error matches a target error
func (e *APIError) Is(target error) bool {
	underlying := e.underlyingError()
	if underlying == nil || target == nil {
		return false
	}
	return errors.Is(underlying, target)
}

// underlyingError maps status codes and error types to sentinel errors
func (e *APIError) underlyingError() error {
	if e == nil {
		return nil
	}
	// Quota exhaustion is a permanent billing failure even though providers return
	// it with a 429 status. Classify it by code before the status map, otherwise
	// it masquerades as a retryable rate limit and the exported ErrQuotaExceeded
	// sentinel is unreachable.
	switch e.Code {
	case apiErrorCodeQuotaExceeded, apiErrorCodeInsufficientQuota:
		return ErrQuotaExceeded
	}

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
	if e == nil {
		return false
	}
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
	return target == ErrInvalidParameters
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
	// An empty collection carries no validation failure, so it must not match
	// ErrInvalidParameters through errors.Is.
	if len(e) == 0 {
		return nil
	}
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
	if e == nil {
		return ""
	}
	return fmt.Sprintf("stream error after %d chunks (%d bytes): %v", e.ChunksRead, e.BytesRead, e.Cause)
}

// Unwrap returns the underlying cause
func (e *StreamError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Is reports whether the error matches common stream errors.
//
// A StreamError matches ErrStreamInterrupted, which is what it is, and
// whatever its cause matches. It deliberately does not claim to be every
// target: the old form asked whether ErrStreamInterrupted matched the target,
// which is true for any target, so a StreamError wrapping context.Canceled
// answered yes to everything and IsTemporary told callers to retry a stream
// the user had canceled.
func (e *StreamError) Is(target error) bool {
	if e == nil {
		return false
	}
	if target == ErrStreamInterrupted {
		return true
	}
	return errors.Is(e.Cause, target)
}

// sanitizeErrorURL strips the parts of a URL that carry credentials before it
// reaches an error string. Providers put API keys in the query (Gemini) and
// userinfo survives a misconfigured base URL, and an error string is the one
// value callers are certain to log.
func sanitizeErrorURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		// The URL is malformed, which is how a misconfigured base URL arrives,
		// so cut by hand: everything from the first '?' goes, and so does any
		// userinfo ahead of the host.
		trimmed := rawURL
		scheme := ""
		if idx := strings.Index(trimmed, "://"); idx >= 0 {
			scheme, trimmed = trimmed[:idx+3], trimmed[idx+3:]
		}
		// Credentials can contain reserved characters that make url.Parse fail.
		// The last '@' before the first '?' or '#' is the userinfo terminator; a
		// path may contain '@' after the authority, but keeping it is safer than
		// leaving any head of a credential behind. Strip query and fragment from
		// the remainder so keys in either do not leak.
		candidate := trimmed
		if at := strings.LastIndex(candidate, "@"); at >= 0 {
			candidate = candidate[at+1:]
		}
		if idx := strings.IndexAny(candidate, "?#"); idx >= 0 {
			candidate = candidate[:idx]
		}
		return scheme + candidate
	}
	u.User = nil
	// Malformed userinfo containing a reserved '?' or '#' before the '@' is
	// parsed into Host rather than User (url treats the text before the last
	// reserved character as the host). Clear the credential material before the
	// last '@' in the authority as well as in Path/Opaque.
	if at := strings.LastIndex(u.Host, "@"); at >= 0 {
		u.Host = u.Host[at+1:]
	} else if strings.ContainsAny(u.Host, "?#") {
		// A lone '?' or '#' in the host without an '@' is credential material
		// that url.Parse could not place in User. Nothing host-like precedes it.
		u.Host = ""
	}
	// A reserved character in a password (for example a slash) makes
	// url.Parse treat the credential as part of the path while leaving the
	// trailing "@host" in Host. If the path contains an '@', drop everything
	// through it so the head of a split credential cannot survive.
	if at := strings.Index(u.Path, "@"); at >= 0 {
		// url.Parse can put the credential itself ("user:1234") in Host when
		// the malformed password contains a slash, leaving the terminal @host in
		// Path. Drop that host too; it is userinfo, not an address.
		if strings.Contains(u.Host, ":") {
			u.Host = ""
		}
		u.Path = u.Path[at+1:]
	}
	if at := strings.Index(u.RawPath, "@"); at >= 0 {
		u.RawPath = u.RawPath[at+1:]
	}
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	// An opaque URL ("https:KEY@host/v1", which a misconfigured base URL can
	// produce) keeps its credential in Opaque rather than in User.
	if u.Opaque != "" {
		if idx := strings.LastIndex(u.Opaque, "@"); idx >= 0 {
			u.Opaque = u.Opaque[idx+1:]
		}
	}
	return u.String()
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
	if e == nil {
		return ""
	}
	if e.Operation != "" {
		return fmt.Sprintf("%s: %s: %v", e.Provider, e.Operation, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Provider, e.Err)
}

// Unwrap returns the underlying error for errors.Is/As compatibility
func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
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
	// Walk the whole error tree rather than using errors.As once: an outer
	// ProviderError may carry an empty Provider when the caller only wanted to
	// attach an operation, while an inner wrapper, a sibling branch of a
	// multi-wrapped error, or an APIError still names the provider that failed.
	if p := providerInTree(err); p != "" {
		return p
	}
	// The unwrap chain does not cover matches behind a custom As method, so
	// fall back to errors.As for both types.
	var provErr *ProviderError
	if errors.As(err, &provErr) && provErr != nil && provErr.Provider != "" {
		return provErr.Provider
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr != nil && apiErr.Provider != "" {
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

	// A canceled or expired context is the caller's decision, not a transient
	// provider fault: retrying it wastes a call and ignores the deadline. Check
	// this before APIError classification because an error can carry both a
	// wrapped context error and a retryable-looking APIError.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Check APIError
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr != nil {
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
		RootCause:   rootCause(err),
	}

	details.Provider = ProviderFromError(err)

	// Extract API error details
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr != nil {
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
	if errors.As(err, &apiErr) && apiErr != nil {
		return apiErr.Error()
	}

	// Check for ProviderError
	var provErr *ProviderError
	if errors.As(err, &provErr) && provErr != nil {
		return provErr.Error()
	}

	// Check for ValidationErrors
	var valErrs ValidationErrors
	if errors.As(err, &valErrs) {
		return valErrs.Error()
	}

	// Check for StreamError
	var streamErr *StreamError
	if errors.As(err, &streamErr) && streamErr != nil {
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
	if errors.As(err, &apiErr) && apiErr != nil {
		if apiErr.Provider == "" {
			apiErr.Provider = provider
		}
		// A caller may wrap a cancellation alongside the API error
		// (fmt.Errorf("%w: %w", context.Canceled, apiErr)). Returning the bare
		// APIError would drop the cancellation, so retry classification sees a
		// retryable status and retries work whose context is already done.
		if ctxErr := firstContextError(err); ctxErr != nil && !errors.Is(apiErr, ctxErr) {
			return fmt.Errorf("%w: %w", ctxErr, apiErr)
		}
		return apiErr
	}

	// For other errors, wrap with provider context
	return WrapProviderError(provider, operation, err)
}

// firstContextError returns the first context cancellation sentinel err matches,
// or nil. It distinguishes an outer cancellation from the API error itself so a
// genuine timeout survives wrapping.
func firstContextError(err error) error {
	for _, target := range []error{context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, target) {
			return target
		}
	}
	return nil
}

// ErrPlanRequired indicates a paid plan is required (HTTP 402).
var ErrPlanRequired = errors.New("paid plan required")

// ErrAssetExpired indicates a hosted media asset's download URL has expired.
var ErrAssetExpired = errors.New("media asset expired")

// ModerationStage identifies where content was filtered.
type ModerationStage string

const (
	// ModerationInput identifies filtering of submitted content.
	ModerationInput ModerationStage = "input"
	// ModerationOutput identifies filtering of generated content.
	ModerationOutput ModerationStage = "output"
)

// ModerationError describes content filtering and whether a charge was incurred.
type ModerationError struct {
	// Stage identifies whether input or output was filtered.
	Stage ModerationStage
	// Reasons contains provider-supplied explanations.
	Reasons []string
	// Charged reports whether the provider charged for this request.
	Charged bool
	// Provider identifies the provider that filtered the content.
	Provider string
}

// Error describes the filtering stage, provider, and reasons.
func (e *ModerationError) Error() string {
	return fmt.Sprintf("%s: content filtered at %s (charged=%t): %v", e.Provider, e.Stage, e.Charged, e.Reasons)
}

// Unwrap returns ErrContentFiltered for errors.Is compatibility.
func (e *ModerationError) Unwrap() error { return ErrContentFiltered }

// rootCause follows Unwrap chains to the innermost error. For an error that
// wraps several (fmt.Errorf with multiple %w, errors.Join) it follows the first
// non-nil child, so the %w: %w wrapper WrapHTTPError builds for a canceled
// request still reports its cause rather than the wrapper itself.
func rootCause(err error) error {
	for {
		var next error
		switch u := err.(type) { //nolint:errorlint // walks one unwrap level at a time; errors.As would skip levels
		case interface{ Unwrap() error }:
			next = u.Unwrap()
		case interface{ Unwrap() []error }:
			for _, child := range u.Unwrap() {
				if child != nil {
					next = child
					break
				}
			}
		}
		if next == nil {
			return err
		}
		err = next
	}
}

// providerInTree returns the first non-empty provider found depth-first in
// err's wrap tree, following both Unwrap() error and Unwrap() []error.
func providerInTree(err error) Provider {
	for err != nil {
		switch e := err.(type) { //nolint:errorlint // inspects this node before walking its children below
		case *ProviderError:
			if e == nil {
				return ""
			}
			if e.Provider != "" {
				return e.Provider
			}
		case *APIError:
			if e == nil {
				return ""
			}
			if e.Provider != "" {
				return e.Provider
			}
		}
		switch u := err.(type) { //nolint:errorlint // walks one unwrap level at a time; errors.As would skip levels
		case interface{ Unwrap() error }:
			err = u.Unwrap()
		case interface{ Unwrap() []error }:
			for _, child := range u.Unwrap() {
				if p := providerInTree(child); p != "" {
					return p
				}
			}
			return ""
		default:
			return ""
		}
	}
	return ""
}
