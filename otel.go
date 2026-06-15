package llms

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	// InstrumentationName is the name used for OTel instrumentation
	InstrumentationName = "github.com/nocturnium/llm-go-sdk"

	// Metric names.
	metricRequests         = "llm.requests"
	metricTokensPrompt     = "llm.tokens.prompt"
	metricTokensCompletion = "llm.tokens.completion"
	metricRequestDuration  = "llm.request.duration"
	metricErrors           = "llm.errors"
	metricStreamChunks     = "llm.stream.chunks"

	// MaxOTelContentCapture limits the amount of streaming content captured
	// to prevent unbounded memory growth in long-running streams.
	maxOTelContentCapture = 100_000 // 100KB

	errorCategoryAuthentication        = "authentication"
	errorCategoryContentFiltered       = "content_filtered"
	errorCategoryContextLengthExceeded = apiErrorCodeContextLengthExceeded
	errorCategoryInvalidRequest        = "invalid_request"
	errorCategoryModelNotFound         = apiErrorCodeModelNotFound
	errorCategoryOther                 = "other"
	errorCategoryPermissionDenied      = "permission_denied"
	errorCategoryQuotaExceeded         = apiErrorCodeQuotaExceeded
	errorCategoryRateLimited           = "rate_limited"
	errorCategoryServerError           = apiErrorTypeServer
	errorCategoryServiceUnavailable    = "service_unavailable"
	errorCategoryStreamTimeout         = "stream_timeout"
	errorCategoryTimeout               = "timeout"
	errorCategoryUnknown               = "unknown"
	apiErrorCodeBadRequest             = "bad_request"
	apiErrorCodeForbidden              = "forbidden"
	apiErrorCodeInternalServerError    = "internal_server_error"
	apiErrorCodeInvalidAPIKey          = "invalid_api_key"
	apiErrorCodeNotFound               = "not_found"
	apiErrorCodeOverloaded             = "overloaded"
	apiErrorCodeRequestTimeout         = "request_timeout"
	apiErrorCodeSafety                 = "safety"
	apiErrorCodeTooManyRequests        = "too_many_requests"
	apiErrorCodeUnauthorized           = "unauthorized"
)

// Attribute keys for spans and metrics
var (
	attrProvider     = attribute.Key("llm.provider")
	attrModel        = attribute.Key("llm.model")
	attrRequestType  = attribute.Key("llm.request.type")
	attrFinishReason = attribute.Key("llm.finish_reason")
	attrErrorType    = attribute.Key("llm.error.type")
	attrStreaming    = attribute.Key("llm.streaming")
	attrToolCalls    = attribute.Key("llm.tool_calls")
)

// OTelMiddleware wraps an LLM with OpenTelemetry instrumentation
type OTelMiddleware struct {
	llm    LLM
	tracer trace.Tracer
	meter  metric.Meter

	// Metrics
	requestCounter   metric.Int64Counter
	promptTokens     metric.Int64Counter
	completionTokens metric.Int64Counter
	requestDuration  metric.Float64Histogram
	errorCounter     metric.Int64Counter
	streamChunkCount metric.Int64Counter

	// Options
	recordContent bool
}

// OTelOption configures the OTel middleware
type OTelOption func(*OTelMiddleware)

