package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	llms "github.com/nocturnium/llm-go-sdk/v4"
)

func TestSlogLogger_LogRequest(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, nil)
	logger := NewSlogLogger(slog.New(handler))

	entry := &LogEntry{
		RequestID: "test-123",
		Provider:  llms.ProviderOpenAI,
		Model:     "gpt-4",
		Operation: "call",
		Messages:  []llms.Message{{Role: llms.RoleUser, Content: "Hello"}},
		Timestamp: time.Now(),
		Streaming: false,
	}

	logger.LogRequest(context.Background(), entry)

	output := buf.String()
	if !strings.Contains(output, "test-123") {
		t.Error("expected request_id in output")
	}
	if !strings.Contains(output, "openai") {
		t.Error("expected provider in output")
	}
	if !strings.Contains(output, "gpt-4") {
		t.Error("expected model in output")
	}
}

func TestSlogLogger_LogResponse(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, nil)
	logger := NewSlogLogger(slog.New(handler))

	entry := &LogEntry{
		RequestID:    "test-456",
		Provider:     llms.ProviderAnthropic,
		Model:        "claude-3",
		Operation:    "generate_content",
		Content:      "Hello, world!",
		Duration:     100 * time.Millisecond,
		FinishReason: "stop",
		Usage: &llms.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	logger.LogResponse(context.Background(), entry)

	output := buf.String()
	if !strings.Contains(output, "test-456") {
		t.Error("expected request_id in output")
	}
	if !strings.Contains(output, "stop") {
		t.Error("expected finish_reason in output")
	}
}

func TestSlogLogger_LogError(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, nil)
	logger := NewSlogLogger(slog.New(handler))

	entry := &LogEntry{
		RequestID: "test-error",
		Provider:  llms.ProviderGemini,
		Model:     "gemini-pro",
		Operation: "call",
		Duration:  50 * time.Millisecond,
	}

	err := &llms.APIError{
		StatusCode: 429,
		Message:    "rate limited",
		Type:       "rate_limit_error",
	}

	logger.LogError(context.Background(), entry, err)

	output := buf.String()
	if !strings.Contains(output, "test-error") {
		t.Error("expected request_id in output")
	}
	if !strings.Contains(output, "429") {
		t.Error("expected status_code in output")
	}
}

// TestSlogLogger_LogError_SanitizesCRLF asserts the logged error string is
// CR/LF-escaped (CWE-117). A provider/upstream error can echo user-controlled
// input; decoded from the slog JSON output, the error must contain no raw CR/LF
// that could forge a log line.
func TestSlogLogger_LogError_SanitizesCRLF(t *testing.T) {
	var buf bytes.Buffer
	logger := NewSlogLogger(slog.New(slog.NewJSONHandler(&buf, nil)))

	logger.LogError(context.Background(), &LogEntry{RequestID: "r1"},
		errors.New("boom\r\nERROR forged=log line"))

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("parse slog output: %v\n%s", err, buf.String())
	}
	gotErr, _ := rec["error"].(string)
	if strings.ContainsAny(gotErr, "\r\n") {
		t.Errorf("error field has raw CR/LF (log-injection): %q", gotErr)
	}
	if !strings.Contains(gotErr, `\n`) {
		t.Errorf("expected CR/LF escaped to a literal sequence, got %q", gotErr)
	}
}

// TestJSONLogger_LogError_SanitizesCRLF asserts the same for the JSON logger:
// after round-tripping through JSON, the decoded error has no raw CR/LF.
func TestJSONLogger_LogError_SanitizesCRLF(t *testing.T) {
	var buf bytes.Buffer
	logger := NewJSONLogger(func(b []byte) error { buf.Write(b); return nil })

	logger.LogError(context.Background(), &LogEntry{RequestID: "r1"},
		errors.New("boom\r\nERROR forged=log line"))

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("parse JSON: %v", err)
	}
	entryData, _ := result["entry"].(map[string]any)
	gotErr, _ := entryData["error"].(string)
	if strings.ContainsAny(gotErr, "\r\n") {
		t.Errorf("error field has raw CR/LF (log-injection): %q", gotErr)
	}
	if !strings.Contains(gotErr, `\n`) {
		t.Errorf("expected CR/LF escaped to a literal sequence, got %q", gotErr)
	}
}

