package llms

import (
	"encoding/json"
	"fmt"
	"time"
)

const toolTypeFunction = string(ToolTypeFunction)

// ResponseFormatType describes a structured output response format.
type ResponseFormatType string

const (
	// ResponseFormatText requests plain text output.
	ResponseFormatText ResponseFormatType = "text"
	// ResponseFormatJSONObject requests JSON object output without a schema.
	ResponseFormatJSONObject ResponseFormatType = "json_object"
	// ResponseFormatJSONSchema requests JSON output constrained by a JSON Schema.
	ResponseFormatJSONSchema ResponseFormatType = "json_schema"
)

// JSONSchema describes a named JSON Schema response format.
type JSONSchema struct {
	Name        string
	Description string
	Schema      json.RawMessage
	Strict      bool
}

// ResponseFormat describes the requested response format for a generation call.
type ResponseFormat struct {
	Type       ResponseFormatType
	JSONSchema *JSONSchema
}

// CallOption is a function that modifies call options
type CallOption func(*CallOptions)

// TraceOptions contains per-call trace context overrides.
type TraceOptions struct {
	TraceID   string
	SpanID    string
	ParentID  string
	UserID    string
	SessionID string
	Tags      []string
	Metadata  map[string]any
	Version   string
}

// CallOptions contains options for LLM calls
type CallOptions struct {
	Model string // Override the default model
	// Temperature is the sampling temperature. A nil pointer means "not set"
	// (the provider/model applies its own default); a non-nil pointer sends the
	// value verbatim, including an explicit 0.0.
	Temperature *float64
	// MaxTokens is the output token limit. A nil pointer means "not set" (the
	// provider/model applies its own default); a non-nil pointer sends the value
	// verbatim, including an explicit 0.
	MaxTokens *int
	// TopP is the nucleus-sampling probability mass. A nil pointer means "not set"
	// (the provider/model applies its own default); a non-nil pointer sends the
	// value verbatim, including an explicit 0.0.
	TopP *float64
	// FrequencyPenalty penalizes repeated tokens. A nil pointer means "not set"
	// (the provider/model applies its own default); a non-nil pointer sends the
	// value verbatim, including an explicit 0.0.
	FrequencyPenalty *float64
	// PresencePenalty penalizes tokens that already appeared. A nil pointer means
	// "not set" (the provider/model applies its own default); a non-nil pointer
	// sends the value verbatim, including an explicit 0.0.
	PresencePenalty *float64
	StopWords       []string
	Tools           []Tool
	ToolChoice      *ToolChoice
	ResponseFormat  *ResponseFormat

	// Reasoning requests model reasoning ("thinking") for this call. Nil uses the
	// model's default behavior. See WithReasoning / WithReasoningEffort.
	Reasoning *ReasoningConfig

	// Cache controls the SDK's automatic prompt caching for this call. Nil uses
	// the provider default. See WithCache / WithCacheTTL / WithoutCache.
	Cache *CacheConfig

	// Message preprocessing options
	DisableMessageMerging bool // If true, consecutive same-role messages won't be merged (default: false, merging enabled)

	// Token estimation options
	EstimateTokens bool // If true, estimate token counts when provider doesn't return them (default: false)

	// Streaming options
	StreamBufferSize  int           // Size of the stream channel buffer (default: 100)
	StreamSendTimeout time.Duration // Timeout for sending to stream channel (default: 30s, 0=use default)

	// Trace contains per-call trace context overrides.
	Trace *TraceOptions

	// Provider-specific extensions (e.g., LoRAX adapter_id)
	// These parameters are merged into the request body at the top level
	ExtraBody map[string]any

	// WebSearch configures web search grounding
	WebSearch *WebSearchConfig
}

// DefaultCallOptions returns the default call options
func DefaultCallOptions() *CallOptions {
	// Temperature and TopP are intentionally left nil so the provider/model can
	// apply its own default. Forcing a value here would prevent callers from
	// distinguishing "use the default" from an explicit choice (e.g. 0.0).
	return &CallOptions{
		StreamBufferSize:  100,
		StreamSendTimeout: DefaultStreamSendTimeout,
	}
}

// WithTemperature sets the temperature for generation.
// The value is stored via a pointer so an explicit 0.0 is preserved and sent to
// the provider (rather than being treated as "unset").
func WithTemperature(temp float64) CallOption {
	return func(o *CallOptions) {
		o.Temperature = &temp
	}
}

// WithMaxTokens sets the maximum tokens for generation
func WithMaxTokens(tokens int) CallOption {
	return func(o *CallOptions) {
		o.MaxTokens = &tokens
	}
}

