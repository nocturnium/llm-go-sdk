package observability

import (
	"context"
	"errors"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v5"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// mockMetricsLLM is a mock LLM for testing metrics middleware
type mockMetricsLLM struct {
	provider    llms.Provider
	model       string
	callResp    string
	callErr     error
	genResp     *llms.Response
	genErr      error
	chunks      []llms.StreamChunk
	streamErr   error
	streamPanic any
	delay       time.Duration
}

func (m *mockMetricsLLM) Call(ctx context.Context, _ string, _ ...llms.CallOption) (string, error) {
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(m.delay):
		}
	}
	if m.callErr != nil {
		return "", m.callErr
	}
	return m.callResp, nil
}

func (m *mockMetricsLLM) GenerateContent(ctx context.Context, _ []llms.Message, _ ...llms.CallOption) (*llms.Response, error) {
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.delay):
		}
	}
	if m.genErr != nil {
		return nil, m.genErr
	}
	return m.genResp, nil
}

func (m *mockMetricsLLM) Stream(ctx context.Context, _ []llms.Message, _ ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	if m.streamPanic != nil {
		panic(m.streamPanic)
	}
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	ch := make(chan llms.StreamChunk)
	go func() {
		defer close(ch)
		for _, chunk := range m.chunks {
			if m.delay > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(m.delay):
				}
			}
			ch <- chunk
		}
	}()
	return ch, nil
}

func (m *mockMetricsLLM) Provider() llms.Provider { return m.provider }
func (m *mockMetricsLLM) Model() string           { return m.model }

