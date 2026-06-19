package observability

import (
	"context"
	"errors"
	"testing"
	"time"
	"unicode/utf8"

	llms "github.com/nocturnium/llm-go-sdk/v3"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const (
	testOtelHelloWorld = "Hello World"
)

// mockOTelLLM is a mock LLM for testing OTel middleware
type mockOTelLLM struct {
	provider  llms.Provider
	model     string
	callResp  string
	callErr   error
	genResp   *llms.Response
	genErr    error
	chunks    []llms.StreamChunk
	streamErr error
}

func (m *mockOTelLLM) Call(_ context.Context, _ string, _ ...llms.CallOption) (string, error) {
	if m.callErr != nil {
		return "", m.callErr
	}
	return m.callResp, nil
}

func (m *mockOTelLLM) GenerateContent(_ context.Context, _ []llms.Message, _ ...llms.CallOption) (*llms.Response, error) {
	if m.genErr != nil {
		return nil, m.genErr
	}
	if m.callErr != nil {
		return nil, m.callErr
	}
	if m.genResp == nil {
		return &llms.Response{Content: m.callResp}, nil
	}
	return m.genResp, nil
}

func (m *mockOTelLLM) Stream(_ context.Context, _ []llms.Message, _ ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	ch := make(chan llms.StreamChunk, len(m.chunks))
	for _, chunk := range m.chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func (m *mockOTelLLM) Provider() llms.Provider { return m.provider }
func (m *mockOTelLLM) Model() string           { return m.model }

func TestNewOTelMiddleware(t *testing.T) {
	llm := &mockOTelLLM{provider: llms.ProviderOpenAI, model: "gpt-4"}

	middleware, err := NewOTelMiddleware(llm)
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
}

func TestOTelMiddleware_Call_Success(t *testing.T) {
	// Setup span recorder
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer(InstrumentationName)

	llm := &mockOTelLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		callResp: "Hello, world!",
	}

	middleware, err := NewOTelMiddleware(llm, WithOTelTracer(tracer))
	if err != nil {
		t.Fatalf("failed to create middleware: %v", err)
	}

	result, err := middleware.Call(context.Background(), "Hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello, world!" {
		t.Errorf("result = %s, want 'Hello, world!'", result)
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

	// Check attributes
	attrs := span.Attributes()
	assertAttribute(t, attrs, "llm.provider", "openai")
	assertAttribute(t, attrs, "llm.model", "gpt-4")
	assertAttribute(t, attrs, "llm.request.type", "call")
}

func TestOTelMiddleware_Call_Error(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer(InstrumentationName)

	expectedErr := &llms.APIError{
		StatusCode: 429,
		Message:    "rate limited",
		Type:       "rate_limit_error",
	}

	llm := &mockOTelLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		callErr:  expectedErr,
	}

	middleware, _ := NewOTelMiddleware(llm, WithOTelTracer(tracer))
	_, err := middleware.Call(context.Background(), "Hi")

	if !errors.Is(err, expectedErr) {
		t.Errorf("error = %v, want %v", err, expectedErr)
	}

	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if span.Status().Code != codes.Error {
		t.Errorf("span status = %v, want Error", span.Status().Code)
	}

	// Check error attributes
	attrs := span.Attributes()
	assertAttribute(t, attrs, "llm.error.status_code", int64(429))
	assertAttribute(t, attrs, "llm.error.type", "rate_limited")
}

func TestOTelMiddleware_GenerateContent_Success(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer(InstrumentationName)

	llm := &mockOTelLLM{
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
			ToolCalls: []llms.ToolCall{{ID: "1"}},
		},
	}

	middleware, _ := NewOTelMiddleware(llm, WithOTelTracer(tracer))
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

	span := spans[0]
	attrs := span.Attributes()
	assertAttribute(t, attrs, "llm.provider", "anthropic")
	assertAttribute(t, attrs, "llm.model", "claude-3")
	assertAttribute(t, attrs, "llm.finish_reason", "stop")
	assertAttribute(t, attrs, "llm.tokens.prompt", int64(100))
	assertAttribute(t, attrs, "llm.tokens.completion", int64(50))
	assertAttribute(t, attrs, "llm.tokens.total", int64(150))
	assertAttribute(t, attrs, "llm.tool_calls", int64(1))
}

func TestOTelMiddleware_Stream_Success(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer(InstrumentationName)

	llm := &mockOTelLLM{
		provider: llms.ProviderGemini,
		model:    "gemini-pro",
		chunks: []llms.StreamChunk{
			{Content: "Hello"},
			{Content: " World"},
			{FinishReason: "stop", Usage: &llms.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
		},
	}

	middleware, _ := NewOTelMiddleware(llm, WithOTelTracer(tracer))
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

	if content != testOtelHelloWorld {
		t.Errorf("content = %s, want 'Hello World'", content)
	}

	// Wait for span to end
	time.Sleep(10 * time.Millisecond)

	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	attrs := span.Attributes()
	assertAttribute(t, attrs, "llm.provider", "gemini")
	assertAttribute(t, attrs, "llm.streaming", true)
	assertAttribute(t, attrs, "llm.stream.chunks", int64(3))
}

func TestOTelMiddleware_Stream_InitError(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer(InstrumentationName)

	expectedErr := errors.New("stream init error")
	llm := &mockOTelLLM{
		provider:  llms.ProviderOpenAI,
		model:     "gpt-4",
		streamErr: expectedErr,
	}

	middleware, _ := NewOTelMiddleware(llm, WithOTelTracer(tracer))
	_, err := middleware.Stream(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "Hi"}})

	if !errors.Is(err, expectedErr) {
		t.Errorf("error = %v, want %v", err, expectedErr)
	}

	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	if spans[0].Status().Code != codes.Error {
		t.Error("expected error status on span")
	}
}

