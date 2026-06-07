package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
)

// MockRequest records the last request received by MockOpenAICompatibleServer.
type MockRequest struct {
	Method string
	Path   string
	Body   map[string]any
}

// MockOpenAICompatibleServer is an httptest harness for OpenAI-compatible APIs.
type MockOpenAICompatibleServer struct {
	server *httptest.Server

	mu          sync.Mutex
	lastRequest MockRequest

	chatResponse       any
	streamChunks       []any
	embeddingResponse  any
	errorStatus        int
	errorBody          any
	holdStreamOpen     bool
	streamRequestSeen  chan struct{}
	streamRequestClose chan struct{}
}

// MockOpenAICompatibleOption configures a MockOpenAICompatibleServer.
type MockOpenAICompatibleOption func(*MockOpenAICompatibleServer)

// NewMockOpenAICompatibleServer starts an OpenAI-compatible httptest server.
func NewMockOpenAICompatibleServer(opts ...MockOpenAICompatibleOption) *MockOpenAICompatibleServer {
	m := &MockOpenAICompatibleServer{
		streamRequestSeen:  make(chan struct{}),
		streamRequestClose: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(m)
	}
	m.server = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

// WithChatCompletionResponse sets the JSON response for /chat/completions.
func WithChatCompletionResponse(resp any) MockOpenAICompatibleOption {
	return func(m *MockOpenAICompatibleServer) {
		m.chatResponse = resp
	}
}

// WithStreamResponse sets SSE chunks for /chat/completions stream requests.
func WithStreamResponse(chunks ...any) MockOpenAICompatibleOption {
	return func(m *MockOpenAICompatibleServer) {
		m.streamChunks = chunks
	}
}

// WithEmbeddingsResponse sets the JSON response for /embeddings.
func WithEmbeddingsResponse(resp any) MockOpenAICompatibleOption {
	return func(m *MockOpenAICompatibleServer) {
		m.embeddingResponse = resp
	}
}

// WithErrorResponse makes every endpoint return the supplied status and body.
func WithErrorResponse(status int, body any) MockOpenAICompatibleOption {
	return func(m *MockOpenAICompatibleServer) {
		m.errorStatus = status
		m.errorBody = body
	}
}

// WithStreamHeldOpenAfterChunks leaves the SSE response open after configured chunks.
func WithStreamHeldOpenAfterChunks() MockOpenAICompatibleOption {
	return func(m *MockOpenAICompatibleServer) {
		m.holdStreamOpen = true
	}
}

// URL returns the server base URL.
func (m *MockOpenAICompatibleServer) URL() string {
	return m.server.URL
}

// Close shuts down the server.
func (m *MockOpenAICompatibleServer) Close() {
	m.server.Close()
}

// LastRequest returns the last request captured by the server.
func (m *MockOpenAICompatibleServer) LastRequest() MockRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return MockRequest{
		Method: m.lastRequest.Method,
		Path:   m.lastRequest.Path,
		Body:   cloneMap(m.lastRequest.Body),
	}
}

// StreamRequestSeen is closed after the server receives a streaming request.
func (m *MockOpenAICompatibleServer) StreamRequestSeen() <-chan struct{} {
	return m.streamRequestSeen
}

// StreamRequestClosed is closed after a held-open stream request context ends.
func (m *MockOpenAICompatibleServer) StreamRequestClosed() <-chan struct{} {
	return m.streamRequestClose
}

func (m *MockOpenAICompatibleServer) handle(w http.ResponseWriter, r *http.Request) {
	m.captureRequest(r)

	if m.errorStatus != 0 {
		writeJSON(w, m.errorStatus, m.errorBody)
		return
	}

	switch r.URL.Path {
	case "/chat/completions":
		if isStreamRequest(m.LastRequest().Body) {
			m.writeStream(w, r)
			return
		}
		writeJSON(w, http.StatusOK, m.chatResponse)
	case "/embeddings":
		writeJSON(w, http.StatusOK, m.embeddingResponse)
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": map[string]any{"message": "not found", "type": "not_found"},
		})
	}
}

func (m *MockOpenAICompatibleServer) captureRequest(r *http.Request) {
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if body == nil {
		body = map[string]any{}
	}

	m.mu.Lock()
	m.lastRequest = MockRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Body:   body,
	}
	m.mu.Unlock()
}

func (m *MockOpenAICompatibleServer) writeStream(w http.ResponseWriter, r *http.Request) {
	closeOnce(m.streamRequestSeen)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	for _, chunk := range m.streamChunks {
		data, err := json.Marshal(chunk)
		if err != nil {
			continue
		}
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(data)
		_, _ = w.Write([]byte("\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}
	if m.holdStreamOpen {
		<-r.Context().Done()
		closeOnce(m.streamRequestClose)
		return
	}
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	if flusher != nil {
		flusher.Flush()
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func isStreamRequest(body map[string]any) bool {
	stream, _ := body["stream"].(bool)
	return stream
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func closeOnce(ch chan struct{}) {
	defer func() { _ = recover() }()
	close(ch)
}
