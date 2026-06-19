package observability

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v3"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	// MaxMetricsContentCapture limits the amount of streaming content captured
	// to prevent unbounded memory growth in long-running streams.
	maxMetricsContentCapture = 100_000 // 100KB
)

// MetricsMiddleware provides enhanced observability combining OTel and cost tracking
// It wraps an LLM with comprehensive tracing, metrics, and cost estimation
type MetricsMiddleware struct {
	llm    llms.LLM
	tracer trace.Tracer
	meter  metric.Meter

	// Cost tracking
	costTracker *llms.CostTracker

	// Standard metrics (from OTel)
	requestCounter   metric.Int64Counter
	promptTokens     metric.Int64Counter
	completionTokens metric.Int64Counter
	requestDuration  metric.Float64Histogram
	errorCounter     metric.Int64Counter
	streamChunkCount metric.Int64Counter

	// Enhanced metrics
	costEstimate      metric.Float64Counter
	timeToFirstToken  metric.Float64Histogram
	tokensPerSecond   metric.Float64Histogram
	activeRequests    metric.Int64UpDownCounter
	successRateWindow *slidingWindow

	// Options
	recordContent   bool
	recordCost      bool
	windowDuration  time.Duration
	activeReqsCount atomic.Int64 // Use atomic to prevent race conditions
}

// MetricsOption configures the metrics middleware
type MetricsOption func(*MetricsMiddleware)

// NewMetricsMiddleware creates a new enhanced metrics middleware
func NewMetricsMiddleware(llm llms.LLM, opts ...MetricsOption) (*MetricsMiddleware, error) {
	m := &MetricsMiddleware{
		llm:            llm,
		tracer:         otel.Tracer(InstrumentationName),
		meter:          otel.Meter(InstrumentationName),
		costTracker:    llms.NewCostTracker(),
		recordContent:  false,
		recordCost:     true,
		windowDuration: 5 * time.Minute,
	}

	for _, opt := range opts {
		opt(m)
	}

	m.successRateWindow = newSlidingWindow(m.windowDuration)

	if err := m.initMetrics(); err != nil {
		return nil, err
	}

	return m, nil
}

// WithMetricsTracer sets a custom tracer
func WithMetricsTracer(tracer trace.Tracer) MetricsOption {
	return func(m *MetricsMiddleware) {
		m.tracer = tracer
	}
}

// WithMetricsMeter sets a custom meter
func WithMetricsMeter(meter metric.Meter) MetricsOption {
	return func(m *MetricsMiddleware) {
		m.meter = meter
	}
}

// WithMetricsCostTracker sets a custom cost tracker
func WithMetricsCostTracker(tracker *llms.CostTracker) MetricsOption {
	return func(m *MetricsMiddleware) {
		m.costTracker = tracker
	}
}

// WithMetricsContentRecording enables recording of message content in spans
func WithMetricsContentRecording(record bool) MetricsOption {
	return func(m *MetricsMiddleware) {
		m.recordContent = record
	}
}

// WithMetricsCostRecording enables recording of cost metrics
func WithMetricsCostRecording(record bool) MetricsOption {
	return func(m *MetricsMiddleware) {
		m.recordCost = record
	}
}

// WithSuccessRateWindow sets the duration for success rate calculation
func WithSuccessRateWindow(duration time.Duration) MetricsOption {
	return func(m *MetricsMiddleware) {
		m.windowDuration = duration
	}
}

// Call wraps the LLM's Call method with enhanced metrics
func (m *MetricsMiddleware) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	ctx, span := m.tracer.Start(ctx, "llm.call",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	start := time.Now()
	provider := m.llm.Provider()
	model := m.llm.Model()

	attrs := []attribute.KeyValue{
		attrProvider.String(string(provider)),
		attrModel.String(model),
		attrRequestType.String("call"),
	}

	span.SetAttributes(attrs...)
	span.SetAttributes(attrStreaming.Bool(false))

	if m.recordContent {
		span.SetAttributes(attribute.String("llm.prompt", truncateForSpan(prompt, 1000)))
	}

	m.requestCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.incrementActive(ctx, attrs)
	defer m.decrementActive(ctx, attrs)

	// Use GenerateContent internally to get usage info
	messages := []llms.Message{{Role: llms.RoleUser, Content: prompt}}
	resp, err := m.llm.GenerateContent(ctx, messages, options...)

	duration := time.Since(start).Seconds()
	m.requestDuration.Record(ctx, duration, metric.WithAttributes(attrs...))

	if err != nil {
		m.recordError(ctx, span, err, attrs)
		m.successRateWindow.Record(false)
		return "", err
	}

	m.successRateWindow.Record(true)
	m.recordUsage(ctx, span, provider, model, resp.Usage, duration, attrs)

	if m.recordContent {
		span.SetAttributes(attribute.String("llm.response", truncateForSpan(resp.Content, 1000)))
	}

	span.SetStatus(codes.Ok, "")
	return resp.Content, nil
}

