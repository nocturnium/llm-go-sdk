package testutil

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

	imageResponse          any
	imageEditResponse      any
	imageStream            []any
	speechData             []byte
	speechContentType      string
	speechStream           []any
	transcriptionResponses map[string]any
	videoCreate            any
	videoStates            []any
	videoPolls             int
	videoData              []byte
	chatResponse           any
	streamChunks           []any
	embeddingResponse      any
	errorStatus            int
	errorBody              any
	holdStreamOpen         bool
	streamRequestSeen      chan struct{}
	streamRequestClose     chan struct{}
}

// MockOpenAICompatibleOption configures a MockOpenAICompatibleServer.
type MockOpenAICompatibleOption func(*MockOpenAICompatibleServer)

// NewMockOpenAICompatibleServer starts an OpenAI-compatible httptest server.
func NewMockOpenAICompatibleServer(opts ...MockOpenAICompatibleOption) *MockOpenAICompatibleServer {
	m := &MockOpenAICompatibleServer{
		imageResponse:     map[string]any{"data": []any{map[string]any{"b64_json": "iVBORw0KGgo="}}, "usage": map[string]any{"output_tokens": 10}},
		imageEditResponse: map[string]any{"data": []any{map[string]any{"b64_json": "iVBORw0KGgo="}}},
		imageStream:       []any{map[string]any{"type": "image_generation.partial_image", "b64_json": "iVBORw0KGgo=", "partial_image_index": 0}, map[string]any{"type": "image_generation.completed", "b64_json": "iVBORw0KGgo=", "usage": map[string]any{"output_tokens": 10}}},
		speechData:        []byte("RIFFmockWAVE"), speechContentType: "audio/wav",
		speechStream: []any{map[string]any{"type": "speech.audio.delta", "audio": "UklGRg=="}, map[string]any{"type": "speech.audio.done", "usage": map[string]any{"output_tokens": 20}}},
		transcriptionResponses: map[string]any{
			"json":          map[string]any{"text": "Hi.", "usage": map[string]any{"type": "tokens", "total_tokens": 25, "input_tokens": 16, "input_token_details": map[string]any{"text_tokens": 0, "audio_tokens": 16}, "output_tokens": 9}},
			"verbose_json":  map[string]any{"text": "Hi.", "language": "en", "duration": 2, "usage": map[string]any{"type": "duration", "seconds": 2}, "segments": []any{map[string]any{"id": 0, "start": 0, "end": 2, "text": "Hi."}}, "words": []any{map[string]any{"word": "Hi.", "start": 0, "end": 2}}},
			"diarized_json": map[string]any{"text": "Hi.", "usage": map[string]any{"type": "tokens", "input_tokens": 16, "input_token_details": map[string]any{"text_tokens": 0, "audio_tokens": 16}, "output_tokens": 9}, "segments": []any{map[string]any{"id": "seg_0", "type": "transcript.text.segment", "speaker": "A", "start": 0, "end": 2, "text": "Hi."}}},
			"text":          "Hi.", "srt": "1\n00:00:00,000 --> 00:00:02,000\nHi.\n", "vtt": "WEBVTT\n\n00:00.000 --> 00:02.000\nHi.\n",
		},
		videoCreate:        map[string]any{"id": "video_test", "object": "video", "status": "queued", "seconds": "4", "model": "sora-2", "size": "1280x720"},
		videoStates:        []any{map[string]any{"id": "video_test", "status": "queued"}, map[string]any{"id": "video_test", "status": "in_progress", "progress": 50}, map[string]any{"id": "video_test", "status": "completed", "progress": 100, "seconds": "4", "model": "sora-2", "size": "1280x720"}},
		videoData:          []byte("\x00\x00\x00\x18ftypmp42"),
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
	body := m.captureRequest(w, r)

	if m.errorStatus != 0 {
		writeJSON(w, m.errorStatus, m.errorBody)
		return
	}

	switch r.URL.Path {
	case "/chat/completions":
		if isStreamRequest(body) {
			m.writeStream(w, r)
			return
		}
		writeJSON(w, http.StatusOK, m.chatResponse)
	case "/images/generations":
		if isStreamRequest(body) {
			m.writeMediaStream(w, r, m.imageStream)
			return
		}
		writeJSON(w, http.StatusOK, m.imageResponse)
	case "/images/edits":
		writeJSON(w, http.StatusOK, m.imageEditResponse)
	case "/audio/speech":
		if body["stream_format"] == "sse" {
			m.writeMediaStream(w, r, m.speechStream)
			return
		}
		w.Header().Set("Content-Type", m.speechContentType)
		_, _ = w.Write(m.speechData)
	case "/audio/transcriptions":
		format, _ := body["response_format"].(string)
		if format == "" {
			format = "json"
		}
		response := m.transcriptionResponses[format]
		if text, ok := response.(string); ok {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, text)
		} else {
			writeJSON(w, http.StatusOK, response)
		}
	case "/videos":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, m.videoCreate)
	case "/embeddings":
		writeJSON(w, http.StatusOK, m.embeddingResponse)
	default:
		if strings.HasPrefix(r.URL.Path, "/videos/") && r.Method == http.MethodGet {
			if strings.HasSuffix(r.URL.Path, "/content") {
				w.Header().Set("Content-Type", "video/mp4")
				_, _ = w.Write(m.videoData)
				return
			}
			m.mu.Lock()
			index := m.videoPolls
			if index >= len(m.videoStates) {
				index = len(m.videoStates) - 1
			}
			m.videoPolls++
			var state any
			if index >= 0 {
				state = m.videoStates[index]
			}
			m.mu.Unlock()
			writeJSON(w, http.StatusOK, state)
			return
		}
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": map[string]any{"message": "not found", "type": "not_found"},
		})
	}
}

