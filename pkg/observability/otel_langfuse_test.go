package observability

import (
	"context"
	"errors"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v3"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// mockLangfuseLLM is a mock LLM for testing Langfuse middleware
type mockLangfuseLLM struct {
	provider  llms.Provider
	model     string
	callResp  string
	callErr   error
	genResp   *llms.Response
	genErr    error
	chunks    []llms.StreamChunk
	streamErr error
}

func (m *mockLangfuseLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	return m.callResp, m.callErr
}

func (m *mockLangfuseLLM) GenerateContent(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.Response, error) {
	if m.genErr != nil {
		return nil, m.genErr
	}
	if m.callErr != nil {
		return nil, m.callErr
	}
	if m.genResp != nil {
		return m.genResp, nil
	}
	return &llms.Response{Content: m.callResp}, nil
}

func (m *mockLangfuseLLM) Stream(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	ch := make(chan llms.StreamChunk, len(m.chunks))
	go func() {
		defer close(ch)
		for _, chunk := range m.chunks {
			ch <- chunk
		}
	}()
	return ch, nil
}

func (m *mockLangfuseLLM) Provider() llms.Provider {
	return m.provider
}

func (m *mockLangfuseLLM) Model() string {
	return m.model
}

func TestNewLangfuseOTelMiddleware(t *testing.T) {
	mock := &mockLangfuseLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
	}

	middleware, err := NewLangfuseOTelMiddleware(mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if middleware.Provider() != llms.ProviderOpenAI {
		t.Errorf("expected provider OpenAI, got %s", middleware.Provider())
	}

	if middleware.Model() != "gpt-4" {
		t.Errorf("expected model gpt-4, got %s", middleware.Model())
	}
}

func TestNewLangfuseOTelMiddleware_WithOptions(t *testing.T) {
	mock := &mockLangfuseLLM{
		provider: llms.ProviderAnthropic,
		model:    "claude-3",
	}

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	reader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(meterProvider)

	costTracker := llms.NewCostTracker()

	middleware, err := NewLangfuseOTelMiddleware(mock,
		WithLangfuseInputCapture(true, 50000),
		WithLangfuseOutputCapture(true, 50000),
		WithLangfuseInputFormat(InputFormatRaw),
		WithLangfuseOutputFormat(OutputFormatRaw),
		WithLangfuseTracer(tracerProvider.Tracer("test")),
		WithLangfuseMeter(meterProvider.Meter("test")),
		WithLangfuseCostTracker(costTracker),
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if middleware.maxInputLen != 50000 {
		t.Errorf("expected maxInputLen 50000, got %d", middleware.maxInputLen)
	}

	if middleware.maxOutputLen != 50000 {
		t.Errorf("expected maxOutputLen 50000, got %d", middleware.maxOutputLen)
	}

	if middleware.inputFormat != InputFormatRaw {
		t.Errorf("expected inputFormat Raw, got %d", middleware.inputFormat)
	}

	if middleware.outputFormat != OutputFormatRaw {
		t.Errorf("expected outputFormat Raw, got %d", middleware.outputFormat)
	}
}

func TestLangfuseOTelMiddleware_Call(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	mock := &mockLangfuseLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		callResp: "Hello response",
	}

	middleware, err := NewLangfuseOTelMiddleware(mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	resp, err := middleware.Call(ctx, "Hello prompt")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp != "Hello response" {
		t.Errorf("expected 'Hello response', got %s", resp)
	}

	// Check span was created
	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Error("expected at least one span")
	}
}

func TestLangfuseOTelMiddleware_Call_Error(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	expectedErr := errors.New("call failed")
	mock := &mockLangfuseLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		callErr:  expectedErr,
	}

	middleware, err := NewLangfuseOTelMiddleware(mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	_, err = middleware.Call(ctx, "Hello prompt")

	if err == nil {
		t.Error("expected error")
	}
}

func TestLangfuseOTelMiddleware_GenerateContent(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	mock := &mockLangfuseLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		genResp: &llms.Response{
			Content:      "Generated content",
			FinishReason: "stop",
			Usage: llms.Usage{
				PromptTokens:     10,
				CompletionTokens: 20,
				TotalTokens:      30,
			},
		},
	}

	middleware, err := NewLangfuseOTelMiddleware(mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Hello"},
	}

	resp, err := middleware.GenerateContent(ctx, messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Content != "Generated content" {
		t.Errorf("expected 'Generated content', got %s", resp.Content)
	}

	if resp.Usage.TotalTokens != 30 {
		t.Errorf("expected 30 total tokens, got %d", resp.Usage.TotalTokens)
	}
}

func TestLangfuseOTelMiddleware_GenerateContent_WithTraceContext(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	mock := &mockLangfuseLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		genResp: &llms.Response{
			Content: "Response with context",
		},
	}

	middleware, err := NewLangfuseOTelMiddleware(mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tc := &TraceContext{
		TraceID:   "trace-123",
		UserID:    "user-456",
		SessionID: "session-789",
		Tags:      []string{"test", "langfuse"},
		Metadata:  map[string]string{"key": "value"},
	}

	ctx := WithTraceContext(context.Background(), tc)
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Hello"},
	}

	resp, err := middleware.GenerateContent(ctx, messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Content != "Response with context" {
		t.Errorf("expected 'Response with context', got %s", resp.Content)
	}
}

func TestLangfuseOTelMiddleware_GenerateContent_WithCallOptions(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	mock := &mockLangfuseLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		genResp: &llms.Response{
			Content: "Response with options",
		},
	}

	middleware, err := NewLangfuseOTelMiddleware(mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Hello"},
	}

	resp, err := middleware.GenerateContent(ctx, messages,
		llms.WithTrace(llms.TraceOptions{
			TraceID:   "trace-override",
			UserID:    "user-override",
			SessionID: "session-override",
			Tags:      []string{"tag1", "tag2"},
			Metadata:  map[string]any{"meta": "data"},
			Version:   "v1.0",
		}),
		llms.WithTemperature(0.8),
		llms.WithMaxTokens(100),
		llms.WithTopP(0.9),
		llms.WithFrequencyPenalty(0.5),
		llms.WithPresencePenalty(0.5),
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Content != "Response with options" {
		t.Errorf("expected 'Response with options', got %s", resp.Content)
	}
}

func TestLangfuseOTelMiddleware_GenerateContent_Error(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	expectedErr := errors.New("generation failed")
	mock := &mockLangfuseLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		genErr:   expectedErr,
	}

	middleware, err := NewLangfuseOTelMiddleware(mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Hello"},
	}

	_, err = middleware.GenerateContent(ctx, messages)
	if err == nil {
		t.Error("expected error")
	}
}

