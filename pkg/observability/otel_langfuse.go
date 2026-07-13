package observability

import (
	"context"
	"strings"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v5"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	// LangfuseInstrumentationName is the instrumentation scope name for Langfuse-compatible spans
	LangfuseInstrumentationName = "github.com/nocturnium/llm-go-sdk/v5/langfuse"

	// Default content capture limits
	defaultMaxInputCapture  = 100_000
	defaultMaxOutputCapture = 100_000
)

// LangfuseOTelMiddleware extends OTelMiddleware with Langfuse-compatible attributes.
// It uses OpenTelemetry GenAI semantic conventions that Langfuse recognizes.
type LangfuseOTelMiddleware struct {
	llm    llms.LLM
	tracer trace.Tracer
	meter  metric.Meter

	// Metrics using GenAI conventions
	requestCounter   metric.Int64Counter
	promptTokens     metric.Int64Counter
	completionTokens metric.Int64Counter
	requestDuration  metric.Float64Histogram
	costEstimate     metric.Float64Counter
	timeToFirstToken metric.Float64Histogram
	errorCounter     metric.Int64Counter

	// Options
	captureInput  bool
	captureOutput bool
	maxInputLen   int
	maxOutputLen  int
	inputFormat   InputFormat
	outputFormat  OutputFormat
	costTracker   *llms.CostTracker
}

// LangfuseOTelOption configures the LangfuseOTelMiddleware
type LangfuseOTelOption func(*LangfuseOTelMiddleware)

