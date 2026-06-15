package llms

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestMetricsMiddleware_RecordUsageUsesCustomCostTrackerPricing(t *testing.T) {
	reader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(reader))
	meter := meterProvider.Meter(InstrumentationName)

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer(InstrumentationName)

	tracker := NewCostTracker(map[string]Pricing{
		"openai:custom-model": {PromptPerMillion: 42.00, CompletionPerMillion: 84.00},
	})
	llm := &mockMetricsLLM{
		provider: ProviderOpenAI,
		model:    "custom-model",
		genResp: &Response{
			Content: "test",
			Usage:   Usage{PromptTokens: 1_000_000, TotalTokens: 1_000_000},
		},
	}

	middleware, err := NewMetricsMiddleware(llm,
		WithMetricsMeter(meter),
		WithMetricsTracer(tracer),
		WithMetricsCostTracker(tracker),
	)
	if err != nil {
		t.Fatalf("failed to create middleware: %v", err)
	}

	_, err = middleware.GenerateContent(context.Background(), []Message{{Role: RoleUser, Content: "test"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := tracker.GetTotalCost(); got != 42.00 {
		t.Fatalf("tracker cost = %f, want 42.00", got)
	}

	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	assertMetricsAttribute(t, spans[0].Attributes(), "llm.cost.estimate", 42.00)

	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("failed to collect metrics: %v", err)
	}
	if got := findFloat64MetricValue(t, data, metricCostEstimate); got != 42.00 {
		t.Errorf("%s = %f, want 42.00", metricCostEstimate, got)
	}
}

func findFloat64MetricValue(t *testing.T, data metricdata.ResourceMetrics, name string) float64 {
	t.Helper()
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[float64])
			if !ok {
				t.Fatalf("%s has data type %T, want metricdata.Sum[float64]", name, m.Data)
			}
			var total float64
			for _, point := range sum.DataPoints {
				total += point.Value
			}
			return total
		}
	}
	t.Fatalf("metric %s not found", name)
	return 0
}
