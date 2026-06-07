package llms

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// float64Ptr returns a pointer to the given float64, for building CallOptions
// whose sampling parameters (Temperature/TopP) are *float64 so an explicit 0.0
// is distinguishable from "unset".
func float64Ptr(v float64) *float64 { return &v }

func intPtr(v int) *int { return &v }

// MockLLM is a test double that implements the LLM interface
type MockLLM struct {
	provider     Provider
	model        string
	callResponse string
	callError    error
	genResponse  *Response
	genError     error
	streamChunks []StreamChunk
	streamError  error
	callCount    int
	lastMessages []Message
	lastOptions  *CallOptions
	capabilities Capabilities
}

// MockLLMOption configures a MockLLM
type MockLLMOption func(*MockLLM)

// NewMockLLM creates a new mock LLM for testing
func NewMockLLM(opts ...MockLLMOption) *MockLLM {
	m := &MockLLM{
		provider: ProviderOpenAI,
		model:    "test-model",
		capabilities: Capabilities{
			Streaming: true,
			Tools:     true,
		},
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// WithMockProvider sets the provider
func WithMockProvider(p Provider) MockLLMOption {
	return func(m *MockLLM) {
		m.provider = p
	}
}

// WithMockModel sets the model
func WithMockModel(model string) MockLLMOption {
	return func(m *MockLLM) {
		m.model = model
	}
}

// WithMockCallResponse sets the Call response
func WithMockCallResponse(resp string) MockLLMOption {
	return func(m *MockLLM) {
		m.callResponse = resp
	}
}

// WithMockCallError sets the Call error
func WithMockCallError(err error) MockLLMOption {
	return func(m *MockLLM) {
		m.callError = err
	}
}

// WithMockGenerateResponse sets the GenerateContent response
func WithMockGenerateResponse(resp *Response) MockLLMOption {
	return func(m *MockLLM) {
		m.genResponse = resp
	}
}

// WithMockGenerateError sets the GenerateContent error
func WithMockGenerateError(err error) MockLLMOption {
	return func(m *MockLLM) {
		m.genError = err
	}
}

// WithMockStreamChunks sets the stream chunks to return
func WithMockStreamChunks(chunks []StreamChunk) MockLLMOption {
	return func(m *MockLLM) {
		m.streamChunks = chunks
	}
}

// WithMockStreamError sets the stream error
func WithMockStreamError(err error) MockLLMOption {
	return func(m *MockLLM) {
		m.streamError = err
	}
}

// WithMockCapabilities sets the capabilities
func WithMockCapabilities(caps Capabilities) MockLLMOption {
	return func(m *MockLLM) {
		m.capabilities = caps
	}
}

func (m *MockLLM) Call(_ context.Context, prompt string, options ...CallOption) (string, error) {
	m.callCount++
	m.lastMessages = []Message{{Role: RoleUser, Content: prompt}}
	m.lastOptions = ApplyOptions(options...)
	if m.callError != nil {
		return "", m.callError
	}
	return m.callResponse, nil
}

func (m *MockLLM) GenerateContent(_ context.Context, messages []Message, options ...CallOption) (*Response, error) {
	m.callCount++
	m.lastMessages = messages
	m.lastOptions = ApplyOptions(options...)
	if m.genError != nil {
		return nil, m.genError
	}
	if m.callError != nil {
		return nil, m.callError
	}
	if m.genResponse != nil {
		return m.genResponse, nil
	}
	return &Response{Content: m.callResponse}, nil
}

func (m *MockLLM) Stream(ctx context.Context, messages []Message, options ...CallOption) (<-chan StreamChunk, error) {
	m.callCount++
	m.lastMessages = messages
	m.lastOptions = ApplyOptions(options...)
	if m.streamError != nil {
		return nil, m.streamError
	}

	ch := make(chan StreamChunk, len(m.streamChunks)+1)
	go func() {
		defer close(ch)
		for _, chunk := range m.streamChunks {
			select {
			case <-ctx.Done():
				return
			case ch <- chunk:
			}
		}
	}()
	return ch, nil
}

func (m *MockLLM) Provider() Provider {
	return m.provider
}

func (m *MockLLM) Model() string {
	return m.model
}

func (m *MockLLM) Capabilities() Capabilities {
	return m.capabilities
}

// CallCount returns the number of times any method was called
func (m *MockLLM) CallCount() int {
	return m.callCount
}

// LastMessages returns the last messages passed to any method
func (m *MockLLM) LastMessages() []Message {
	return m.lastMessages
}

// LastOptions returns the last options passed to any method
func (m *MockLLM) LastOptions() *CallOptions {
	return m.lastOptions
}

// Reset resets the call count and last messages
func (m *MockLLM) Reset() {
	m.callCount = 0
	m.lastMessages = nil
	m.lastOptions = nil
}

// Ensure MockLLM implements LLM and CapableProvider
var (
	_ LLM             = (*MockLLM)(nil)
	_ CapableProvider = (*MockLLM)(nil)
)

// MockServerConfig configures a mock HTTP server for testing
type MockServerConfig struct {
	// Response to return
	Response any
	// Status code to return (default 200)
	StatusCode int
	// Handler function (if set, overrides Response)
	Handler http.HandlerFunc
	// Delay before responding
	Delay int
}

// NewMockServer creates a new mock HTTP server for testing
func NewMockServer(t *testing.T, config MockServerConfig) *httptest.Server {
	t.Helper()

	handler := config.Handler
	if handler == nil {
		handler = func(w http.ResponseWriter, _ *http.Request) {
			status := config.StatusCode
			if status == 0 {
				status = http.StatusOK
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			if config.Response != nil {
				_ = json.NewEncoder(w).Encode(config.Response)
			}
		}
	}

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

// StandardChatResponse returns a standard chat completion response for testing
func StandardChatResponse(content string) map[string]any {
	return map[string]any{
		"id":      "test-123",
		"object":  "chat.completion",
		"created": 1677652288,
		"model":   "test-model",
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 20,
			"total_tokens":      30,
		},
	}
}

// ToolCallResponse returns a chat response with tool calls for testing
func ToolCallResponse(toolID, toolName, arguments string) map[string]any {
	return map[string]any{
		"id":      "test-123",
		"object":  "chat.completion",
		"created": 1677652288,
		"model":   "test-model",
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "",
					"tool_calls": []map[string]any{
						{
							"id":   toolID,
							"type": "function",
							"function": map[string]any{
								"name":      toolName,
								"arguments": arguments,
							},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     15,
			"completion_tokens": 25,
			"total_tokens":      40,
		},
	}
}

// ErrorResponse returns an API error response for testing
func ErrorResponse(_ int, errorType, message string) map[string]any {
	return map[string]any{
		"error": map[string]any{
			"type":    errorType,
			"message": message,
			"code":    errorType,
		},
	}
}

// StreamingChunks returns a slice of StreamChunks for testing streaming
func StreamingChunks(contents ...string) []StreamChunk {
	chunks := make([]StreamChunk, 0, len(contents)+1)
	for _, content := range contents {
		chunks = append(chunks, StreamChunk{Content: content})
	}
	// Add final chunk with done flag
	chunks = append(chunks, StreamChunk{
		Done:         true,
		FinishReason: "stop",
		Usage: &Usage{
			PromptTokens:     10,
			CompletionTokens: len(contents) * 5,
			TotalTokens:      10 + len(contents)*5,
		},
	})
	return chunks
}

// AssertNoError fails the test if err is not nil
func AssertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// AssertError fails the test if err is nil
func AssertError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// AssertEqual fails the test if got != want
func AssertEqual[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", msg, got, want)
	}
}

// AssertContains fails the test if s does not contain substr
func AssertContains(t *testing.T, s, substr string, msg string) {
	t.Helper()
	if len(substr) == 0 {
		return
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return
		}
	}
	t.Errorf("%s: %q does not contain %q", msg, s, substr)
}