func waitForMetricsSpans(t *testing.T, recorder *tracetest.SpanRecorder, want int) []sdktrace.ReadOnlySpan {
	t.Helper()

	deadline := time.After(500 * time.Millisecond)
	for {
		spans := recorder.Ended()
		if len(spans) >= want {
			return spans
		}

		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d ended span(s), got %d", want, len(spans))
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestNewMetricsMiddleware(t *testing.T) {
	llm := &mockMetricsLLM{provider: llms.ProviderOpenAI, model: "gpt-4"}

	middleware, err := NewMetricsMiddleware(llm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if middleware.llm != llm {
		t.Error("llm not set correctly")
	}
	if middleware.tracer == nil {
		t.Error("tracer should not be nil")
	}
	if middleware.meter == nil {
		t.Error("meter should not be nil")
	}
	if middleware.costTracker == nil {
		t.Error("costTracker should not be nil")
	}
}

func TestMetricsMiddleware_Call_Success(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer(InstrumentationName)

	llm := &mockMetricsLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4o",
		genResp: &llms.Response{
			Content: "Hello!",
			Usage:   llms.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
	}

	middleware, err := NewMetricsMiddleware(llm, WithMetricsTracer(tracer))
	if err != nil {
		t.Fatalf("failed to create middleware: %v", err)
	}

	result, err := middleware.Call(context.Background(), "Hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello!" {
		t.Errorf("result = %s, want 'Hello!'", result)
	}

	// Check spans
	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if span.Name() != "llm.call" {
		t.Errorf("span name = %s, want 'llm.call'", span.Name())
	}
	if span.Status().Code != codes.Ok {
		t.Errorf("span status = %v, want Ok", span.Status().Code)
	}

	// Check cost tracking
	usage := middleware.CostTracker().GetUsage(llms.ProviderOpenAI, "gpt-4o")
	if usage == nil {
		t.Fatal("expected usage to be tracked")
	}
	if usage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", usage.PromptTokens)
	}
}

func TestMetricsMiddleware_GenerateContent_Success(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer(InstrumentationName)

	llm := &mockMetricsLLM{
		provider: llms.ProviderAnthropic,
		model:    "claude-3",
		genResp: &llms.Response{
			Content:      "Generated response",
			FinishReason: "stop",
			Usage: llms.Usage{
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
			},
		},
	}

	middleware, err := NewMetricsMiddleware(llm, WithMetricsTracer(tracer))
	if err != nil {
		t.Fatalf("failed to create middleware: %v", err)
	}

	resp, err := middleware.GenerateContent(context.Background(), []llms.Message{
		{Role: llms.RoleUser, Content: "Hello"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Generated response" {
		t.Errorf("content = %s, want 'Generated response'", resp.Content)
	}

	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	attrs := spans[0].Attributes()
	assertMetricsAttribute(t, attrs, "llm.tokens.prompt", int64(100))
	assertMetricsAttribute(t, attrs, "llm.tokens.completion", int64(50))
}

func TestMetricsMiddleware_Stream_TimeToFirstToken(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer(InstrumentationName)

	llm := &mockMetricsLLM{
		provider: llms.ProviderGemini,
		model:    "gemini-pro",
		delay:    10 * time.Millisecond,
		chunks: []llms.StreamChunk{
			{Content: "Hello"},
			{Content: " World"},
			{FinishReason: "stop", Usage: &llms.Usage{PromptTokens: 10, CompletionTokens: 5}},
		},
	}

	middleware, err := NewMetricsMiddleware(llm, WithMetricsTracer(tracer))
	if err != nil {
		t.Fatalf("failed to create middleware: %v", err)
	}

	stream, err := middleware.Stream(context.Background(), []llms.Message{
		{Role: llms.RoleUser, Content: "Hi"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Consume stream
	var content string
	for chunk := range stream {
		content += chunk.Content
	}

	if content != "Hello World" {
		t.Errorf("content = %s, want 'Hello World'", content)
	}

	spans := waitForMetricsSpans(t, spanRecorder, 1)
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	// Check that time to first token was recorded
	attrs := spans[0].Attributes()
	var foundTTFT bool
	for _, attr := range attrs {
		if string(attr.Key) == "llm.stream.time_to_first_token" {
			foundTTFT = true
			if attr.Value.AsFloat64() <= 0 {
				t.Error("time_to_first_token should be positive")
			}
			break
		}
	}
	if !foundTTFT {
		t.Error("expected time_to_first_token attribute")
	}
}

func TestMetricsMiddleware_Stream_AbandonedConsumerDecrementsActiveRequests(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer(InstrumentationName)

	llm := &mockMetricsLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		chunks: []llms.StreamChunk{
			{Content: "one"},
			{Content: "two"},
			{Content: "three"},
		},
	}

	middleware, err := NewMetricsMiddleware(llm, WithMetricsTracer(tracer))
	if err != nil {
		t.Fatalf("failed to create middleware: %v", err)
	}

	stream, err := middleware.Stream(context.Background(),
		[]llms.Message{{Role: llms.RoleUser, Content: "Hi"}},
		llms.WithStreamBufferSize(1),
		llms.WithStreamSendTimeout(10*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stream == nil {
		t.Fatal("expected stream")
	}

	deadline := time.After(500 * time.Millisecond)
	for middleware.ActiveRequests() != 0 {
		select {
		case <-deadline:
			t.Fatalf("active requests = %d, want 0", middleware.ActiveRequests())
		case <-time.After(5 * time.Millisecond):
		}
	}

	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status().Code != codes.Error {
		t.Errorf("span status = %v, want Error", spans[0].Status().Code)
	}
	if middleware.SuccessRate() != 0 {
		t.Errorf("success rate = %f, want 0", middleware.SuccessRate())
	}
}

func TestMetricsMiddleware_Stream_PanicDecrementsActiveRequests(t *testing.T) {
	llm := &mockMetricsLLM{
		provider:    llms.ProviderOpenAI,
		model:       "gpt-4",
		streamPanic: "stream panic",
	}

	middleware, err := NewMetricsMiddleware(llm)
	if err != nil {
		t.Fatalf("failed to create middleware: %v", err)
	}
	before := middleware.ActiveRequests()

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("expected panic")
		}
		if got := middleware.ActiveRequests(); got != before {
			t.Fatalf("active requests after panic = %d, want %d", got, before)
		}
	}()

	_, _ = middleware.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "Hi"}})
}

func TestMetricsMiddleware_Stream_SuccessDecrementsActiveRequestsOnce(t *testing.T) {
	llm := &mockMetricsLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		chunks: []llms.StreamChunk{
			{Content: "one"},
			{Content: "two"},
			{Done: true, FinishReason: "stop"},
		},
	}

	middleware, err := NewMetricsMiddleware(llm)
	if err != nil {
		t.Fatalf("failed to create middleware: %v", err)
	}
	before := middleware.ActiveRequests()

	stream, err := middleware.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "Hi"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := middleware.ActiveRequests(); got != before+1 {
		t.Fatalf("active requests after stream start = %d, want %d", got, before+1)
	}

	var chunks int
	for range stream {
		chunks++
	}
	t.Logf("drained %d chunks", chunks)

	deadline := time.Now().Add(time.Second)
	for {
		if got := middleware.ActiveRequests(); got == before {
			return
		} else if time.Now().After(deadline) {
			t.Fatalf("active requests after stream drain = %d, want %d", got, before)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMetricsMiddleware_Error(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer(InstrumentationName)

	expectedErr := &llms.APIError{
		StatusCode: 429,
		Message:    "rate limited",
		Type:       "rate_limit_error",
	}

	llm := &mockMetricsLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		genErr:   expectedErr,
	}

	middleware, _ := NewMetricsMiddleware(llm, WithMetricsTracer(tracer))
	_, err := middleware.GenerateContent(context.Background(), []llms.Message{
		{Role: llms.RoleUser, Content: "Hi"},
	})

	if !errors.Is(err, expectedErr) {
		t.Errorf("error = %v, want %v", err, expectedErr)
	}

	spans := spanRecorder.Ended()
	if spans[0].Status().Code != codes.Error {
		t.Error("expected error status on span")
	}

	// Success rate should decrease
	if middleware.SuccessRate() != 0.0 {
		t.Errorf("success rate = %f, want 0.0", middleware.SuccessRate())
	}
}

func TestMetricsMiddleware_SuccessRate(t *testing.T) {
	llm := &mockMetricsLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
	}

	middleware, _ := NewMetricsMiddleware(llm, WithSuccessRateWindow(1*time.Minute))

	// Initial success rate should be 1.0
	if middleware.SuccessRate() != 1.0 {
		t.Errorf("initial success rate = %f, want 1.0", middleware.SuccessRate())
	}

	// Make some successful requests
	for i := 0; i < 3; i++ {
		llm.genResp = &llms.Response{Content: "ok"}
		_, _ = middleware.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "test"}})
	}

	if middleware.SuccessRate() != 1.0 {
		t.Errorf("success rate after 3 successes = %f, want 1.0", middleware.SuccessRate())
	}

	// Make a failed request
	llm.genErr = errors.New("error")
	_, _ = middleware.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "test"}})

	// 3 successes, 1 failure = 0.75
	rate := middleware.SuccessRate()
	if rate != 0.75 {
		t.Errorf("success rate = %f, want 0.75", rate)
	}
}