// NewLangfuseOTelMiddleware creates a new Langfuse-compatible OpenTelemetry middleware
func NewLangfuseOTelMiddleware(llm llms.LLM, opts ...LangfuseOTelOption) (*LangfuseOTelMiddleware, error) {
	m := &LangfuseOTelMiddleware{
		llm:    llm,
		tracer: otel.Tracer(LangfuseInstrumentationName),
		meter:  otel.Meter(LangfuseInstrumentationName),
		// Privacy-safe by default: prompts/responses are NOT captured unless the
		// caller explicitly opts in via WithLangfuseInputCapture /
		// WithLangfuseOutputCapture. This matches the generic OTel middleware
		// (recordContent:false) and the metrics middleware.
		captureInput:  false,
		captureOutput: false,
		maxInputLen:   defaultMaxInputCapture,
		maxOutputLen:  defaultMaxOutputCapture,
		inputFormat:   InputFormatMessages,
		outputFormat:  OutputFormatStructured,
	}

	for _, opt := range opts {
		opt(m)
	}

	if err := m.initMetrics(); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *LangfuseOTelMiddleware) initMetrics() error {
	var err error

	m.requestCounter, err = m.meter.Int64Counter(
		MetricGenAIRequests,
		metric.WithDescription("Number of GenAI requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return err
	}

	m.promptTokens, err = m.meter.Int64Counter(
		MetricGenAITokensPrompt,
		metric.WithDescription("Number of prompt tokens used"),
		metric.WithUnit("{token}"),
	)
	if err != nil {
		return err
	}

	m.completionTokens, err = m.meter.Int64Counter(
		MetricGenAITokensCompletion,
		metric.WithDescription("Number of completion tokens generated"),
		metric.WithUnit("{token}"),
	)
	if err != nil {
		return err
	}

	m.requestDuration, err = m.meter.Float64Histogram(
		MetricGenAIDuration,
		metric.WithDescription("Duration of GenAI requests"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}

	m.costEstimate, err = m.meter.Float64Counter(
		MetricGenAICost,
		metric.WithDescription("Estimated cost of GenAI requests"),
		metric.WithUnit("{USD}"),
	)
	if err != nil {
		return err
	}

	m.timeToFirstToken, err = m.meter.Float64Histogram(
		MetricGenAITimeToFirstToken,
		metric.WithDescription("Time to first token for streaming requests"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}

	m.errorCounter, err = m.meter.Int64Counter(
		"gen_ai.client.errors",
		metric.WithDescription("Number of GenAI errors"),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		return err
	}

	return nil
}

// Configuration options

// WithLangfuseInputCapture enables/disables input capture and sets max length
func WithLangfuseInputCapture(enabled bool, maxLen int) LangfuseOTelOption {
	return func(m *LangfuseOTelMiddleware) {
		m.captureInput = enabled
		if maxLen > 0 {
			m.maxInputLen = maxLen
		}
	}
}

// WithLangfuseOutputCapture enables/disables output capture and sets max length
func WithLangfuseOutputCapture(enabled bool, maxLen int) LangfuseOTelOption {
	return func(m *LangfuseOTelMiddleware) {
		m.captureOutput = enabled
		if maxLen > 0 {
			m.maxOutputLen = maxLen
		}
	}
}

// WithLangfuseInputFormat sets the format for input capture
func WithLangfuseInputFormat(format InputFormat) LangfuseOTelOption {
	return func(m *LangfuseOTelMiddleware) {
		m.inputFormat = format
	}
}

// WithLangfuseOutputFormat sets the format for output capture
func WithLangfuseOutputFormat(format OutputFormat) LangfuseOTelOption {
	return func(m *LangfuseOTelMiddleware) {
		m.outputFormat = format
	}
}

// WithLangfuseTracer sets a custom tracer
func WithLangfuseTracer(tracer trace.Tracer) LangfuseOTelOption {
	return func(m *LangfuseOTelMiddleware) {
		m.tracer = tracer
	}
}

// WithLangfuseMeter sets a custom meter
func WithLangfuseMeter(meter metric.Meter) LangfuseOTelOption {
	return func(m *LangfuseOTelMiddleware) {
		m.meter = meter
	}
}

// WithLangfuseCostTracker enables cost tracking
func WithLangfuseCostTracker(tracker *llms.CostTracker) LangfuseOTelOption {
	return func(m *LangfuseOTelMiddleware) {
		m.costTracker = tracker
	}
}

// Call wraps the LLM's Call method with Langfuse-compatible tracing
func (m *LangfuseOTelMiddleware) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	opts := llms.ApplyOptions(options...)
	tc := m.resolveTraceContext(ctx, opts)

	ctx, span := m.tracer.Start(ctx, "llm.generation",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	start := time.Now()

	// Set Langfuse and GenAI attributes
	m.setLangfuseAttributes(span, tc)
	m.setGenAIRequestAttributes(span, opts)

	// Capture input
	if m.captureInput {
		inputStr := truncateForSpan(prompt, m.maxInputLen)
		span.SetAttributes(
			keyGenAIPrompt.String(inputStr),
			keyInputValue.String(inputStr),
		)
	}

	// Record request metric
	attrs := m.getMetricAttributes(opts)
	m.requestCounter.Add(ctx, 1, metric.WithAttributes(attrs...))

	result, err := llms.Call(ctx, m.llm, prompt, options...)

	duration := time.Since(start)
	m.requestDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))

	if err != nil {
		m.recordError(ctx, span, err, attrs)
		return "", err
	}

	// Capture output
	if m.captureOutput {
		outputStr := truncateForSpan(result, m.maxOutputLen)
		span.SetAttributes(
			keyGenAICompletion.String(outputStr),
			keyOutputValue.String(outputStr),
		)
	}

	span.SetStatus(codes.Ok, "")
	return result, nil
}

// GenerateContent wraps the LLM's GenerateContent method with Langfuse-compatible tracing
func (m *LangfuseOTelMiddleware) GenerateContent(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.Response, error) {
	opts := llms.ApplyOptions(options...)
	tc := m.resolveTraceContext(ctx, opts)

	ctx, span := m.tracer.Start(ctx, "llm.generation",
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	start := time.Now()

	// Set Langfuse and GenAI attributes
	m.setLangfuseAttributes(span, tc)
	m.setGenAIRequestAttributes(span, opts)

	// Capture input
	if m.captureInput {
		inputStr := FormatInput(messages, m.inputFormat)
		span.SetAttributes(
			keyGenAIPrompt.String(truncateForSpan(inputStr, m.maxInputLen)),
			keyInputValue.String(truncateForSpan(inputStr, m.maxInputLen)),
		)
	}

	// Record request metric
	attrs := m.getMetricAttributes(opts)
	m.requestCounter.Add(ctx, 1, metric.WithAttributes(attrs...))

	resp, err := m.llm.GenerateContent(ctx, messages, options...)

	duration := time.Since(start)
	m.requestDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))

	if err != nil {
		m.recordError(ctx, span, err, attrs)
		return nil, err
	}

	// Set response attributes
	m.setGenAIResponseAttributes(span, resp, duration)

	// Record token metrics
	if resp.Usage.TotalTokens > 0 {
		m.promptTokens.Add(ctx, int64(resp.Usage.PromptTokens), metric.WithAttributes(attrs...))
		m.completionTokens.Add(ctx, int64(resp.Usage.CompletionTokens), metric.WithAttributes(attrs...))
	}

	// Record cost if tracker available
	if m.costTracker != nil {
		cost, known := m.costTracker.Record(m.llm.Provider(), m.resolveModel(opts), resp.Usage)
		if known {
			m.costEstimate.Add(ctx, cost, metric.WithAttributes(attrs...))
			span.SetAttributes(keyGenAIUsageCost.Float64(cost))
		}
	}

	// Capture output
	if m.captureOutput {
		outputStr := FormatOutput(resp, m.outputFormat)
		span.SetAttributes(
			keyGenAICompletion.String(truncateForSpan(outputStr, m.maxOutputLen)),
			keyOutputValue.String(truncateForSpan(outputStr, m.maxOutputLen)),
		)
	}

	span.SetStatus(codes.Ok, "")
	return resp, nil
}