// NewOTelMiddleware creates a new OpenTelemetry instrumented LLM wrapper
func NewOTelMiddleware(llm LLM, opts ...OTelOption) (*OTelMiddleware, error) {
	m := &OTelMiddleware{
		llm:           llm,
		tracer:        otel.Tracer(InstrumentationName),
		meter:         otel.Meter(InstrumentationName),
		recordContent: false,
	}

	for _, opt := range opts {
		opt(m)
	}

	if err := m.initMetrics(); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *OTelMiddleware) initMetrics() error {
	var err error

	m.requestCounter, err = m.meter.Int64Counter(
		metricRequests,
		metric.WithDescription("Number of LLM requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return err
	}

	m.promptTokens, err = m.meter.Int64Counter(
		metricTokensPrompt,
		metric.WithDescription("Number of prompt tokens used"),
		metric.WithUnit("{token}"),
	)
	if err != nil {
		return err
	}

	m.completionTokens, err = m.meter.Int64Counter(
		metricTokensCompletion,
		metric.WithDescription("Number of completion tokens generated"),
		metric.WithUnit("{token}"),
	)
	if err != nil {
		return err
	}

	m.requestDuration, err = m.meter.Float64Histogram(
		metricRequestDuration,
		metric.WithDescription("Duration of LLM requests"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}

	m.errorCounter, err = m.meter.Int64Counter(
		metricErrors,
		metric.WithDescription("Number of LLM errors"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return err
	}

	m.streamChunkCount, err = m.meter.Int64Counter(
		metricStreamChunks,
		metric.WithDescription("Number of stream chunks received"),
		metric.WithUnit("{chunk}"),
	)
	if err != nil {
		return err
	}

	return nil
}

// WithOTelTracer sets a custom tracer
func WithOTelTracer(tracer trace.Tracer) OTelOption {
	return func(m *OTelMiddleware) {
		m.tracer = tracer
	}
}

// WithOTelMeter sets a custom meter
func WithOTelMeter(meter metric.Meter) OTelOption {
	return func(m *OTelMiddleware) {
		m.meter = meter
	}
}

// WithContentRecording enables recording of message content in spans
// WARNING: This may expose sensitive data in traces
func WithContentRecording(record bool) OTelOption {
	return func(m *OTelMiddleware) {
		m.recordContent = record
	}
}

// Call wraps the LLM's Call method with tracing and metrics
func (m *OTelMiddleware) Call(ctx context.Context, prompt string, options ...CallOption) (string, error) {
	ctx, span := m.tracer.Start(ctx, "llm.call",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	start := time.Now()
	provider := m.llm.Provider()
	model := m.llm.Model()

	// Set common attributes
	span.SetAttributes(
		attrProvider.String(string(provider)),
		attrModel.String(model),
		attrRequestType.String("call"),
		attrStreaming.Bool(false),
	)

	if m.recordContent {
		span.SetAttributes(attribute.String("llm.prompt", truncateForSpan(prompt, 1000)))
	}

	// Record request
	attrs := []attribute.KeyValue{
		attrProvider.String(string(provider)),
		attrModel.String(model),
		attrRequestType.String("call"),
	}
	m.requestCounter.Add(ctx, 1, metric.WithAttributes(attrs...))

	result, err := Call(ctx, m.llm, prompt, options...)

	duration := time.Since(start).Seconds()
	m.requestDuration.Record(ctx, duration, metric.WithAttributes(attrs...))

	if err != nil {
		m.recordError(ctx, span, err, attrs)
		return "", err
	}

	if m.recordContent {
		span.SetAttributes(attribute.String("llm.response", truncateForSpan(result, 1000)))
	}

	span.SetStatus(codes.Ok, "")
	return result, nil
}

// GenerateContent wraps the LLM's GenerateContent method with tracing and metrics
func (m *OTelMiddleware) GenerateContent(ctx context.Context, messages []Message, options ...CallOption) (*Response, error) {
	ctx, span := m.tracer.Start(ctx, "llm.generate_content",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	start := time.Now()
	provider := m.llm.Provider()
	model := m.llm.Model()

	// Set common attributes
	span.SetAttributes(
		attrProvider.String(string(provider)),
		attrModel.String(model),
		attrRequestType.String("generate_content"),
		attrStreaming.Bool(false),
		attribute.Int("llm.message_count", len(messages)),
	)

	if m.recordContent && len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		span.SetAttributes(attribute.String("llm.last_message", truncateForSpan(lastMsg.Content, 500)))
	}

	// Record request
	attrs := []attribute.KeyValue{
		attrProvider.String(string(provider)),
		attrModel.String(model),
		attrRequestType.String("generate_content"),
	}
	m.requestCounter.Add(ctx, 1, metric.WithAttributes(attrs...))

	resp, err := m.llm.GenerateContent(ctx, messages, options...)

	duration := time.Since(start).Seconds()
	m.requestDuration.Record(ctx, duration, metric.WithAttributes(attrs...))

	if err != nil {
		m.recordError(ctx, span, err, attrs)
		return nil, err
	}

	// Record token usage
	m.promptTokens.Add(ctx, int64(resp.Usage.PromptTokens), metric.WithAttributes(attrs...))
	m.completionTokens.Add(ctx, int64(resp.Usage.CompletionTokens), metric.WithAttributes(attrs...))

	// Set response attributes
	span.SetAttributes(
		attrFinishReason.String(string(resp.FinishReason)),
		attribute.Int("llm.tokens.prompt", resp.Usage.PromptTokens),
		attribute.Int("llm.tokens.completion", resp.Usage.CompletionTokens),
		attribute.Int("llm.tokens.total", resp.Usage.TotalTokens),
		attrToolCalls.Int(len(resp.ToolCalls)),
	)

	if m.recordContent {
		span.SetAttributes(attribute.String("llm.response", truncateForSpan(resp.Content, 1000)))
	}

	span.SetStatus(codes.Ok, "")
	return resp, nil
}

// Stream wraps the LLM's Stream method with tracing and metrics
func (m *OTelMiddleware) Stream(ctx context.Context, messages []Message, options ...CallOption) (<-chan StreamChunk, error) {
	ctx, span := m.tracer.Start(ctx, "llm.stream",
		trace.WithSpanKind(trace.SpanKindClient),
	)

	start := time.Now()
	provider := m.llm.Provider()
	model := m.llm.Model()

	// Set common attributes
	span.SetAttributes(
		attrProvider.String(string(provider)),
		attrModel.String(model),
		attrRequestType.String("stream"),
		attrStreaming.Bool(true),
		attribute.Int("llm.message_count", len(messages)),
	)

	// Record request
	attrs := []attribute.KeyValue{
		attrProvider.String(string(provider)),
		attrModel.String(model),
		attrRequestType.String("stream"),
	}
	m.requestCounter.Add(ctx, 1, metric.WithAttributes(attrs...))

	stream, err := m.llm.Stream(ctx, messages, options...)
	if err != nil {
		duration := time.Since(start).Seconds()
		m.requestDuration.Record(ctx, duration, metric.WithAttributes(attrs...))
		m.recordError(ctx, span, err, attrs)
		span.End()
		return nil, err
	}

	// Apply options to get buffer size and timeout for backpressure handling
	opts := ApplyOptions(options...)
	wrappedStream := make(chan StreamChunk, opts.StreamBufferSize)
	sender := NewStreamSender(ctx, wrappedStream, opts.StreamSendTimeout)

	go func() {
		defer close(wrappedStream)
		defer span.End()

		var chunkCount int64
		var contentBuilder strings.Builder
		var contentTruncated bool
		var usage *Usage
		var finishReason string
		var toolCallCount int
		var hadError bool

		// Pre-allocate some capacity to reduce allocations
		contentBuilder.Grow(1024)

		// Ensure metrics are always recorded, even on panic or early return.
		// This defer runs after the span.End() defer, so metrics are recorded
		// before the span ends.
		defer func() {
			if r := recover(); r != nil {
				// Record panic as error
				panicErr := fmt.Errorf("panic in stream processing: %v", r)
				m.recordError(ctx, span, panicErr, attrs)
				hadError = true
			}

			// Always record duration and chunk count
			duration := time.Since(start).Seconds()
			m.requestDuration.Record(ctx, duration, metric.WithAttributes(attrs...))
			if chunkCount > 0 {
				m.streamChunkCount.Add(ctx, chunkCount, metric.WithAttributes(attrs...))
			}

			// Record token usage if available
			if usage != nil {
				m.promptTokens.Add(ctx, int64(usage.PromptTokens), metric.WithAttributes(attrs...))
				m.completionTokens.Add(ctx, int64(usage.CompletionTokens), metric.WithAttributes(attrs...))

				span.SetAttributes(
					attribute.Int("llm.tokens.prompt", usage.PromptTokens),
					attribute.Int("llm.tokens.completion", usage.CompletionTokens),
					attribute.Int("llm.tokens.total", usage.TotalTokens),
				)
			}

			span.SetAttributes(
				attrFinishReason.String(finishReason),
				attribute.Int64("llm.stream.chunks", chunkCount),
				attrToolCalls.Int(toolCallCount),
			)

			if m.recordContent {
				span.SetAttributes(attribute.String("llm.response", truncateForSpan(contentBuilder.String(), 1000)))
			}

			if !hadError {
				if err := ctx.Err(); err != nil {
					hadError = true
					m.recordError(ctx, span, err, attrs)
				}
			}

			if !hadError {
				span.SetStatus(codes.Ok, "")
			}
		}()

		for chunk := range stream {
			chunkCount++

			// Use StreamSender to handle backpressure. On early exit a terminal
			// chunk is forwarded so the consumer never sees a silent close.
			sendResult := sender.Send(chunk)
			if sender.ForwardTerminalOnEarlyExit(sendResult) {
				hadError = true
				m.recordError(ctx, span, streamSendResultError(ctx, sendResult), attrs)
				return
			}

			if chunk.Error != nil {
				hadError = true
				m.recordError(ctx, span, chunk.Error, attrs)
				return
			}

			// Limit captured content to prevent unbounded memory growth.
			// This uses a strings.Builder for efficient concatenation and
			// stops capturing once we reach the limit to prevent OOM.
			if !contentTruncated && contentBuilder.Len() < maxOTelContentCapture {
				remaining := maxOTelContentCapture - contentBuilder.Len()
				if len(chunk.Content) <= remaining {
					contentBuilder.WriteString(chunk.Content)
				} else {
					contentBuilder.WriteString(truncateUTF8(chunk.Content, remaining, ""))
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
				toolCallCount = len(chunk.ToolCalls)
			}
		}
	}()

	return wrappedStream, nil
}

// Provider returns the underlying LLM's provider
func (m *OTelMiddleware) Provider() Provider {
	return m.llm.Provider()
}

// Model returns the underlying LLM's model
func (m *OTelMiddleware) Model() string {
	return m.llm.Model()
}

// Unwrap returns the underlying LLM
func (m *OTelMiddleware) Unwrap() LLM {
	return m.llm
}

func (m *OTelMiddleware) recordError(ctx context.Context, span trace.Span, err error, attrs []attribute.KeyValue) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())

	errorType := normalizeErrorType(err)
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		span.SetAttributes(
			attribute.Int("llm.error.status_code", apiErr.StatusCode),
			attrErrorType.String(errorType),
		)
	}

	attrs = append(attrs, attrErrorType.String(errorType))
	m.errorCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
}

func truncateForSpan(s string, maxLen int) string {
	return truncateUTF8(s, maxLen, "...")
}

func truncateUTF8(s string, maxLen int, suffix string) string {
	if len(s) <= maxLen {
		return s
	}

	end := 0
	for i := range s {
		if i > maxLen {
			break
		}
		end = i
	}
	if end == 0 {
		_, size := utf8.DecodeRuneInString(s)
		firstRune := s[:size]
		if len(firstRune) <= maxLen {
			return firstRune + suffix
		}
		return suffix
	}
	return s[:end] + suffix
}

func streamSendResultError(ctx context.Context, result SendResult) error {
	switch result {
	case SendContextCanceled:
		if err := ctx.Err(); err != nil {
			return err
		}
		return ErrStreamInterrupted
	case SendTimeout:
		return ErrStreamTimeout
	default:
		return ErrStreamInterrupted
	}
}

func normalizeErrorType(err error) string {
	switch {
	case err == nil:
		return errorCategoryUnknown
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, ErrTimeout):
		return errorCategoryTimeout
	case errors.Is(err, ErrStreamTimeout):
		return errorCategoryStreamTimeout
	case errors.Is(err, ErrRateLimited):
		return errorCategoryRateLimited
	case errors.Is(err, ErrQuotaExceeded):
		return errorCategoryQuotaExceeded
	case errors.Is(err, ErrAuthenticationFailed):
		return errorCategoryAuthentication
	case errors.Is(err, ErrPermissionDenied):
		return errorCategoryPermissionDenied
	case errors.Is(err, ErrModelNotFound):
		return errorCategoryModelNotFound
	case errors.Is(err, ErrContextLengthExceeded):
		return errorCategoryContextLengthExceeded
	case errors.Is(err, ErrContentFiltered):
		return errorCategoryContentFiltered
	case errors.Is(err, ErrServiceUnavailable):
		return errorCategoryServiceUnavailable
	case errors.Is(err, ErrServerError):
		return errorCategoryServerError
	case errors.Is(err, ErrInvalidParameters), errors.Is(err, ErrEmptyMessages):
		return errorCategoryInvalidRequest
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return errorCategoryUnknown
	}
	typeCategory := normalizeAPIErrorType(apiErr.Type)
	codeCategory := normalizeAPIErrorType(apiErr.Code)
	if typeCategory != "" {
		return typeCategory
	}
	if codeCategory != "" {
		return codeCategory
	}
	switch apiErr.StatusCode {
	case 400:
		return errorCategoryInvalidRequest
	case 401:
		return errorCategoryAuthentication
	case 403:
		return errorCategoryPermissionDenied
	case 404:
		return errorCategoryModelNotFound
	case 408:
		return errorCategoryTimeout
	case 413:
		return errorCategoryContextLengthExceeded
	case 429:
		return errorCategoryRateLimited
	case 500, 502:
		return errorCategoryServerError
	case 503, 504:
		return errorCategoryServiceUnavailable
	}
	if apiErr.Type != "" || apiErr.Code != "" {
		return errorCategoryOther
	}
	return errorCategoryUnknown
}

func normalizeAPIErrorType(value string) string {
	switch strings.ToLower(value) {
	case "":
		return ""
	case apiErrorTypeRateLimit, errorCategoryRateLimited, apiErrorCodeTooManyRequests:
		return errorCategoryRateLimited
	case apiErrorTypeAuthentication, apiErrorCodeUnauthorized, apiErrorCodeInvalidAPIKey:
		return errorCategoryAuthentication
	case apiErrorTypePermission, errorCategoryPermissionDenied, apiErrorCodeForbidden:
		return errorCategoryPermissionDenied
	case apiErrorCodeQuotaExceeded, apiErrorCodeInsufficientQuota:
		return errorCategoryQuotaExceeded
	case apiErrorCodeContentFilter, errorCategoryContentFiltered, apiErrorCodeSafety:
		return errorCategoryContentFiltered
	case apiErrorCodeModelNotFound, apiErrorCodeNotFound:
		return errorCategoryModelNotFound
	case apiErrorCodeContextLengthExceeded:
		return errorCategoryContextLengthExceeded
	case apiErrorTypeInvalidRequest, errorCategoryInvalidRequest, apiErrorCodeBadRequest:
		return errorCategoryInvalidRequest
	case errorCategoryTimeout, apiErrorCodeRequestTimeout:
		return errorCategoryTimeout
	case apiErrorTypeServer, apiErrorCodeInternalServerError:
		return errorCategoryServerError
	case errorCategoryServiceUnavailable, apiErrorCodeOverloaded:
		return errorCategoryServiceUnavailable
	}
	return ""
}

// Ensure OTelMiddleware implements LLM
var _ LLM = (*OTelMiddleware)(nil)