func TestMetricsMiddleware_ActiveRequests(t *testing.T) {
	llm := &mockMetricsLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		delay:    50 * time.Millisecond,
		genResp:  &llms.Response{Content: "ok"},
	}

	middleware, _ := NewMetricsMiddleware(llm)

	// Start multiple concurrent requests
	done := make(chan struct{})
	for i := 0; i < 3; i++ {
		go func() {
			_, _ = middleware.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "test"}})
			done <- struct{}{}
		}()
	}

	// Wait a bit for requests to start
	time.Sleep(10 * time.Millisecond)

	active := middleware.ActiveRequests()
	if active != 3 {
		t.Errorf("active requests = %d, want 3", active)
	}

	// Wait for all to complete
	for i := 0; i < 3; i++ {
		<-done
	}

	active = middleware.ActiveRequests()
	if active != 0 {
		t.Errorf("active requests after completion = %d, want 0", active)
	}
}

func TestMetricsMiddleware_CostRecording(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := meterProvider.Meter(InstrumentationName)

	llm := &mockMetricsLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4o",
		genResp: &llms.Response{
			Content: "test",
			Usage:   llms.Usage{PromptTokens: 1000000, CompletionTokens: 500000}, // 1M prompt, 0.5M completion
		},
	}

	middleware, _ := NewMetricsMiddleware(llm, WithMetricsMeter(meter), WithMetricsCostRecording(true))

	_, _ = middleware.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "test"}})

	// Collect metrics
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("failed to collect metrics: %v", err)
	}

	// Verify cost metric was recorded
	if len(data.ScopeMetrics) == 0 {
		t.Fatal("expected scope metrics")
	}

	found := false
	for _, m := range data.ScopeMetrics[0].Metrics {
		if m.Name == metricCostEstimate {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected cost estimate metric")
	}

	// Also verify cost tracker has the usage
	tracker := middleware.CostTracker()
	cost := tracker.GetTotalCost()
	// Expected: (1M/1M * 2.50) + (0.5M/1M * 10.00) = 2.50 + 5.00 = 7.50
	if cost != 7.50 {
		t.Errorf("total cost = %f, want 7.50", cost)
	}
}

func TestMetricsMiddleware_TokensPerSecond(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer(InstrumentationName)

	llm := &mockMetricsLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		delay:    50 * time.Millisecond, // Adds some latency
		genResp: &llms.Response{
			Content: "test",
			Usage:   llms.Usage{PromptTokens: 10, CompletionTokens: 100},
		},
	}

	middleware, _ := NewMetricsMiddleware(llm, WithMetricsTracer(tracer))

	_, _ = middleware.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "test"}})

	spans := spanRecorder.Ended()
	attrs := spans[0].Attributes()

	var foundTPS bool
	for _, attr := range attrs {
		if string(attr.Key) == "llm.tokens_per_second" {
			foundTPS = true
			tps := attr.Value.AsFloat64()
			// With 100 completion tokens and ~50ms delay, TPS should be around 2000
			if tps <= 0 {
				t.Error("tokens_per_second should be positive")
			}
			break
		}
	}
	if !foundTPS {
		t.Error("expected tokens_per_second attribute")
	}
}