func TestLangfuseOTelMiddleware_Stream(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	mock := &mockLangfuseLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		chunks: []llms.StreamChunk{
			{Content: "Hello "},
			{Content: "World"},
			{
				FinishReason: "stop",
				Usage: &llms.Usage{
					PromptTokens:     10,
					CompletionTokens: 5,
					TotalTokens:      15,
				},
			},
		},
	}

	middleware, err := NewLangfuseOTelMiddleware(mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Hello"},
	}

	stream, err := middleware.Stream(ctx, messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var content string
	for chunk := range stream {
		content += chunk.Content
	}

	if content != "Hello World" {
		t.Errorf("expected 'Hello World', got %s", content)
	}

	// Wait for span to complete
	time.Sleep(100 * time.Millisecond)
}

func TestLangfuseOTelMiddleware_Stream_Error(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	expectedErr := errors.New("stream failed")
	mock := &mockLangfuseLLM{
		provider:  llms.ProviderOpenAI,
		model:     "gpt-4",
		streamErr: expectedErr,
	}

	middleware, err := NewLangfuseOTelMiddleware(mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Hello"},
	}

	_, err = middleware.Stream(ctx, messages)
	if err == nil {
		t.Error("expected error")
	}
}

func TestLangfuseOTelMiddleware_Stream_WithToolCalls(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	mock := &mockLangfuseLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		chunks: []llms.StreamChunk{
			{Content: "Let me help you"},
			{
				ToolCalls: []llms.ToolCall{
					{ID: "call_1", Type: "function", Function: &llms.FunctionCall{Name: "get_weather"}},
				},
			},
			{FinishReason: "tool_calls"},
		},
	}

	middleware, err := NewLangfuseOTelMiddleware(mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "What's the weather?"},
	}

	stream, err := middleware.Stream(ctx, messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var hasToolCalls bool
	for chunk := range stream {
		if len(chunk.ToolCalls) > 0 {
			hasToolCalls = true
		}
	}

	if !hasToolCalls {
		t.Error("expected tool calls in stream")
	}

	time.Sleep(100 * time.Millisecond)
}

