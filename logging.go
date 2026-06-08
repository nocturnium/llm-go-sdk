package llms

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// maxLoggedStreamContent is the maximum content size (in bytes) to log for streaming responses.
// This prevents unbounded memory growth when logging large streaming responses.
const maxLoggedStreamContent = 100_000 // 100KB

// Logger is the interface for logging LLM requests and responses
type Logger interface {
	// LogRequest is called before making an LLM request
	LogRequest(ctx context.Context, req *LogEntry)

	// LogResponse is called after receiving an LLM response
	LogResponse(ctx context.Context, resp *LogEntry)

	// LogError is called when an error occurs
	LogError(ctx context.Context, entry *LogEntry, err error)
}

// LogEntry contains information about an LLM request or response
type LogEntry struct {
	// RequestID is a unique identifier for this request
	RequestID string `json:"request_id,omitempty"`

	// Provider is the LLM provider (openai, anthropic, etc.)
	Provider Provider `json:"provider,omitempty"`

	// Model is the model name
	Model string `json:"model,omitempty"`

	// Operation type (call, generate_content, stream, embed)
	Operation string `json:"operation,omitempty"`

	// Messages is the input messages (may be redacted)
	Messages []Message `json:"messages,omitempty"`

	// Response content (may be truncated)
	Content string `json:"content,omitempty"`

	// Token usage
	Usage *Usage `json:"usage,omitempty"`

	// Duration of the request
	Duration time.Duration `json:"duration,omitempty"`

	// Timestamp of the request
	Timestamp time.Time `json:"timestamp"`

	// FinishReason from the response
	FinishReason string `json:"finish_reason,omitempty"`

	// ToolCalls if any were made
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// Streaming indicates if this was a streaming request
	Streaming bool `json:"streaming,omitempty"`

	// Error message if an error occurred
	Error string `json:"error,omitempty"`

	// Extra metadata
	Metadata map[string]any `json:"metadata,omitempty"`

	// Langfuse-compatible fields
	// These fields enable seamless integration with Langfuse observability

	// TraceID is the Langfuse trace ID
	TraceID string `json:"trace_id,omitempty"`

	// SpanID is the Langfuse observation/span ID
	SpanID string `json:"span_id,omitempty"`

	// ParentSpanID is the parent observation ID for nested spans
	ParentSpanID string `json:"parent_span_id,omitempty"`

	// UserID is the end-user identifier
	UserID string `json:"user_id,omitempty"`

	// SessionID groups related traces
	SessionID string `json:"session_id,omitempty"`

	// Tags for categorization
	Tags []string `json:"tags,omitempty"`

	// Version of the application/component
	Version string `json:"version,omitempty"`

	// Environment (production, staging, development)
	Environment string `json:"environment,omitempty"`

	// InputJSON is the structured input as JSON string
	InputJSON string `json:"input_json,omitempty"`

	// OutputJSON is the structured output as JSON string
	OutputJSON string `json:"output_json,omitempty"`

	// CostUSD is the estimated cost in USD
	CostUSD float64 `json:"cost_usd,omitempty"`

	// RequestParameters captures the request settings for reproducibility
	RequestParameters map[string]any `json:"request_parameters,omitempty"`

	// TimeToFirstToken for streaming requests
	TimeToFirstToken time.Duration `json:"time_to_first_token,omitempty"`
}