func (m *MockOpenAICompatibleServer) captureRequest(w http.ResponseWriter, r *http.Request) map[string]any {
	var body map[string]any
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		body = map[string]any{}
		const maxMultipartBytes = 64 << 20 // bound the body before parsing (gosec)
		r.Body = http.MaxBytesReader(w, r.Body, maxMultipartBytes)
		if err := r.ParseMultipartForm(32 << 20); err == nil { // #nosec G120 -- body bounded by MaxBytesReader above; test-only mock server
			defer func() { _ = r.MultipartForm.RemoveAll() }()
			for key, values := range r.MultipartForm.Value {
				if len(values) == 1 {
					body[key] = values[0]
				} else {
					body[key] = append([]string(nil), values...)
				}
			}
			for key, files := range r.MultipartForm.File {
				var uploads []any
				for _, file := range files {
					opened, err := file.Open()
					if err != nil {
						continue
					}
					data, _ := io.ReadAll(opened)
					_ = opened.Close()
					uploads = append(uploads, map[string]any{"filename": file.Filename, "content_type": file.Header.Get("Content-Type"), "data": string(data)})
				}
				body[key] = uploads
			}
		}
	} else if r.Body != nil {
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
	return body
}

func (m *MockOpenAICompatibleServer) writeStream(w http.ResponseWriter, r *http.Request) {
	m.writeMediaStream(w, r, m.streamChunks)
}

func (m *MockOpenAICompatibleServer) writeMediaStream(w http.ResponseWriter, r *http.Request, chunks []any) {
	closeOnce(m.streamRequestSeen)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	for _, chunk := range chunks {
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

// WithImageResponse configures image generations.
func WithImageResponse(resp any) MockOpenAICompatibleOption {
	return func(m *MockOpenAICompatibleServer) { m.imageResponse = resp }
}

// WithImageEditResponse configures multipart image edits.
func WithImageEditResponse(resp any) MockOpenAICompatibleOption {
	return func(m *MockOpenAICompatibleServer) { m.imageEditResponse = resp }
}

// WithImageStreamResponse configures image SSE events.
func WithImageStreamResponse(events ...any) MockOpenAICompatibleOption {
	return func(m *MockOpenAICompatibleServer) { m.imageStream = events }
}

// WithSpeechResponse configures binary speech.
func WithSpeechResponse(data []byte, contentType string) MockOpenAICompatibleOption {
	return func(m *MockOpenAICompatibleServer) { m.speechData = data; m.speechContentType = contentType }
}

// WithSpeechStreamResponse configures speech SSE events.
func WithSpeechStreamResponse(events ...any) MockOpenAICompatibleOption {
	return func(m *MockOpenAICompatibleServer) { m.speechStream = events }
}

// WithTranscriptionResponse configures one response format; strings are written verbatim.
func WithTranscriptionResponse(format string, resp any) MockOpenAICompatibleOption {
	return func(m *MockOpenAICompatibleServer) { m.transcriptionResponses[format] = resp }
}

// WithVideoResponse configures job creation and successive poll responses.
func WithVideoResponse(create any, states ...any) MockOpenAICompatibleOption {
	return func(m *MockOpenAICompatibleServer) { m.videoCreate = create; m.videoStates = states }
}

// WithVideoContent configures completed MP4 bytes.
func WithVideoContent(data []byte) MockOpenAICompatibleOption {
	return func(m *MockOpenAICompatibleServer) { m.videoData = data }
}