func TestMetricsMiddleware_ContentRecording(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer(InstrumentationName)

	llm := &mockMetricsLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		genResp:  &llms.Response{Content: "response content"},
	}

	middleware, _ := NewMetricsMiddleware(llm,
		WithMetricsTracer(tracer),
		WithMetricsContentRecording(true),
	)

	_, _ = middleware.Call(context.Background(), "test prompt")

	spans := spanRecorder.Ended()
	attrs := spans[0].Attributes()

	assertMetricsAttribute(t, attrs, "llm.prompt", "test prompt")
	assertMetricsAttribute(t, attrs, "llm.response", "response content")
}

func TestMetricsMiddleware_GetProvider(t *testing.T) {
	llm := &mockMetricsLLM{provider: llms.ProviderGemini}
	middleware, _ := NewMetricsMiddleware(llm)

	if middleware.Provider() != llms.ProviderGemini {
		t.Errorf("provider = %s, want gemini", middleware.Provider())
	}
}

func TestMetricsMiddleware_GetModel(t *testing.T) {
	llm := &mockMetricsLLM{model: "test-model"}
	middleware, _ := NewMetricsMiddleware(llm)

	if middleware.Model() != "test-model" {
		t.Errorf("model = %s, want test-model", middleware.Model())
	}
}

func TestMetricsMiddleware_Unwrap(t *testing.T) {
	llm := &mockMetricsLLM{}
	middleware, _ := NewMetricsMiddleware(llm)

	if middleware.Unwrap() != llm {
		t.Error("Unwrap should return underlying LLM")
	}
}

func TestSlidingWindow(t *testing.T) {
	window := newSlidingWindow(100 * time.Millisecond)

	// Initial rate is 1.0
	if window.SuccessRate() != 1.0 {
		t.Errorf("initial rate = %f, want 1.0", window.SuccessRate())
	}

	// Record some successes
	window.Record(true)
	window.Record(true)
	if window.SuccessRate() != 1.0 {
		t.Errorf("after 2 successes = %f, want 1.0", window.SuccessRate())
	}

	// Record a failure
	window.Record(false)
	// 2 success, 1 failure = 0.666...
	rate := window.SuccessRate()
	if rate < 0.66 || rate > 0.67 {
		t.Errorf("after 1 failure = %f, want ~0.67", rate)
	}

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// Record new entries
	window.Record(true)
	// Old entries should be cleaned, only new success remains
	if window.SuccessRate() != 1.0 {
		t.Errorf("after window expiry = %f, want 1.0", window.SuccessRate())
	}
}

func TestMetricsMiddleware_WithCustomCostTracker(t *testing.T) {
	tracker := llms.NewCostTracker()
	llm := &mockMetricsLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		genResp:  &llms.Response{Content: "ok", Usage: llms.Usage{PromptTokens: 100}},
	}

	middleware, _ := NewMetricsMiddleware(llm, WithMetricsCostTracker(tracker))

	_, _ = middleware.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "test"}})

	// Verify the custom tracker was used
	if middleware.CostTracker() != tracker {
		t.Error("should use custom cost tracker")
	}
	if tracker.GetTotalRequests() != 1 {
		t.Error("custom tracker should have recorded the request")
	}
}

func assertMetricsAttribute(t *testing.T, attrs []attribute.KeyValue, key string, expected any) {
	t.Helper()
	for _, attr := range attrs {
		if string(attr.Key) == key {
			switch v := expected.(type) {
			case string:
				if attr.Value.AsString() != v {
					t.Errorf("attribute %s = %v, want %v", key, attr.Value.AsString(), v)
				}
			case int64:
				if attr.Value.AsInt64() != v {
					t.Errorf("attribute %s = %v, want %v", key, attr.Value.AsInt64(), v)
				}
			case bool:
				if attr.Value.AsBool() != v {
					t.Errorf("attribute %s = %v, want %v", key, attr.Value.AsBool(), v)
				}
			case float64:
				if attr.Value.AsFloat64() != v {
					t.Errorf("attribute %s = %v, want %v", key, attr.Value.AsFloat64(), v)
				}
			}
			return
		}
	}
	t.Errorf("attribute %s not found", key)
}