// ToLangfuseGeneration converts LogEntry to a Langfuse-compatible generation format.
// This can be used to send data to Langfuse's API or for logging in a Langfuse-compatible format.
func (e *LogEntry) ToLangfuseGeneration() map[string]any {
	gen := map[string]any{
		"type":       "GENERATION",
		"name":       e.Operation,
		"start_time": e.Timestamp.Format(time.RFC3339Nano),
		"end_time":   e.Timestamp.Add(e.Duration).Format(time.RFC3339Nano),
		"model":      e.Model,
	}

	if e.TraceID != "" {
		gen["trace_id"] = e.TraceID
	}
	if e.SpanID != "" {
		gen["observation_id"] = e.SpanID
	}
	if e.ParentSpanID != "" {
		gen["parent_observation_id"] = e.ParentSpanID
	}

	// Input/Output
	if e.InputJSON != "" {
		gen["input"] = e.InputJSON
	} else if len(e.Messages) > 0 {
		gen["input"] = FormatInput(e.Messages, InputFormatMessages)
	}

	if e.OutputJSON != "" {
		gen["output"] = e.OutputJSON
	} else if e.Content != "" {
		gen["output"] = e.Content
	}

	// Usage
	if e.Usage != nil {
		gen["usage"] = map[string]any{
			"prompt_tokens":     e.Usage.PromptTokens,
			"completion_tokens": e.Usage.CompletionTokens,
			"total_tokens":      e.Usage.TotalTokens,
		}
	}

	// Langfuse-specific fields
	if e.UserID != "" {
		gen["user_id"] = e.UserID
	}
	if e.SessionID != "" {
		gen["session_id"] = e.SessionID
	}
	if len(e.Tags) > 0 {
		gen["tags"] = e.Tags
	}
	if e.Version != "" {
		gen["version"] = e.Version
	}

	// Metadata
	if len(e.Metadata) > 0 || len(e.RequestParameters) > 0 {
		metadata := make(map[string]any)
		for k, v := range e.Metadata {
			metadata[k] = v
		}
		for k, v := range e.RequestParameters {
			metadata["param_"+k] = v
		}
		if e.CostUSD > 0 {
			metadata["cost_usd"] = e.CostUSD
		}
		if e.TimeToFirstToken > 0 {
			metadata["time_to_first_token_ms"] = e.TimeToFirstToken.Milliseconds()
		}
		gen["metadata"] = metadata
	}

	return gen
}

// PopulateFromTraceContext fills LogEntry fields from a TraceContext
func (e *LogEntry) PopulateFromTraceContext(tc *TraceContext) {
	if tc == nil {
		return
	}

	e.TraceID = tc.TraceID
	e.SpanID = tc.SpanID
	e.ParentSpanID = tc.ParentID
	e.UserID = tc.UserID
	e.SessionID = tc.SessionID
	e.Version = tc.Version
	e.Environment = tc.Environment

	if len(tc.Tags) > 0 {
		e.Tags = make([]string, len(tc.Tags))
		copy(e.Tags, tc.Tags)
	}

	// Merge propagated metadata into Metadata
	if len(tc.Metadata) > 0 {
		if e.Metadata == nil {
			e.Metadata = make(map[string]any)
		}
		for k, v := range tc.Metadata {
			e.Metadata[k] = v
		}
	}
}

// PopulateFromCallOptions fills LogEntry fields from CallOptions
func (e *LogEntry) PopulateFromCallOptions(opts *CallOptions) {
	if opts == nil {
		return
	}

	if opts.Trace != nil {
		// Override with call options if provided
		if opts.Trace.TraceID != "" {
			e.TraceID = opts.Trace.TraceID
		}
		if opts.Trace.SpanID != "" {
			e.SpanID = opts.Trace.SpanID
		}
		if opts.Trace.ParentID != "" {
			e.ParentSpanID = opts.Trace.ParentID
		}
		if opts.Trace.UserID != "" {
			e.UserID = opts.Trace.UserID
		}
		if opts.Trace.SessionID != "" {
			e.SessionID = opts.Trace.SessionID
		}
		if opts.Trace.Version != "" {
			e.Version = opts.Trace.Version
		}
		if len(opts.Trace.Tags) > 0 {
			e.Tags = append(e.Tags, opts.Trace.Tags...)
		}

		// Merge trace metadata
		if len(opts.Trace.Metadata) > 0 {
			if e.Metadata == nil {
				e.Metadata = make(map[string]any)
			}
			for k, v := range opts.Trace.Metadata {
				e.Metadata[k] = v
			}
		}
	}

	// Capture request parameters. Temperature/TopP are only recorded when the
	// caller explicitly set them (non-nil pointer); 0 is a valid set value.
	e.RequestParameters = map[string]any{}
	if opts.MaxTokens != nil {
		e.RequestParameters["max_tokens"] = *opts.MaxTokens
	}
	if opts.Temperature != nil {
		e.RequestParameters["temperature"] = *opts.Temperature
	}
	if opts.TopP != nil {
		e.RequestParameters["top_p"] = *opts.TopP
	}
	if opts.FrequencyPenalty != 0 {
		e.RequestParameters["frequency_penalty"] = opts.FrequencyPenalty
	}
	if opts.PresencePenalty != 0 {
		e.RequestParameters["presence_penalty"] = opts.PresencePenalty
	}
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type == ResponseFormatJSONObject {
		e.RequestParameters["json_mode"] = true
	}
}