func TestLangfuseOTelMiddleware_Stream_ChunkError(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	mock := &mockLangfuseLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		chunks: []llms.StreamChunk{
			{Content: "Hello"},
			{Error: errors.New("chunk error")},
		},
	}

	middleware, err := NewLangfuseOTelMiddleware(mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Hello"},
	}

	stream, err := middleware.Stream(ctx, messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var hadError bool
	for chunk := range stream {
		if chunk.Error != nil {
			hadError = true
		}
	}

	if !hadError {
		t.Error("expected error in stream")
	}

	time.Sleep(100 * time.Millisecond)
}

func TestLangfuseOTelMiddleware_Stream_CanceledSpanIsError(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer(LangfuseInstrumentationName)

	mock := &mockLangfuseLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		chunks: []llms.StreamChunk{
			{Content: "one"},
			{Content: "two"},
			{Content: "three"},
		},
	}

	middleware, err := NewLangfuseOTelMiddleware(mock, WithLangfuseTracer(tracer))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := middleware.Stream(ctx,
		[]llms.Message{{Role: llms.RoleUser, Content: "Hello"}},
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

func TestLangfuseOTelMiddleware_Unwrap(t *testing.T) {
	mock := &mockLangfuseLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
	}

	middleware, err := NewLangfuseOTelMiddleware(mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	unwrapped := middleware.Unwrap()
	if unwrapped != mock {
		t.Error("Unwrap should return the underlying LLM")
	}
}

func TestLangfuseOTelMiddleware_WithCostTracker(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	mock := &mockLangfuseLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		genResp: &llms.Response{
			Content: "Response",
			Usage: llms.Usage{
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
			},
		},
	}

	costTracker := llms.NewCostTracker()

	middleware, err := NewLangfuseOTelMiddleware(mock,
		WithLangfuseCostTracker(costTracker),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Hello"},
	}

	_, err = middleware.GenerateContent(ctx, messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Cost tracker should have recorded usage
	promptTokens, _ := costTracker.GetTotalTokens()
	if promptTokens != 100 {
		t.Errorf("expected 100 prompt tokens in tracker, got %d", promptTokens)
	}
}

func TestLangfuseOTelMiddleware_CostUsesTrackerPricingWithCacheTokens(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer(LangfuseInstrumentationName)

	usage := llms.Usage{
		PromptTokens:        1_000_000,
		CompletionTokens:    1_000_000,
		CacheReadTokens:     1_000_000,
		CacheCreationTokens: 1_000_000,
		TotalTokens:         4_000_000,
	}
	mock := &mockLangfuseLLM{
		provider: llms.ProviderAnthropic,
		model:    "claude-3-opus-20240229",
		genResp: &llms.Response{
			Content: "Response",
			Usage:   usage,
		},
	}
	costTracker := llms.NewCostTracker()
	middleware, err := NewLangfuseOTelMiddleware(mock,
		WithLangfuseTracer(tracer),
		WithLangfuseCostTracker(costTracker),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = middleware.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "Hello"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	trackerCost := costTracker.GetTotalCost()
	spans := spanRecorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	assertMetricsAttribute(t, spans[0].Attributes(), AttrGenAIUsageCost, trackerCost)
}

func TestLangfuseOTelMiddleware_DisabledCapture(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	otel.SetTracerProvider(tracerProvider)

	mock := &mockLangfuseLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		callResp: "Response",
	}

	middleware, err := NewLangfuseOTelMiddleware(mock,
		WithLangfuseInputCapture(false, 0),
		WithLangfuseOutputCapture(false, 0),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if middleware.captureInput {
		t.Error("expected captureInput to be false")
	}
	if middleware.captureOutput {
		t.Error("expected captureOutput to be false")
	}

	ctx := context.Background()
	_, err = middleware.Call(ctx, "Hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestLangfuseOTelMiddleware_CaptureOffByDefault verifies that prompt/response
// capture is privacy-safe by default: a middleware constructed without any
// capture options must NOT capture input or output.
func TestLangfuseOTelMiddleware_CaptureOffByDefault(t *testing.T) {
	mock := &mockLangfuseLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
	}

	middleware, err := NewLangfuseOTelMiddleware(mock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if middleware.captureInput {
		t.Error("expected captureInput to be false by default (privacy-safe)")
	}
	if middleware.captureOutput {
		t.Error("expected captureOutput to be false by default (privacy-safe)")
	}
}