func TestSlogLogger_WithRedaction(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, nil)
	logger := NewSlogLogger(slog.New(handler), WithRedaction(true))

	entry := &LogEntry{
		RequestID: "test-redact",
		Provider:  llms.ProviderOpenAI,
		Model:     "gpt-4",
		Operation: "call",
		Messages:  []llms.Message{{Role: llms.RoleUser, Content: "secret password: 12345"}},
		Timestamp: time.Now(),
	}

	logger.LogRequest(context.Background(), entry)

	output := buf.String()
	if strings.Contains(output, "secret password") {
		t.Error("expected content to be redacted")
	}
}

func TestSlogLogger_WithMaxLength(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, nil)
	// Disable redaction (on by default) so content is actually logged and the
	// truncation behavior under test can be observed.
	logger := NewSlogLogger(slog.New(handler), WithRedaction(false), WithMaxLength(10))

	entry := &LogEntry{
		RequestID: "test-truncate",
		Provider:  llms.ProviderOpenAI,
		Model:     "gpt-4",
		Operation: "generate_content",
		Content:   "This is a very long response that should be truncated",
		Duration:  time.Second,
	}

	logger.LogResponse(context.Background(), entry)

	output := buf.String()
	if strings.Contains(output, "truncated") {
		t.Error("expected content to be truncated before 'truncated' word")
	}
}

func TestNopLogger(_ *testing.T) {
	logger := NopLogger{}

	// These should not panic
	logger.LogRequest(context.Background(), &LogEntry{})
	logger.LogResponse(context.Background(), &LogEntry{})
	logger.LogError(context.Background(), &LogEntry{}, errors.New("test"))
}

func TestDefaultIDGenerator_UniqueInTightLoop(t *testing.T) {
	seen := make(map[string]struct{}, 10_000)
	for i := 0; i < 10_000; i++ {
		id := defaultIDGenerator()
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate request ID generated: %s", id)
		}
		seen[id] = struct{}{}
	}
}

func TestTruncateString_UTF8Safe(t *testing.T) {
	result := truncateString("ab😀cd", 4)
	if !utf8.ValidString(result) {
		t.Fatalf("truncateString returned invalid UTF-8: %q", result)
	}
	if result != "ab..." {
		t.Errorf("truncateString returned %q, want %q", result, "ab...")
	}
}

func TestJSONLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewJSONLogger(func(b []byte) error {
		buf.Write(b)
		return nil
	})

	entry := &LogEntry{
		RequestID: "json-test",
		Provider:  llms.ProviderOpenAI,
		Model:     "gpt-4",
		Operation: "call",
		Timestamp: time.Now(),
	}

	logger.LogRequest(context.Background(), entry)

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result["type"] != "request" {
		t.Errorf("type = %v, want request", result["type"])
	}
}

func TestJSONLogger_LogError(t *testing.T) {
	var buf bytes.Buffer
	logger := NewJSONLogger(func(b []byte) error {
		buf.Write(b)
		return nil
	})

	entry := &LogEntry{
		RequestID: "json-error",
		Provider:  llms.ProviderOpenAI,
		Operation: "call",
	}

	logger.LogError(context.Background(), entry, errors.New("test error"))

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result["type"] != "error" {
		t.Errorf("type = %v, want error", result["type"])
	}

	entryData, ok := result["entry"].(map[string]any)
	if !ok {
		t.Fatal("entry is not a map[string]any")
	}
	if entryData["error"] != "test error" {
		t.Errorf("error = %v, want 'test error'", entryData["error"])
	}
}

// mockLLM is a mock LLM for testing
type mockLLM struct {
	callFn   func(ctx context.Context, prompt string, options ...llms.CallOption) (string, error)
	genFn    func(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.Response, error)
	streamFn func(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (<-chan llms.StreamChunk, error)
	provider llms.Provider
	model    string
}

func (m *mockLLM) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	if m.callFn != nil {
		return m.callFn(ctx, prompt, options...)
	}
	return "response", nil
}