// NopLogger is a no-op logger that discards all logs
type NopLogger struct{}

// LogRequest does nothing
func (NopLogger) LogRequest(context.Context, *LogEntry) {}

// LogResponse does nothing
func (NopLogger) LogResponse(context.Context, *LogEntry) {}

// LogError does nothing
func (NopLogger) LogError(context.Context, *LogEntry, error) {}

// truncateString truncates a string to maxLength and appends "..." if truncated.
// If maxLength is <= 0, the original string is returned unchanged.
func truncateString(s string, maxLength int) string {
	if maxLength <= 0 || len(s) <= maxLength {
		return s
	}
	return s[:maxLength] + "..."
}

// logValueReplacer neutralizes the carriage-return and line-feed characters that
// an attacker could use to forge additional log entries (CWE-117 log injection).
var logValueReplacer = strings.NewReplacer("\r", "\\r", "\n", "\\n")

// sanitizeLogValue escapes CR/LF in user-controlled content (prompts, responses)
// so it cannot break out of its log field and inject forged log lines. It is
// applied at the point such values are formatted for logging.
func sanitizeLogValue(s string) string {
	return logValueReplacer.Replace(s)
}

// LoggingMiddleware wraps an LLM with logging capabilities
type LoggingMiddleware struct {
	llm    LLM
	logger Logger
	genID  func() string
}

// NewLoggingMiddleware creates a new logging middleware
func NewLoggingMiddleware(llm LLM, logger Logger) *LoggingMiddleware {
	return &LoggingMiddleware{
		llm:    llm,
		logger: logger,
		genID:  defaultIDGenerator,
	}
}

// WithIDGenerator sets a custom ID generator for request IDs
func (m *LoggingMiddleware) WithIDGenerator(genID func() string) *LoggingMiddleware {
	m.genID = genID
	return m
}

// Call wraps the LLM's Call method with logging
func (m *LoggingMiddleware) Call(ctx context.Context, prompt string, options ...CallOption) (string, error) {
	requestID := m.genID()
	start := time.Now()

	entry := &LogEntry{
		RequestID: requestID,
		Provider:  m.llm.Provider(),
		Model:     m.llm.Model(),
		Operation: "call",
		Messages:  []Message{{Role: RoleUser, Content: prompt}},
		Timestamp: start,
		Streaming: false,
	}

	m.logger.LogRequest(ctx, entry)

	result, err := Call(ctx, m.llm, prompt, options...)

	entry.Duration = time.Since(start)

	if err != nil {
		m.logger.LogError(ctx, entry, err)
		return "", err
	}

	entry.Content = result
	m.logger.LogResponse(ctx, entry)

	return result, nil
}

// GenerateContent wraps the LLM's GenerateContent method with logging
func (m *LoggingMiddleware) GenerateContent(ctx context.Context, messages []Message, options ...CallOption) (*Response, error) {
	requestID := m.genID()
	start := time.Now()

	entry := &LogEntry{
		RequestID: requestID,
		Provider:  m.llm.Provider(),
		Model:     m.llm.Model(),
		Operation: "generate_content",
		Messages:  messages,
		Timestamp: start,
		Streaming: false,
	}

	m.logger.LogRequest(ctx, entry)

	resp, err := m.llm.GenerateContent(ctx, messages, options...)

	entry.Duration = time.Since(start)

	if err != nil {
		m.logger.LogError(ctx, entry, err)
		return nil, err
	}

	entry.Content = resp.Content
	entry.Usage = &resp.Usage
	entry.FinishReason = string(resp.FinishReason)
	entry.ToolCalls = resp.ToolCalls
	m.logger.LogResponse(ctx, entry)

	return resp, nil
}