// GenerateContent wraps the LLM's GenerateContent method with enhanced metrics
func (m *MetricsMiddleware) GenerateContent(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.Response, error) {
	ctx, span := m.tracer.Start(ctx, "llm.generate_content",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	start := time.Now()
	provider := m.llm.Provider()
	model := m.llm.Model()

	attrs := []attribute.KeyValue{
		attrProvider.String(string(provider)),
		attrModel.String(model),
		attrRequestType.String("generate_content"),
	}

	span.SetAttributes(attrs...)
	span.SetAttributes(
		attrStreaming.Bool(false),
		attribute.Int("llm.message_count", len(messages)),
	)

	if m.recordContent && len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		span.SetAttributes(attribute.String("llm.last_message", truncateForSpan(lastMsg.Content, 500)))
	}

	m.requestCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.incrementActive(ctx, attrs)
	defer m.decrementActive(ctx, attrs)

	resp, err := m.llm.GenerateContent(ctx, messages, options...)

	duration := time.Since(start).Seconds()
	m.requestDuration.Record(ctx, duration, metric.WithAttributes(attrs...))

	if err != nil {
		m.recordError(ctx, span, err, attrs)
		m.successRateWindow.Record(false)
		return nil, err
	}

	m.successRateWindow.Record(true)
	m.recordUsage(ctx, span, provider, model, resp.Usage, duration, attrs)

	span.SetAttributes(
		attrFinishReason.String(string(resp.FinishReason)),
		attrToolCalls.Int(len(resp.ToolCalls)),
	)

	if m.recordContent {
		span.SetAttributes(attribute.String("llm.response", truncateForSpan(resp.Content, 1000)))
	}

	span.SetStatus(codes.Ok, "")
	return resp, nil
}

// Stream wraps the LLM's Stream method with enhanced metrics including time-to-first-token
func (m *MetricsMiddleware) Stream(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	ctx, span := m.tracer.Start(ctx, "llm.stream",
		trace.WithSpanKind(trace.SpanKindClient),
	)

	start := time.Now()
	provider := m.llm.Provider()
	model := m.llm.Model()

	attrs := []attribute.KeyValue{
		attrProvider.String(string(provider)),
		attrModel.String(model),
		attrRequestType.String("stream"),
	}

	span.SetAttributes(attrs...)
	span.SetAttributes(
		attrStreaming.Bool(true),
		attribute.Int("llm.message_count", len(messages)),
	)

	m.requestCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.incrementActive(ctx, attrs)

	stream, err := m.llm.Stream(ctx, messages, options...)
	if err != nil {
		duration := time.Since(start).Seconds()
		m.requestDuration.Record(ctx, duration, metric.WithAttributes(attrs...))
		m.recordError(ctx, span, err, attrs)
		m.decrementActive(ctx, attrs)
		m.successRateWindow.Record(false)
		span.End()
		return nil, err
	}

	opts := llms.ApplyOptions(options...)
	wrappedStream := make(chan llms.StreamChunk, opts.StreamBufferSize)
	sender := llms.NewStreamSender(ctx, wrappedStream, opts.StreamSendTimeout)
	go func() {
		// Order matters: the finalize defer (registered last, runs first under
		// LIFO) populates the span, then span.End() must run before
		// decrementActive so that an observer waiting on ActiveRequests()==0 is
		// guaranteed the span has already ended.
		defer close(wrappedStream)
		defer m.decrementActive(ctx, attrs)
		defer span.End()

		var chunkCount int64
		var contentBuilder strings.Builder
		var contentTruncated bool
		var usage *llms.Usage
		var finishReason string
		var toolCallCount int
		var firstTokenTime time.Time
		var hadFirstToken bool
		var hadError bool

		// Pre-allocate some capacity to reduce allocations
		contentBuilder.Grow(1024)

		defer func() {
			duration := time.Since(start).Seconds()
			m.requestDuration.Record(ctx, duration, metric.WithAttributes(attrs...))
			if chunkCount > 0 {
				m.streamChunkCount.Add(ctx, chunkCount, metric.WithAttributes(attrs...))
			}

			if !hadError {
				if err := ctx.Err(); err != nil {
					hadError = true
					m.recordError(ctx, span, err, attrs)
					m.successRateWindow.Record(false)
				}
			}

			if !hadError {
				m.successRateWindow.Record(true)
			}

			if usage != nil {
				m.recordUsage(ctx, span, provider, model, *usage, duration, attrs)
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
				span.SetStatus(codes.Ok, "")
			}
		}()

		for chunk := range stream {
			chunkCount++
			sendResult := sender.Send(chunk)
			if sender.ForwardTerminalOnEarlyExit(sendResult) {
				hadError = true
				m.recordError(ctx, span, streamSendResultError(ctx, sendResult), attrs)
				m.successRateWindow.Record(false)
				return
			}

			if !hadFirstToken && chunk.Content != "" {
				hadFirstToken = true
				firstTokenTime = time.Now()
				ttft := firstTokenTime.Sub(start).Seconds()
				m.timeToFirstToken.Record(ctx, ttft, metric.WithAttributes(attrs...))
				span.SetAttributes(attribute.Float64("llm.stream.time_to_first_token", ttft))
			}

			if chunk.Error != nil {
				hadError = true
				m.recordError(ctx, span, chunk.Error, attrs)
				m.successRateWindow.Record(false)
				return
			}

			// Limit captured content to prevent unbounded memory growth.
			if !contentTruncated && contentBuilder.Len() < maxMetricsContentCapture {
				remaining := maxMetricsContentCapture - contentBuilder.Len()
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
func (m *MetricsMiddleware) Provider() llms.Provider {
	return m.llm.Provider()
}

// Model returns the underlying LLM's model
func (m *MetricsMiddleware) Model() string {
	return m.llm.Model()
}

// Unwrap returns the underlying LLM
func (m *MetricsMiddleware) Unwrap() llms.LLM {
	return m.llm
}

// CostTracker returns the cost tracker for usage statistics
func (m *MetricsMiddleware) CostTracker() *llms.CostTracker {
	return m.costTracker
}

// SuccessRate returns the current success rate over the configured window
func (m *MetricsMiddleware) SuccessRate() float64 {
	return m.successRateWindow.SuccessRate()
}

// ActiveRequests returns the current number of active requests
func (m *MetricsMiddleware) ActiveRequests() int64 {
	return m.activeReqsCount.Load()
}

// Ensure MetricsMiddleware implements LLM
var _ llms.LLM = (*MetricsMiddleware)(nil)