// Stream wraps the LLM's Stream method with Langfuse-compatible tracing
func (m *LangfuseOTelMiddleware) Stream(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	opts := llms.ApplyOptions(options...)
	tc := m.resolveTraceContext(ctx, opts)

	ctx, span := m.tracer.Start(ctx, "llm.generation",
		trace.WithSpanKind(trace.SpanKindClient),
	)

	start := time.Now()

	// Set Langfuse and GenAI attributes
	m.setLangfuseAttributes(span, tc)
	m.setGenAIRequestAttributes(span, opts)
	span.SetAttributes(attribute.Bool("gen_ai.streaming", true))

	// Capture input
	if m.captureInput {
		inputStr := FormatInput(messages, m.inputFormat)
		span.SetAttributes(
			keyGenAIPrompt.String(truncateForSpan(inputStr, m.maxInputLen)),
			keyInputValue.String(truncateForSpan(inputStr, m.maxInputLen)),
		)
	}

	// Record request metric
	attrs := m.getMetricAttributes(opts)
	m.requestCounter.Add(ctx, 1, metric.WithAttributes(attrs...))

	stream, err := m.llm.Stream(ctx, messages, options...)
	if err != nil {
		duration := time.Since(start)
		m.requestDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))
		m.recordError(ctx, span, err, attrs)
		span.End()
		return nil, err
	}

	wrappedStream := make(chan llms.StreamChunk, opts.StreamBufferSize)
	sender := llms.NewStreamSender(ctx, wrappedStream, opts.StreamSendTimeout)

	go func() {
		defer close(wrappedStream)
		defer span.End()

		var contentBuilder strings.Builder
		var contentTruncated bool
		var usage *llms.Usage
		var finishReason string
		var toolCallCount int
		var hadError bool
		var firstChunkReceived bool
		var ttft time.Duration

		contentBuilder.Grow(1024)

		defer func() {
			duration := time.Since(start)
			m.requestDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(attrs...))

			if ttft > 0 {
				m.timeToFirstToken.Record(ctx, ttft.Seconds(), metric.WithAttributes(attrs...))
				span.SetAttributes(keyGenAITimeToFirstToken.Float64(ttft.Seconds()))
			}

			if usage != nil {
				m.promptTokens.Add(ctx, int64(usage.PromptTokens), metric.WithAttributes(attrs...))
				m.completionTokens.Add(ctx, int64(usage.CompletionTokens), metric.WithAttributes(attrs...))

				span.SetAttributes(
					keyGenAIUsagePromptTokens.Int(usage.PromptTokens),
					keyGenAIUsageCompletionTokens.Int(usage.CompletionTokens),
					keyGenAIUsageTotalTokens.Int(usage.TotalTokens),
				)

				if m.costTracker != nil {
					cost, known := m.costTracker.Record(m.llm.Provider(), m.resolveModel(opts), *usage)
					if known {
						m.costEstimate.Add(ctx, cost, metric.WithAttributes(attrs...))
						span.SetAttributes(keyGenAIUsageCost.Float64(cost))
					}
				}
			}

			span.SetAttributes(keyGenAIResponseFinishReason.String(finishReason))
			if toolCallCount > 0 {
				span.SetAttributes(attribute.Int("gen_ai.tool_calls", toolCallCount))
			}

			if m.captureOutput && contentBuilder.Len() > 0 {
				outputStr := contentBuilder.String()
				span.SetAttributes(
					keyGenAICompletion.String(truncateForSpan(outputStr, m.maxOutputLen)),
					keyOutputValue.String(truncateForSpan(outputStr, m.maxOutputLen)),
				)
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
			if !firstChunkReceived && chunk.Content != "" {
				firstChunkReceived = true
				ttft = time.Since(start)
			}

			// On early exit a terminal chunk is forwarded so the consumer never
			// sees a silent close.
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

			if !contentTruncated && contentBuilder.Len() < m.maxOutputLen {
				remaining := m.maxOutputLen - contentBuilder.Len()
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
func (m *LangfuseOTelMiddleware) Provider() llms.Provider {
	return m.llm.Provider()
}

// Model returns the underlying LLM's model
func (m *LangfuseOTelMiddleware) Model() string {
	return m.llm.Model()
}

// Unwrap returns the underlying LLM
func (m *LangfuseOTelMiddleware) Unwrap() llms.LLM {
	return m.llm
}

// Helper methods

func (m *LangfuseOTelMiddleware) resolveTraceContext(ctx context.Context, opts *llms.CallOptions) *TraceContext {
	tc := GetTraceContext(ctx)
	if tc == nil {
		tc = &TraceContext{}
	} else {
		tc = tc.Clone()
	}

	if opts.Trace == nil {
		return tc
	}

	// Override with call options if provided
	if opts.Trace.TraceID != "" {
		tc.TraceID = opts.Trace.TraceID
	}
	if opts.Trace.SpanID != "" {
		tc.SpanID = opts.Trace.SpanID
	}
	if opts.Trace.ParentID != "" {
		tc.ParentID = opts.Trace.ParentID
	}
	if opts.Trace.UserID != "" {
		tc.UserID = opts.Trace.UserID
	}
	if opts.Trace.SessionID != "" {
		tc.SessionID = opts.Trace.SessionID
	}
	if len(opts.Trace.Tags) > 0 {
		tc.Tags = append(tc.Tags, opts.Trace.Tags...)
	}
	if opts.Trace.Version != "" {
		tc.Version = opts.Trace.Version
	}
	if len(opts.Trace.Metadata) > 0 {
		if tc.Metadata == nil {
			tc.Metadata = make(map[string]string)
		}
		for k, v := range opts.Trace.Metadata {
			value, ok := v.(string)
			if !ok {
				continue
			}
			if IsValidMetadataKey(k) && ValidateMetadataValue(value) {
				tc.Metadata[k] = value
			}
		}
	}

	return tc
}

func (m *LangfuseOTelMiddleware) setLangfuseAttributes(span trace.Span, tc *TraceContext) {
	if tc == nil {
		return
	}

	if tc.TraceID != "" {
		span.SetAttributes(keyLangfuseTraceID.String(tc.TraceID))
	}
	if tc.SpanID != "" {
		span.SetAttributes(keyLangfuseObservationID.String(tc.SpanID))
	}
	if tc.ParentID != "" {
		span.SetAttributes(keyLangfuseParentObservationID.String(tc.ParentID))
	}
	if tc.UserID != "" {
		span.SetAttributes(keyLangfuseUserID.String(tc.UserID))
	}
	if tc.SessionID != "" {
		span.SetAttributes(keyLangfuseSessionID.String(tc.SessionID))
	}
	if len(tc.Tags) > 0 {
		span.SetAttributes(keyLangfuseTags.StringSlice(tc.Tags))
	}
	if tc.Version != "" {
		span.SetAttributes(keyLangfuseVersion.String(tc.Version))
	}
	if tc.Release != "" {
		span.SetAttributes(keyLangfuseRelease.String(tc.Release))
	}
	if tc.Environment != "" {
		span.SetAttributes(keyLangfuseEnvironment.String(tc.Environment))
	}

	// Set propagated metadata as span attributes
	for k, v := range tc.Metadata {
		span.SetAttributes(attribute.String("langfuse.metadata."+k, v))
	}
}

func (m *LangfuseOTelMiddleware) setGenAIRequestAttributes(span trace.Span, opts *llms.CallOptions) {
	model := m.resolveModel(opts)

	span.SetAttributes(
		keyGenAISystem.String(ProviderToGenAISystem(m.llm.Provider())),
		keyGenAIRequestModel.String(model),
		keyGenAIOperationName.String("chat"),
	)

	if opts.Temperature != nil {
		span.SetAttributes(keyGenAIRequestTemperature.Float64(*opts.Temperature))
	}
	if opts.MaxTokens != nil {
		span.SetAttributes(keyGenAIRequestMaxTokens.Int(*opts.MaxTokens))
	}
	if opts.TopP != nil {
		span.SetAttributes(keyGenAIRequestTopP.Float64(*opts.TopP))
	}
	if opts.FrequencyPenalty != nil {
		span.SetAttributes(keyGenAIRequestFrequencyPenalty.Float64(*opts.FrequencyPenalty))
	}
	if opts.PresencePenalty != nil {
		span.SetAttributes(keyGenAIRequestPresencePenalty.Float64(*opts.PresencePenalty))
	}
}

func (m *LangfuseOTelMiddleware) setGenAIResponseAttributes(span trace.Span, resp *llms.Response, duration time.Duration) {
	span.SetAttributes(
		keyGenAIResponseModel.String(m.llm.Model()),
		keyGenAIResponseFinishReason.String(string(resp.FinishReason)),
	)

	if resp.Usage.TotalTokens > 0 {
		span.SetAttributes(
			keyGenAIUsagePromptTokens.Int(resp.Usage.PromptTokens),
			keyGenAIUsageCompletionTokens.Int(resp.Usage.CompletionTokens),
			keyGenAIUsageTotalTokens.Int(resp.Usage.TotalTokens),
		)

		// Calculate tokens per second
		if duration.Seconds() > 0 && resp.Usage.CompletionTokens > 0 {
			tps := float64(resp.Usage.CompletionTokens) / duration.Seconds()
			span.SetAttributes(keyGenAITokensPerSecond.Float64(tps))
		}
	}

	if len(resp.ToolCalls) > 0 {
		span.SetAttributes(attribute.Int("gen_ai.tool_calls", len(resp.ToolCalls)))
	}
}

func (m *LangfuseOTelMiddleware) resolveModel(opts *llms.CallOptions) string {
	if opts.Model != "" {
		return opts.Model
	}
	return m.llm.Model()
}

func (m *LangfuseOTelMiddleware) getMetricAttributes(opts *llms.CallOptions) []attribute.KeyValue {
	return []attribute.KeyValue{
		keyGenAISystem.String(ProviderToGenAISystem(m.llm.Provider())),
		keyGenAIRequestModel.String(m.resolveModel(opts)),
	}
}

func (m *LangfuseOTelMiddleware) recordError(ctx context.Context, span trace.Span, err error, attrs []attribute.KeyValue) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	m.errorCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// Ensure LangfuseOTelMiddleware implements LLM and Wrapper
var (
	_ llms.LLM     = (*LangfuseOTelMiddleware)(nil)
	_ llms.Wrapper = (*LangfuseOTelMiddleware)(nil)
)