// Stream wraps the LLM's Stream method with logging
func (m *LoggingMiddleware) Stream(ctx context.Context, messages []Message, options ...CallOption) (<-chan StreamChunk, error) {
	requestID := m.genID()
	start := time.Now()

	entry := &LogEntry{
		RequestID: requestID,
		Provider:  m.llm.Provider(),
		Model:     m.llm.Model(),
		Operation: "stream",
		Messages:  messages,
		Timestamp: start,
		Streaming: true,
	}

	m.logger.LogRequest(ctx, entry)

	stream, err := m.llm.Stream(ctx, messages, options...)
	if err != nil {
		entry.Duration = time.Since(start)
		m.logger.LogError(ctx, entry, err)
		return nil, err
	}

	// Apply options to get buffer size and timeout for backpressure handling
	opts := ApplyOptions(options...)
	wrappedStream := make(chan StreamChunk, opts.StreamBufferSize)
	sender := NewStreamSender(ctx, wrappedStream, opts.StreamSendTimeout)

	go func() {
		defer close(wrappedStream)

		var contentBuilder strings.Builder
		var usage *Usage
		var finishReason string
		var toolCalls []ToolCall
		contentTruncated := false
		streamInterrupted := false

		for chunk := range stream {
			// Use StreamSender to handle backpressure - if consumer stops reading,
			// we don't block forever and can log what we have. On early exit a
			// terminal chunk is forwarded so the consumer never sees a silent close.
			if sender.ForwardTerminalOnEarlyExit(sender.Send(chunk)) {
				streamInterrupted = true
				break
			}

			if chunk.Error != nil {
				entry.Duration = time.Since(start)
				m.logger.LogError(ctx, entry, chunk.Error)
				return
			}

			// Limit logged content to prevent unbounded memory growth
			if !contentTruncated && contentBuilder.Len() < maxLoggedStreamContent {
				remaining := maxLoggedStreamContent - contentBuilder.Len()
				if len(chunk.Content) <= remaining {
					contentBuilder.WriteString(chunk.Content)
				} else {
					contentBuilder.WriteString(chunk.Content[:remaining])
					contentBuilder.WriteString("...[truncated]")
					contentTruncated = true
				}
			}

			if chunk.Usage != nil {
				usage = chunk.Usage
			}
			if chunk.FinishReason != "" {
				finishReason = string(chunk.FinishReason)
			}
			if len(chunk.ToolCalls) > 0 {
				toolCalls = chunk.ToolCalls
			}
		}

		entry.Duration = time.Since(start)
		if streamInterrupted {
			entry.Content = contentBuilder.String() + "...[stream interrupted]"
		} else {
			entry.Content = contentBuilder.String()
		}
		entry.Usage = usage
		entry.FinishReason = finishReason
		entry.ToolCalls = toolCalls
		m.logger.LogResponse(ctx, entry)
	}()

	return wrappedStream, nil
}

// Provider returns the underlying LLM's provider
func (m *LoggingMiddleware) Provider() Provider {
	return m.llm.Provider()
}

// Model returns the underlying LLM's model
func (m *LoggingMiddleware) Model() string {
	return m.llm.Model()
}

// Unwrap returns the underlying LLM
func (m *LoggingMiddleware) Unwrap() LLM {
	return m.llm
}

var (
	requestCounter uint64
	// IDBufPool reuses byte slices for ID generation to reduce allocations.
	// Each buffer is sized for: timestamp (14) + "-" (1) + letter (1) = 16 bytes.
	idBufPool = sync.Pool{
		New: func() any {
			buf := make([]byte, 0, 16)
			return &buf
		},
	}
)

func defaultIDGenerator() string {
	count := atomic.AddUint64(&requestCounter, 1)
	letter := byte('A' + count%26)

	// Get buffer from pool and reset it
	bufPtr, ok := idBufPool.Get().(*[]byte)
	if !ok {
		// This should never happen if the pool is used correctly,
		// but fall back to a new buffer if it does
		newBuf := make([]byte, 0, 32)
		bufPtr = &newBuf
	}
	buf := (*bufPtr)[:0]

	// Use AppendFormat to avoid intermediate string allocation from Format()
	// Use UTC for consistency in distributed systems
	buf = time.Now().UTC().AppendFormat(buf, "20060102150405")
	buf = append(buf, '-', letter)

	// Copy to result string before returning buffer to pool
	result := string(buf)

	// Return buffer to pool
	*bufPtr = buf
	idBufPool.Put(bufPtr)

	return result
}

// Ensure LoggingMiddleware implements LLM
var _ LLM = (*LoggingMiddleware)(nil)
