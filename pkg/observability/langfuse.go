package observability

import (
	"context"
	"regexp"
	"time"
)

// Langfuse-compatible context key type
type langfuseContextKey string

const (
	traceContextKey langfuseContextKey = "langfuse_trace_context"

	// MaxMetadataValueLength is the maximum length for propagated metadata values (Langfuse constraint)
	MaxMetadataValueLength = 200
)

// alphanumericRegex validates that metadata keys are alphanumeric only (Langfuse constraint)
var alphanumericRegex = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

// TraceContext holds Langfuse-compatible trace information that can be propagated
// across the call hierarchy. This mirrors Langfuse's trace attributes.
type TraceContext struct {
	// Trace identity
	TraceID  string `json:"trace_id,omitempty"`
	SpanID   string `json:"span_id,omitempty"`
	ParentID string `json:"parent_id,omitempty"`

	// Langfuse-specific attributes
	UserID      string   `json:"user_id,omitempty"`
	SessionID   string   `json:"session_id,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Version     string   `json:"version,omitempty"`
	Release     string   `json:"release,omitempty"`
	Environment string   `json:"environment,omitempty"`

	// Propagated metadata (alphanumeric keys, max 200 char values per Langfuse constraints)
	Metadata map[string]string `json:"metadata,omitempty"`
}

// GenerationMetadata holds LLM-specific metadata for Langfuse generations.
// This captures all the information Langfuse expects for an LLM generation observation.
type GenerationMetadata struct {
	// Model info
	RequestModel  string `json:"request_model,omitempty"`
	ResponseModel string `json:"response_model,omitempty"`

	// Input/Output (stored as JSON strings for Langfuse compatibility)
	Input  string `json:"input,omitempty"`
	Output string `json:"output,omitempty"`

	// Request parameters
	Temperature      *float64 `json:"temperature,omitempty"`
	MaxTokens        *int     `json:"max_tokens,omitempty"`
	TopP             *float64 `json:"top_p,omitempty"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`

	// Usage
	PromptTokens     int     `json:"prompt_tokens,omitempty"`
	CompletionTokens int     `json:"completion_tokens,omitempty"`
	TotalTokens      int     `json:"total_tokens,omitempty"`
	CostUSD          float64 `json:"cost_usd,omitempty"`

	// Timing
	StartTime        time.Time     `json:"start_time,omitempty"`
	EndTime          time.Time     `json:"end_time,omitempty"`
	Duration         time.Duration `json:"duration,omitempty"`
	TimeToFirstToken time.Duration `json:"time_to_first_token,omitempty"`

	// Status
	FinishReason string `json:"finish_reason,omitempty"`
	Error        string `json:"error,omitempty"`
	StatusCode   int    `json:"status_code,omitempty"`
}

// WithTraceContext attaches a TraceContext to the given context.
// The trace context will be available to all downstream calls.
func WithTraceContext(ctx context.Context, tc *TraceContext) context.Context {
	if tc == nil {
		return ctx
	}
	return context.WithValue(ctx, traceContextKey, tc)
}

// GetTraceContext retrieves the TraceContext from the given context.
// Returns nil if no trace context is set.
func GetTraceContext(ctx context.Context) *TraceContext {
	if tc, ok := ctx.Value(traceContextKey).(*TraceContext); ok {
		return tc
	}
	return nil
}

// TraceContextOption is a functional option for configuring TraceContext
type TraceContextOption func(*TraceContext)

// PropagateAttributes creates a child context with inherited trace attributes.
// This mirrors Langfuse's propagate_attributes() behavior, automatically
// propagating user_id, session_id, tags, version, and metadata to child contexts.
func PropagateAttributes(ctx context.Context, overrides ...TraceContextOption) context.Context {
	parent := GetTraceContext(ctx)
	child := &TraceContext{}

	// Copy from parent if exists
	if parent != nil {
		child.UserID = parent.UserID
		child.SessionID = parent.SessionID
		child.Version = parent.Version
		child.Release = parent.Release
		child.Environment = parent.Environment

		// Deep copy tags
		if len(parent.Tags) > 0 {
			child.Tags = make([]string, len(parent.Tags))
			copy(child.Tags, parent.Tags)
		}

		// Deep copy metadata
		if len(parent.Metadata) > 0 {
			child.Metadata = make(map[string]string, len(parent.Metadata))
			for k, v := range parent.Metadata {
				child.Metadata[k] = v
			}
		}
	}

	// Apply overrides
	for _, opt := range overrides {
		opt(child)
	}

	return WithTraceContext(ctx, child)
}

// WithUserID sets the user ID in the trace context
func WithUserID(id string) TraceContextOption {
	return func(tc *TraceContext) {
		tc.UserID = id
	}
}

// WithSessionID sets the session ID in the trace context
func WithSessionID(id string) TraceContextOption {
	return func(tc *TraceContext) {
		tc.SessionID = id
	}
}

// WithTags appends tags to the trace context
func WithTags(tags ...string) TraceContextOption {
	return func(tc *TraceContext) {
		tc.Tags = append(tc.Tags, tags...)
	}
}

// WithVersion sets the version in the trace context
func WithVersion(version string) TraceContextOption {
	return func(tc *TraceContext) {
		tc.Version = version
	}
}

// WithRelease sets the release in the trace context
func WithRelease(release string) TraceContextOption {
	return func(tc *TraceContext) {
		tc.Release = release
	}
}

// WithEnvironment sets the environment in the trace context
func WithEnvironment(env string) TraceContextOption {
	return func(tc *TraceContext) {
		tc.Environment = env
	}
}

// WithMetadata adds a key-value pair to the propagated metadata.
// Keys must be alphanumeric only, and values are limited to 200 characters
// per Langfuse constraints. Invalid entries are silently ignored.
func WithMetadata(key, value string) TraceContextOption {
	return func(tc *TraceContext) {
		if !IsValidMetadataKey(key) {
			return
		}
		if len(value) > MaxMetadataValueLength {
			return
		}
		if tc.Metadata == nil {
			tc.Metadata = make(map[string]string)
		}
		tc.Metadata[key] = value
	}
}

// WithMetadataMap adds multiple key-value pairs to the propagated metadata.
// Keys must be alphanumeric only, and values are limited to 200 characters.
// Invalid entries are silently ignored.
func WithMetadataMap(metadata map[string]string) TraceContextOption {
	return func(tc *TraceContext) {
		if tc.Metadata == nil {
			tc.Metadata = make(map[string]string)
		}
		for k, v := range metadata {
			if IsValidMetadataKey(k) && len(v) <= MaxMetadataValueLength {
				tc.Metadata[k] = v
			}
		}
	}
}

// IsValidMetadataKey checks if a key is valid for Langfuse propagated metadata.
// Keys must be alphanumeric only per Langfuse constraints.
func IsValidMetadataKey(key string) bool {
	return key != "" && alphanumericRegex.MatchString(key)
}

// ValidateMetadataValue checks if a value is valid for Langfuse propagated metadata.
// Values must be at most 200 characters per Langfuse constraints.
func ValidateMetadataValue(value string) bool {
	return len(value) <= MaxMetadataValueLength
}

// NewTraceContext creates a new TraceContext with the given options
func NewTraceContext(opts ...TraceContextOption) *TraceContext {
	tc := &TraceContext{}
	for _, opt := range opts {
		opt(tc)
	}
	return tc
}

// Clone creates a deep copy of the TraceContext
func (tc *TraceContext) Clone() *TraceContext {
	if tc == nil {
		return nil
	}

	clone := &TraceContext{
		TraceID:     tc.TraceID,
		SpanID:      tc.SpanID,
		ParentID:    tc.ParentID,
		UserID:      tc.UserID,
		SessionID:   tc.SessionID,
		Version:     tc.Version,
		Release:     tc.Release,
		Environment: tc.Environment,
	}

	if len(tc.Tags) > 0 {
		clone.Tags = make([]string, len(tc.Tags))
		copy(clone.Tags, tc.Tags)
	}

	if len(tc.Metadata) > 0 {
		clone.Metadata = make(map[string]string, len(tc.Metadata))
		for k, v := range tc.Metadata {
			clone.Metadata[k] = v
		}
	}

	return clone
}