func TestOTelMiddleware_Stream_CanceledSpanIsError(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer(InstrumentationName)

	llm := &mockOTelLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		chunks: []llms.StreamChunk{
			{Content: "one"},
			{Content: "two"},
			{Content: "three"},
		},
	}

	middleware, _ := NewOTelMiddleware(llm, WithOTelTracer(tracer))
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := middleware.Stream(ctx,
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
	cancel()

	deadline := time.After(500 * time.Millisecond)
	for len(spanRecorder.Ended()) == 0 {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for stream span to end")
		case <-time.After(5 * time.Millisecond):
		}
	}

	spans := spanRecorder.Ended()
	if spans[0].Status().Code == codes.Ok {
		t.Fatalf("span status = Ok, want non-Ok for canceled stream")
	}
}

func TestOTelMiddleware_WithContentRecording(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer(InstrumentationName)

	llm := &mockOTelLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		callResp: "response content",
	}

	middleware, _ := NewOTelMiddleware(llm,
		WithOTelTracer(tracer),
		WithContentRecording(true),
	)

	_, _ = middleware.Call(context.Background(), "test prompt")

	spans := spanRecorder.Ended()
	attrs := spans[0].Attributes()

	assertAttribute(t, attrs, "llm.prompt", "test prompt")
	assertAttribute(t, attrs, "llm.response", "response content")
}

func TestOTelMiddleware_GetProvider(t *testing.T) {
	llm := &mockOTelLLM{provider: llms.ProviderOpenAI}
	middleware, _ := NewOTelMiddleware(llm)

	if middleware.Provider() != llms.ProviderOpenAI {
		t.Errorf("provider = %s, want openai", middleware.Provider())
	}
}

func TestOTelMiddleware_GetModel(t *testing.T) {
	llm := &mockOTelLLM{model: "gpt-4-turbo"}
	middleware, _ := NewOTelMiddleware(llm)

	if middleware.Model() != "gpt-4-turbo" {
		t.Errorf("model = %s, want gpt-4-turbo", middleware.Model())
	}
}

func TestOTelMiddleware_Unwrap(t *testing.T) {
	llm := &mockOTelLLM{}
	middleware, _ := NewOTelMiddleware(llm)

	if middleware.Unwrap() != llm {
		t.Error("Unwrap should return underlying LLM")
	}
}

func TestOTelMiddleware_Metrics(t *testing.T) {
	// Create a test meter provider with a reader
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := meterProvider.Meter(InstrumentationName)

	llm := &mockOTelLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		genResp: &llms.Response{
			Content:      "test",
			FinishReason: "stop",
			Usage:        llms.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
		},
	}

	middleware, _ := NewOTelMiddleware(llm, WithOTelMeter(meter))

	// Make a request
	_, _ = middleware.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "Hi"}})

	// Collect metrics
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("failed to collect metrics: %v", err)
	}

	// Check that we have metrics
	if len(data.ScopeMetrics) == 0 {
		t.Fatal("expected scope metrics")
	}

	metrics := data.ScopeMetrics[0].Metrics
	if len(metrics) == 0 {
		t.Fatal("expected metrics to be recorded")
	}

	// Check for expected metric names
	metricNames := make(map[string]bool)
	for _, m := range metrics {
		metricNames[m.Name] = true
	}

	expectedMetrics := []string{
		metricRequests,
		metricTokensPrompt,
		metricTokensCompletion,
		metricRequestDuration,
	}

	for _, name := range expectedMetrics {
		if !metricNames[name] {
			t.Errorf("missing metric: %s", name)
		}
	}
}

func TestTruncateForSpan(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a long string", 10, "this is a ..."},
		{"", 10, ""},
	}

	for _, tc := range tests {
		result := truncateForSpan(tc.input, tc.maxLen)
		if result != tc.expected {
			t.Errorf("truncateForSpan(%q, %d) = %q, want %q", tc.input, tc.maxLen, result, tc.expected)
		}
	}
}

func TestTruncateForSpan_UTF8Safe(t *testing.T) {
	result := truncateForSpan("ab😀cd", 4)
	if !utf8.ValidString(result) {
		t.Fatalf("truncateForSpan returned invalid UTF-8: %q", result)
	}
	if result != "ab..." {
		t.Errorf("truncateForSpan returned %q, want %q", result, "ab...")
	}
}

func TestNormalizeErrorType_BucketsUnknownAPIError(t *testing.T) {
	err := &llms.APIError{
		StatusCode: 418,
		Message:    "provider-specific failure",
		Code:       "tenant_12345_model_67890",
	}
	if got := normalizeErrorType(err); got != "other" {
		t.Errorf("normalizeErrorType() = %q, want other", got)
	}
}

// Helper function to assert attribute values
func assertAttribute(t *testing.T, attrs []attribute.KeyValue, key string, expected any) {
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
			}
			return
		}
	}
	t.Errorf("attribute %s not found", key)
}