func (m *mockLLM) GenerateContent(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.Response, error) {
	if m.genFn != nil {
		return m.genFn(ctx, messages, options...)
	}
	if m.callFn != nil {
		prompt := ""
		if len(messages) > 0 {
			prompt = messages[len(messages)-1].Content
		}
		content, err := m.callFn(ctx, prompt, options...)
		if err != nil {
			return nil, err
		}
		return &llms.Response{Content: content}, nil
	}
	return &llms.Response{Content: "response"}, nil
}

func (m *mockLLM) Stream(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	if m.streamFn != nil {
		return m.streamFn(ctx, messages, options...)
	}
	ch := make(chan llms.StreamChunk, 1)
	ch <- llms.StreamChunk{Content: "stream response"}
	close(ch)
	return ch, nil
}

func (m *mockLLM) Provider() llms.Provider {
	return m.provider
}

func (m *mockLLM) Model() string {
	return m.model
}

func TestLoggingMiddleware_Call(t *testing.T) {
	var requestLogged, responseLogged bool

	mockLogger := &testLogger{
		onRequest:  func(_ *LogEntry) { requestLogged = true },
		onResponse: func(_ *LogEntry) { responseLogged = true },
	}

	llm := &mockLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		callFn: func(_ context.Context, _ string, _ ...llms.CallOption) (string, error) {
			return "Hello!", nil
		},
	}

	middleware := NewLoggingMiddleware(llm, mockLogger)
	result, err := middleware.Call(context.Background(), "Hi")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello!" {
		t.Errorf("result = %s, want %s", result, "Hello!")
	}
	if !requestLogged {
		t.Error("request was not logged")
	}
	if !responseLogged {
		t.Error("response was not logged")
	}
}

func TestLoggingMiddleware_Call_Error(t *testing.T) {
	var errorLogged bool
	expectedErr := errors.New("api error")

	mockLogger := &testLogger{
		onError: func(_ *LogEntry, _ error) { errorLogged = true },
	}

	llm := &mockLLM{
		provider: llms.ProviderOpenAI,
		model:    "gpt-4",
		callFn: func(_ context.Context, _ string, _ ...llms.CallOption) (string, error) {
			return "", expectedErr
		},
	}

	middleware := NewLoggingMiddleware(llm, mockLogger)
	_, err := middleware.Call(context.Background(), "Hi")

	if !errors.Is(err, expectedErr) {
		t.Errorf("err = %v, want %v", err, expectedErr)
	}
	if !errorLogged {
		t.Error("error was not logged")
	}
}

