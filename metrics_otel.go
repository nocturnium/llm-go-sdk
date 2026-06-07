package llms

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Enhanced metric names.
const (
	metricCostEstimate        = "llm.cost.estimate"
	metricTimeToFirstToken    = "llm.stream.time_to_first_token"
	metricTokensPerSecond     = "llm.tokens_per_second"
	metricActiveRequests      = "llm.active_requests"
	metricRequestsSuccessRate = "llm.success_rate"
)

// initMetrics initializes all OTel metrics for the middleware
func (m *MetricsMiddleware) initMetrics() error {
	var err error

	// Standard metrics
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

	// Enhanced metrics
	m.costEstimate, err = m.meter.Float64Counter(
		metricCostEstimate,
		metric.WithDescription("Estimated cost of LLM requests in USD"),
		metric.WithUnit("USD"),
	)
	if err != nil {
		return err
	}

	m.timeToFirstToken, err = m.meter.Float64Histogram(
		metricTimeToFirstToken,
		metric.WithDescription("Time until first token received in stream"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}

	m.tokensPerSecond, err = m.meter.Float64Histogram(
		metricTokensPerSecond,
		metric.WithDescription("Token generation throughput"),
		metric.WithUnit("{token}/s"),
	)
	if err != nil {
		return err
	}

	m.activeRequests, err = m.meter.Int64UpDownCounter(
		metricActiveRequests,
		metric.WithDescription("Number of currently active LLM requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return err
	}

	return nil
}

// recordUsage records token usage metrics and cost
func (m *MetricsMiddleware) recordUsage(ctx context.Context, span trace.Span, provider Provider, model string, usage Usage, duration float64, attrs []attribute.KeyValue) {
	m.promptTokens.Add(ctx, int64(usage.PromptTokens), metric.WithAttributes(attrs...))
	m.completionTokens.Add(ctx, int64(usage.CompletionTokens), metric.WithAttributes(attrs...))

	span.SetAttributes(
		attribute.Int("llm.tokens.prompt", usage.PromptTokens),
		attribute.Int("llm.tokens.completion", usage.CompletionTokens),
		attribute.Int("llm.tokens.total", usage.TotalTokens),
	)

	// Record tokens per second (throughput)
	if duration > 0 && usage.CompletionTokens > 0 {
		tps := float64(usage.CompletionTokens) / duration
		m.tokensPerSecond.Record(ctx, tps, metric.WithAttributes(attrs...))
		span.SetAttributes(attribute.Float64("llm.tokens_per_second", tps))
	}

	// Track and record cost
	if m.recordCost {
		m.costTracker.Record(provider, model, usage)
		cost := EstimateCost(provider, model, usage)
		if cost > 0 {
			m.costEstimate.Add(ctx, cost, metric.WithAttributes(attrs...))
			span.SetAttributes(attribute.Float64("llm.cost.estimate", cost))
		}
	}
}

// recordError records error metrics and span status
func (m *MetricsMiddleware) recordError(ctx context.Context, span trace.Span, err error, attrs []attribute.KeyValue) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())

	errorType := "unknown"
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		errorType = apiErr.Type
		if errorType == "" {
			errorType = apiErr.Code
		}
		span.SetAttributes(
			attribute.Int("llm.error.status_code", apiErr.StatusCode),
			attrErrorType.String(errorType),
		)
	}

	attrs = append(attrs, attrErrorType.String(errorType))
	m.errorCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// incrementActive increments the active request counter
func (m *MetricsMiddleware) incrementActive(ctx context.Context, attrs []attribute.KeyValue) {
	// Use atomic operation to prevent race conditions.
	// The atomic increment and metric update are now consistent operations.
	m.activeReqsCount.Add(1)
	m.activeRequests.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// decrementActive decrements the active request counter
func (m *MetricsMiddleware) decrementActive(ctx context.Context, attrs []attribute.KeyValue) {
	// Use atomic operation to prevent race conditions.
	m.activeReqsCount.Add(-1)
	m.activeRequests.Add(ctx, -1, metric.WithAttributes(attrs...))
}