// WithTopP sets the top_p value for generation.
// The value is stored via a pointer so an explicit 0.0 is preserved and sent to
// the provider (rather than being treated as "unset").
func WithTopP(topP float64) CallOption {
	return func(o *CallOptions) {
		o.TopP = &topP
	}
}

// WithFrequencyPenalty sets the frequency penalty
func WithFrequencyPenalty(penalty float64) CallOption {
	return func(o *CallOptions) {
		o.FrequencyPenalty = &penalty
	}
}

// WithPresencePenalty sets the presence penalty
func WithPresencePenalty(penalty float64) CallOption {
	return func(o *CallOptions) {
		o.PresencePenalty = &penalty
	}
}

// WithStopWords sets the stop words
func WithStopWords(words []string) CallOption {
	return func(o *CallOptions) {
		o.StopWords = words
	}
}

// WithTools sets the tools available for the model to call
func WithTools(tools []Tool) CallOption {
	return func(o *CallOptions) {
		o.Tools = tools
	}
}

// WithToolChoiceAuto lets the model decide whether to call tools.
func WithToolChoiceAuto() CallOption {
	return func(o *CallOptions) {
		o.ToolChoice = &ToolChoice{Mode: ToolChoiceAuto}
	}
}

// WithToolChoiceNone prevents the model from calling tools.
func WithToolChoiceNone() CallOption {
	return func(o *CallOptions) {
		o.ToolChoice = &ToolChoice{Mode: ToolChoiceNone}
	}
}

// WithToolChoiceRequired forces the model to call a tool.
func WithToolChoiceRequired() CallOption {
	return func(o *CallOptions) {
		o.ToolChoice = &ToolChoice{Mode: ToolChoiceRequired}
	}
}

// WithToolChoiceTool forces the model to call the named tool.
func WithToolChoiceTool(name string) CallOption {
	return func(o *CallOptions) {
		o.ToolChoice = &ToolChoice{Mode: ToolChoiceTool, Tool: name}
	}
}

// WithJSONMode enables JSON mode for structured output
func WithJSONMode() CallOption {
	return func(o *CallOptions) {
		o.ResponseFormat = &ResponseFormat{Type: ResponseFormatJSONObject}
	}
}

// WithJSONSchema requests JSON output that conforms to the provided JSON Schema.
func WithJSONSchema(name string, schema json.RawMessage, strict bool) CallOption {
	return func(o *CallOptions) {
		o.ResponseFormat = &ResponseFormat{
			Type: ResponseFormatJSONSchema,
			JSONSchema: &JSONSchema{
				Name:   name,
				Schema: append(json.RawMessage(nil), schema...),
				Strict: strict,
			},
		}
	}
}

// WithModel sets the model to use for this call only, overriding the client's
// construction-time default for this GenerateContent, Stream, or Call
// invocation. To set a client's default model at construction time, use the
// provider-specific option such as openai.WithModel or anthropic.WithModel.
func WithModel(model string) CallOption {
	return func(o *CallOptions) {
		o.Model = model
	}
}

// WithStreamBufferSize sets the size of the stream channel buffer.
// Default is 100. Larger values reduce backpressure but use more memory.
func WithStreamBufferSize(size int) CallOption {
	return func(o *CallOptions) {
		if size > 0 {
			o.StreamBufferSize = size
		}
	}
}

// WithStreamSendTimeout sets the timeout for sending chunks to the stream channel.
// If the consumer stops reading and the buffer fills, chunks will be dropped after
// this timeout so the producing goroutine can exit instead of leaking.
// A value of 0 (or negative) leaves the default in effect (DefaultStreamSendTimeout,
// 30s) rather than disabling the timeout, to avoid goroutine leaks.
// Default is 30 seconds.
func WithStreamSendTimeout(d time.Duration) CallOption {
	return func(o *CallOptions) {
		o.StreamSendTimeout = d
	}
}

// WithDisableMessageMerging disables automatic merging of consecutive same-role messages.
// By default, the SDK merges consecutive user or assistant messages to avoid API validation
// errors from providers that reject such patterns. Use this option if you need to preserve
// the exact message structure. Note that ValidateMessages still applies when merging is
// disabled: structurally invalid conversations (e.g. one beginning with a tool message) are
// still rejected; only the consecutive-same-role merging step is skipped.
func WithDisableMessageMerging() CallOption {
	return func(o *CallOptions) {
		o.DisableMessageMerging = true
	}
}

// WithEstimateTokens enables token count estimation when providers don't return usage data.
// When enabled, the SDK will estimate prompt and completion tokens based on text length
// and common tokenizer heuristics. This is useful for cost tracking and rate limiting
// when using providers that don't report token usage.
//
// Note: Estimates are approximations. Actual token counts vary by model and tokenizer.
func WithEstimateTokens() CallOption {
	return func(o *CallOptions) {
		o.EstimateTokens = true
	}
}