func TestLoggingMiddleware_GenerateContent(t *testing.T) {
	var loggedEntry *LogEntry

	mockLogger := &testLogger{
		onResponse: func(entry *LogEntry) {
			loggedEntry = entry
		},
	}

	llm := &mockLLM{
		provider: llms.ProviderAnthropic,
		model:    "claude-3",
		genFn: func(_ context.Context, _ []llms.Message, _ ...llms.CallOption) (*llms.Response, error) {
			return &llms.Response{
				Content:      "Generated content",
				FinishReason: "stop",
				Usage: llms.Usage{
					PromptTokens:     10,
					CompletionTokens: 20,
					TotalTokens:      30,
				},
			}, nil
		},
	}

	middleware := NewLoggingMiddleware(llm, mockLogger)
	resp, err := middleware.GenerateContent(context.Background(), []llms.Message{
		{Role: llms.RoleUser, Content: "Test"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Generated content" {
		t.Errorf("content = %s, want 'Generated content'", resp.Content)
	}
	if loggedEntry == nil {
		t.Fatal("response entry was not logged")
	}
	if loggedEntry.Usage.TotalTokens != 30 {
		t.Errorf("logged tokens = %d, want 30", loggedEntry.Usage.TotalTokens)
	}
}

func TestLoggingMiddleware_Stream(t *testing.T) {
	var responseEntry *LogEntry

	mockLogger := &testLogger{
		onResponse: func(entry *LogEntry) {
			responseEntry = entry
		},
	}

	llm := &mockLLM{
		provider: llms.ProviderGemini,
		model:    "gemini-pro",
		streamFn: func(_ context.Context, _ []llms.Message, _ ...llms.CallOption) (<-chan llms.StreamChunk, error) {
			ch := make(chan llms.StreamChunk, 3)
			ch <- llms.StreamChunk{Content: "Hello"}
			ch <- llms.StreamChunk{Content: " World"}
			ch <- llms.StreamChunk{FinishReason: "stop"}
			close(ch)
			return ch, nil
		},
	}

	middleware := NewLoggingMiddleware(llm, mockLogger)
	stream, err := middleware.Stream(context.Background(), []llms.Message{
		{Role: llms.RoleUser, Content: "Test"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var fullContent string
	for chunk := range stream {
		fullContent += chunk.Content
	}

	if fullContent != "Hello World" {
		t.Errorf("content = %s, want %q", fullContent, "Hello World")
	}

	// Wait a bit for the logging goroutine
	time.Sleep(10 * time.Millisecond)

	if responseEntry == nil {
		t.Fatal("response was not logged")
	}
	if responseEntry.Content != "Hello World" {
		t.Errorf("logged content = %s, want %q", responseEntry.Content, "Hello World")
	}
}

func TestLoggingMiddleware_GetProvider(t *testing.T) {
	llm := &mockLLM{provider: llms.ProviderOpenAI}
	middleware := NewLoggingMiddleware(llm, NopLogger{})

	if middleware.Provider() != llms.ProviderOpenAI {
		t.Errorf("provider = %s, want openai", middleware.Provider())
	}
}

func TestLoggingMiddleware_GetModel(t *testing.T) {
	llm := &mockLLM{model: "gpt-4"}
	middleware := NewLoggingMiddleware(llm, NopLogger{})

	if middleware.Model() != "gpt-4" {
		t.Errorf("model = %s, want gpt-4", middleware.Model())
	}
}

func TestLoggingMiddleware_Unwrap(t *testing.T) {
	llm := &mockLLM{}
	middleware := NewLoggingMiddleware(llm, NopLogger{})

	if middleware.Unwrap() != llm {
		t.Error("Unwrap should return the underlying LLM")
	}
}

// testLogger is a test helper for capturing log entries
type testLogger struct {
	onRequest  func(*LogEntry)
	onResponse func(*LogEntry)
	onError    func(*LogEntry, error)
}

func (l *testLogger) LogRequest(_ context.Context, entry *LogEntry) {
	if l.onRequest != nil {
		l.onRequest(entry)
	}
}

func (l *testLogger) LogResponse(_ context.Context, entry *LogEntry) {
	if l.onResponse != nil {
		l.onResponse(entry)
	}
}

func (l *testLogger) LogError(_ context.Context, entry *LogEntry, err error) {
	if l.onError != nil {
		l.onError(entry, err)
	}
}

// Langfuse-specific tests

func TestLogEntry_ToLangfuseGeneration(t *testing.T) {
	now := time.Now()
	entry := &LogEntry{
		RequestID:    "req-123",
		Provider:     llms.ProviderOpenAI,
		Model:        "gpt-4",
		Operation:    "generate_content",
		Content:      "Hello, world!",
		Timestamp:    now,
		Duration:     100 * time.Millisecond,
		FinishReason: "stop",
		Usage: &llms.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
		TraceID:   "trace-456",
		SpanID:    "span-789",
		UserID:    "user-abc",
		SessionID: "session-def",
		Tags:      []string{"production", "chat"},
		Version:   "1.0.0",
		CostUSD:   0.001,
		Metadata:  map[string]any{"custom": "value"},
	}

	gen := entry.ToLangfuseGeneration()

	// Check required fields
	if gen["type"] != "GENERATION" {
		t.Errorf("type = %v, want GENERATION", gen["type"])
	}
	if gen["name"] != "generate_content" {
		t.Errorf("name = %v, want generate_content", gen["name"])
	}
	if gen["model"] != "gpt-4" {
		t.Errorf("model = %v, want gpt-4", gen["model"])
	}

	// Check trace fields
	if gen["trace_id"] != "trace-456" {
		t.Errorf("trace_id = %v, want trace-456", gen["trace_id"])
	}
	if gen["observation_id"] != "span-789" {
		t.Errorf("observation_id = %v, want span-789", gen["observation_id"])
	}

	// Check Langfuse fields
	if gen["user_id"] != "user-abc" {
		t.Errorf("user_id = %v, want user-abc", gen["user_id"])
	}
	if gen["session_id"] != "session-def" {
		t.Errorf("session_id = %v, want session-def", gen["session_id"])
	}

	// Check usage
	usage, ok := gen["usage"].(map[string]any)
	if !ok {
		t.Fatal("usage is not a map")
	}
	if usage["total_tokens"] != 15 {
		t.Errorf("total_tokens = %v, want 15", usage["total_tokens"])
	}
}

func TestLogEntry_ToLangfuseGeneration_Minimal(t *testing.T) {
	entry := &LogEntry{
		Operation: "call",
		Model:     "gpt-4",
		Timestamp: time.Now(),
	}

	gen := entry.ToLangfuseGeneration()

	if gen["type"] != "GENERATION" {
		t.Errorf("type = %v, want GENERATION", gen["type"])
	}

	// Optional fields should not be present
	if _, exists := gen["trace_id"]; exists {
		t.Error("trace_id should not be present when empty")
	}
	if _, exists := gen["user_id"]; exists {
		t.Error("user_id should not be present when empty")
	}
}

func TestLogEntry_ToLangfuseGeneration_MetadataIncludesCostAndTTFTWithoutOtherMetadata(t *testing.T) {
	entry := &LogEntry{
		Operation:        "stream",
		Model:            "gpt-4",
		Timestamp:        time.Now(),
		CostUSD:          0.001,
		TimeToFirstToken: 25 * time.Millisecond,
	}

	gen := entry.ToLangfuseGeneration()
	metadata, ok := gen["metadata"].(map[string]any)
	if !ok {
		t.Fatal("metadata is not a map")
	}
	if metadata["cost_usd"] != 0.001 {
		t.Errorf("cost_usd = %v, want 0.001", metadata["cost_usd"])
	}
	if metadata["time_to_first_token_ms"] != int64(25) {
		t.Errorf("time_to_first_token_ms = %v, want 25", metadata["time_to_first_token_ms"])
	}
}

func TestLogEntry_PopulateFromTraceContext(t *testing.T) {
	tc := &TraceContext{
		TraceID:     "trace-123",
		SpanID:      "span-456",
		ParentID:    "parent-789",
		UserID:      "user-abc",
		SessionID:   "session-def",
		Tags:        []string{"tag1", "tag2"},
		Version:     "2.0.0",
		Environment: "staging",
		Metadata:    map[string]string{"key1": "value1"},
	}

	entry := &LogEntry{}
	entry.PopulateFromTraceContext(tc)

	if entry.TraceID != "trace-123" {
		t.Errorf("TraceID = %s, want trace-123", entry.TraceID)
	}
	if entry.SpanID != "span-456" {
		t.Errorf("SpanID = %s, want span-456", entry.SpanID)
	}
	if entry.ParentSpanID != "parent-789" {
		t.Errorf("ParentSpanID = %s, want parent-789", entry.ParentSpanID)
	}
	if entry.UserID != "user-abc" {
		t.Errorf("UserID = %s, want user-abc", entry.UserID)
	}
	if entry.SessionID != "session-def" {
		t.Errorf("SessionID = %s, want session-def", entry.SessionID)
	}
	if entry.Version != "2.0.0" {
		t.Errorf("Version = %s, want 2.0.0", entry.Version)
	}
	if entry.Environment != "staging" {
		t.Errorf("Environment = %s, want staging", entry.Environment)
	}
	if len(entry.Tags) != 2 {
		t.Errorf("Tags length = %d, want 2", len(entry.Tags))
	}
	if entry.Metadata["key1"] != "value1" {
		t.Errorf("Metadata[key1] = %v, want value1", entry.Metadata["key1"])
	}
}

func TestLogEntry_PopulateFromTraceContext_Nil(t *testing.T) {
	entry := &LogEntry{
		UserID: "existing-user",
	}

	entry.PopulateFromTraceContext(nil)

	// Should not panic and should not modify existing values
	if entry.UserID != "existing-user" {
		t.Errorf("UserID = %s, want existing-user", entry.UserID)
	}
}

func TestLogEntry_PopulateFromCallOptions(t *testing.T) {
	opts := &llms.CallOptions{
		Temperature:      float64Ptr(0.7),
		MaxTokens:        intPtr(1000),
		TopP:             float64Ptr(0.9),
		FrequencyPenalty: float64Ptr(0.5),
		PresencePenalty:  float64Ptr(0.3),
		ResponseFormat:   &llms.ResponseFormat{Type: llms.ResponseFormatJSONObject},
		Trace: &llms.TraceOptions{
			TraceID:   "opt-trace",
			SpanID:    "opt-span",
			ParentID:  "opt-parent",
			UserID:    "opt-user",
			SessionID: "opt-session",
			Version:   "opt-version",
			Tags:      []string{"opt-tag1", "opt-tag2"},
			Metadata:  map[string]any{"opt_key": "opt_value", "opt_count": 2},
		},
	}

	entry := &LogEntry{}
	entry.PopulateFromCallOptions(opts)

	if entry.TraceID != "opt-trace" {
		t.Errorf("TraceID = %s, want opt-trace", entry.TraceID)
	}
	if entry.UserID != "opt-user" {
		t.Errorf("UserID = %s, want opt-user", entry.UserID)
	}
	if entry.Version != "opt-version" {
		t.Errorf("Version = %s, want opt-version", entry.Version)
	}
	if len(entry.Tags) != 2 {
		t.Errorf("Tags length = %d, want 2", len(entry.Tags))
	}
	if entry.Metadata["opt_key"] != "opt_value" {
		t.Errorf("Metadata[opt_key] = %v, want opt_value", entry.Metadata["opt_key"])
	}
	if entry.Metadata["opt_count"] != 2 {
		t.Errorf("Metadata[opt_count] = %v, want 2", entry.Metadata["opt_count"])
	}

	// Check request parameters
	if entry.RequestParameters["temperature"] != 0.7 {
		t.Errorf("temperature = %v, want 0.7", entry.RequestParameters["temperature"])
	}
	if entry.RequestParameters["json_mode"] != true {
		t.Error("json_mode should be true")
	}
}

func TestLogEntry_PopulateFromCallOptions_Nil(t *testing.T) {
	entry := &LogEntry{
		UserID: "existing-user",
	}

	entry.PopulateFromCallOptions(nil)

	// Should not panic and should not modify existing values
	if entry.UserID != "existing-user" {
		t.Errorf("UserID = %s, want existing-user", entry.UserID)
	}
}

func TestSanitizeLogValue(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello world", "hello world"},
		{"newline", "line1\nline2", "line1\\nline2"},
		{"carriage_return", "a\rb", "a\\rb"},
		{"crlf", "a\r\nb", "a\\r\\nb"},
		{"forged_entry", "ok\nERROR forged log line", "ok\\nERROR forged log line"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeLogValue(tt.in); got != tt.want {
				t.Errorf("sanitizeLogValue(%q) = %q, want %q", tt.in, got, tt.want)
			}
			// A sanitized value must never contain a raw CR or LF.
			if got := sanitizeLogValue(tt.in); strings.ContainsAny(got, "\r\n") {
				t.Errorf("sanitizeLogValue(%q) = %q still contains a raw CR/LF", tt.in, got)
			}
		})
	}
}