// WithTrace sets per-call trace context overrides.
func WithTrace(t TraceOptions) CallOption {
	return func(o *CallOptions) {
		trace := t
		if len(t.Tags) > 0 {
			trace.Tags = append([]string(nil), t.Tags...)
		}
		if len(t.Metadata) > 0 {
			trace.Metadata = make(map[string]any, len(t.Metadata))
			for k, v := range t.Metadata {
				trace.Metadata[k] = v
			}
		}
		o.Trace = &trace
	}
}

// Provider-specific extension options
// These allow passing custom parameters to provider APIs (e.g., LoRAX adapter_id)

// WithExtraBody sets additional parameters to merge into the API request body.
// These parameters are added at the top level of the JSON request.
// Useful for provider-specific extensions like LoRAX adapter_id.
//
// Example:
//
//	llms.WithExtraBody(map[string]any{
//	    "adapter_id": "predibase/sql-lora",
//	    "custom_param": "value",
//	})
func WithExtraBody(extra map[string]any) CallOption {
	return func(o *CallOptions) {
		o.ExtraBody = extra
	}
}

// WithExtraBodyParam sets a single parameter in the extra body.
// Multiple calls will merge parameters.
func WithExtraBodyParam(key string, value any) CallOption {
	return func(o *CallOptions) {
		if o.ExtraBody == nil {
			o.ExtraBody = make(map[string]any)
		}
		o.ExtraBody[key] = value
	}
}

// WithAdapterID sets the adapter_id parameter for LoRAX and compatible servers.
// This is a convenience wrapper around WithExtraBodyParam.
//
// Example:
//
//	llms.WithAdapterID("predibase/sql-lora")
func WithAdapterID(adapterID string) CallOption {
	return WithExtraBodyParam("adapter_id", adapterID)
}

// WithWebSearch configures web search grounding for the request.
// Providers may implement this natively (Z.AI, Perplexity) or via
// external tools (Brave, Tavily, etc.).
func WithWebSearch(config WebSearchConfig) CallOption {
	return func(o *CallOptions) {
		o.WebSearch = &config
	}
}

// WithWebSearchEnabled is a convenience function to enable web search
// with default settings.
func WithWebSearchEnabled() CallOption {
	return WithWebSearch(WebSearchConfig{Enabled: true})
}

// ApplyOptions applies call options and returns the configured options
func ApplyOptions(opts ...CallOption) *CallOptions {
	options := DefaultCallOptions()
	for _, opt := range opts {
		opt(options)
	}
	return options
}

// Validate checks if the call options are valid and returns any validation errors.
// Returns nil if all options are valid.
func (o *CallOptions) Validate() error {
	var errs ValidationErrors

	if o.Temperature != nil && (*o.Temperature < 0 || *o.Temperature > 2.0) {
		errs = append(errs, ValidationError{
			Field:   "temperature",
			Value:   *o.Temperature,
			Message: "must be between 0 and 2",
		})
	}

	if o.MaxTokens != nil && *o.MaxTokens < 0 {
		errs = append(errs, ValidationError{
			Field:   "max_tokens",
			Value:   *o.MaxTokens,
			Message: "must be non-negative",
		})
	}

	if o.TopP != nil && (*o.TopP < 0 || *o.TopP > 1.0) {
		errs = append(errs, ValidationError{
			Field:   "top_p",
			Value:   *o.TopP,
			Message: "must be between 0 and 1",
		})
	}

	if o.FrequencyPenalty != nil && (*o.FrequencyPenalty < -2.0 || *o.FrequencyPenalty > 2.0) {
		errs = append(errs, ValidationError{
			Field:   "frequency_penalty",
			Value:   *o.FrequencyPenalty,
			Message: "must be between -2 and 2",
		})
	}

	if o.PresencePenalty != nil && (*o.PresencePenalty < -2.0 || *o.PresencePenalty > 2.0) {
		errs = append(errs, ValidationError{
			Field:   "presence_penalty",
			Value:   *o.PresencePenalty,
			Message: "must be between -2 and 2",
		})
	}

	// Validate tools
	for i, tool := range o.Tools {
		if tool.Type == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("tools[%d].type", i),
				Value:   tool.Type,
				Message: "type is required",
			})
		}
		if tool.Type == ToolTypeFunction && tool.Function == nil {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("tools[%d].function", i),
				Value:   nil,
				Message: "function definition is required for function type tools",
			})
		}
		if tool.Function != nil && tool.Function.Name == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("tools[%d].function.name", i),
				Value:   "",
				Message: "function name is required",
			})
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}
