package anthropicapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v4"
)

const (
	testMessageDelta = "message_delta"
)

func TestNewClient_Defaults(t *testing.T) {
	client := NewClient(ClientConfig{
		APIKey:          "test-key",
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})

	if client.baseURL != defaultBaseURL {
		t.Errorf("expected baseURL=%s, got %s", defaultBaseURL, client.baseURL)
	}
	if client.apiKey != "test-key" {
		t.Errorf("expected apiKey=test-key, got %s", client.apiKey)
	}
}

func TestNewClient_CustomBaseURL(t *testing.T) {
	client := NewClient(ClientConfig{
		BaseURL:         "https://custom.api.com",
		APIKey:          "test-key",
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})

	if client.baseURL != "https://custom.api.com" {
		t.Errorf("expected baseURL=https://custom.api.com, got %s", client.baseURL)
	}
}

func TestNewClient_WithCustomHTTPClient(t *testing.T) {
	client := NewClient(ClientConfig{
		APIKey:     "test-key",
		HTTPClient: &http.Client{},
	})

	if client.httpClient == nil {
		t.Error("expected HTTP client to be configured")
	}
}

func TestClient_CreateMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.URL.Path != "/messages" {
			t.Errorf("expected path=/messages, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("expected x-api-key=test-key, got %s", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != anthropicVersion {
			t.Errorf("expected anthropic-version=%s, got %s", anthropicVersion, r.Header.Get("anthropic-version"))
		}

		// Parse request
		var req MessagesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Model != "claude-sonnet-4-20250514" {
			t.Errorf("expected model=claude-sonnet-4-20250514, got %s", req.Model)
		}

		// Send response
		resp := MessagesResponse{
			ID:   "msg_123",
			Type: "message",
			Role: "assistant",
			Content: []ContentPart{
				{Type: "text", Text: "Hello!"},
			},
			StopReason: "end_turn",
			Usage:      Usage{InputTokens: 10, OutputTokens: 5},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:         server.URL,
		APIKey:          "test-key",
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})

	resp, err := client.CreateMessage(context.Background(), &MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []Message{
			{Role: "user", Content: []ContentPart{{Type: "text", Text: "Hello"}}},
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.ID != "msg_123" {
		t.Errorf("expected id=msg_123, got %s", resp.ID)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("expected 1 content, got %d", len(resp.Content))
	}
	if resp.Content[0].Text != "Hello!" {
		t.Errorf("expected text=Hello!, got %s", resp.Content[0].Text)
	}
}

func TestClient_CreateMessage_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type":    "authentication_error",
			"message": "Invalid API key",
		})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:         server.URL,
		APIKey:          "bad-key",
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})

	_, err := client.CreateMessage(context.Background(), &MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages:  []Message{{Role: "user", Content: []ContentPart{{Type: "text", Text: "Hi"}}}},
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "Invalid API key") {
		t.Errorf("expected error to contain 'Invalid API key', got %s", err.Error())
	}
}

func TestClient_CreateMessageStream(t *testing.T) {
	sseData := `event: message_start
data: {"type":"message_start","message":{"id":"msg_123","role":"assistant","content":[],"usage":{"input_tokens":10,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}

`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req MessagesRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		if !req.Stream {
			t.Error("expected stream=true in request")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sseData))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:         server.URL,
		APIKey:          "test-key",
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})

	stream, err := client.CreateMessageStream(context.Background(), &MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages:  []Message{{Role: "user", Content: []ContentPart{{Type: "text", Text: "Hello"}}}},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	// Read message_start
	event, err := stream.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != "message_start" {
		t.Errorf("expected type=message_start, got %s", event.Type)
	}

	// Read content_block_start
	event, err = stream.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != "content_block_start" {
		t.Errorf("expected type=content_block_start, got %s", event.Type)
	}

	// Read first delta
	event, err = stream.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != "content_block_delta" {
		t.Errorf("expected type=content_block_delta, got %s", event.Type)
	}
	if event.Delta == nil || event.Delta.Text != "Hello" {
		t.Errorf("expected delta text=Hello, got %v", event.Delta)
	}

	// Read second delta
	event, err = stream.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Delta.Text != " world" {
		t.Errorf("expected delta text=' world', got %s", event.Delta.Text)
	}

	// Read content_block_stop
	event, err = stream.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != "content_block_stop" {
		t.Errorf("expected type=content_block_stop, got %s", event.Type)
	}

	// Read message_delta
	event, err = stream.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != testMessageDelta {
		t.Errorf("expected type=message_delta, got %s", event.Type)
	}

	// Read message_stop
	event, err = stream.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != "message_stop" {
		t.Errorf("expected type=message_stop, got %s", event.Type)
	}
	if !IsEndOfStream(event) {
		t.Error("expected IsEndOfStream to return true")
	}
}

func TestStreamReader_ErrorEvent(t *testing.T) {
	sseData := `event: message_start
data: {"type":"message_start","message":{"id":"msg_123","role":"assistant","content":[],"usage":{"input_tokens":10,"output_tokens":0}}}

event: error
data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}

`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sseData))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:         server.URL,
		APIKey:          "test-key",
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})

	stream, err := client.CreateMessageStream(context.Background(), &MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages:  []Message{{Role: "user", Content: []ContentPart{{Type: "text", Text: "Hello"}}}},
	})
	if err != nil {
		t.Fatalf("CreateMessageStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	event, err := stream.Read()
	if err != nil {
		t.Fatalf("first Read: %v", err)
	}
	if event.Type != "message_start" {
		t.Fatalf("expected message_start, got %s", event.Type)
	}

	_, err = stream.Read()
	if err == nil {
		t.Fatal("expected error event to return error")
	}
	if errors.Is(err, io.EOF) {
		t.Fatalf("expected non-EOF error, got %v", err)
	}
	var apiErr *llms.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T: %v", err, err)
	}
	if apiErr.Type != "overloaded_error" || apiErr.Message != "Overloaded" {
		t.Fatalf("unexpected APIError: %#v", apiErr)
	}
}

func TestBuildTextMessage(t *testing.T) {
	msg := BuildTextMessage("user", "Hello world")

	if msg.Role != "user" {
		t.Errorf("expected role=user, got %s", msg.Role)
	}
	if len(msg.Content) != 1 {
		t.Fatalf("expected 1 content part, got %d", len(msg.Content))
	}
	if msg.Content[0].Type != "text" {
		t.Errorf("expected type=text, got %s", msg.Content[0].Type)
	}
	if msg.Content[0].Text != "Hello world" {
		t.Errorf("expected text='Hello world', got %s", msg.Content[0].Text)
	}
}

func TestBuildToolResultMessage(t *testing.T) {
	msg := BuildToolResultMessage("toolu_123", `{"temp": 72}`, false)

	if msg.Role != "user" {
		t.Errorf("expected role=user, got %s", msg.Role)
	}
	if len(msg.Content) != 1 {
		t.Fatalf("expected 1 content part, got %d", len(msg.Content))
	}
	if msg.Content[0].Type != "tool_result" {
		t.Errorf("expected type=tool_result, got %s", msg.Content[0].Type)
	}
	if msg.Content[0].ToolUseID != "toolu_123" {
		t.Errorf("expected tool_use_id=toolu_123, got %s", msg.Content[0].ToolUseID)
	}
	if msg.Content[0].IsError {
		t.Error("expected is_error=false")
	}
}

func TestBuildToolResultMessage_WithError(t *testing.T) {
	msg := BuildToolResultMessage("toolu_123", "Error: not found", true)

	if !msg.Content[0].IsError {
		t.Error("expected is_error=true")
	}
}

func TestExtractTextContent(t *testing.T) {
	tests := []struct {
		name     string
		content  []ContentPart
		expected string
	}{
		{
			name:     "empty",
			content:  []ContentPart{},
			expected: "",
		},
		{
			name: "single text",
			content: []ContentPart{
				{Type: "text", Text: "Hello"},
			},
			expected: "Hello",
		},
		{
			name: "multiple text",
			content: []ContentPart{
				{Type: "text", Text: "Hello "},
				{Type: "text", Text: "world"},
			},
			expected: "Hello world",
		},
		{
			name: "mixed types",
			content: []ContentPart{
				{Type: "text", Text: "Hello"},
				{Type: "tool_use", ID: "toolu_123"},
				{Type: "text", Text: " world"},
			},
			expected: "Hello world",
		},
		{
			name: "only tool_use",
			content: []ContentPart{
				{Type: "tool_use", ID: "toolu_123", Name: "get_weather"},
			},
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ExtractTextContent(tc.content)
			if result != tc.expected {
				t.Errorf("expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

func TestIsEndOfStream(t *testing.T) {
	tests := []struct {
		event    *StreamEvent
		expected bool
	}{
		{&StreamEvent{Type: "message_stop"}, true},
		{&StreamEvent{Type: "message_start"}, false},
		{&StreamEvent{Type: "content_block_delta"}, false},
		{&StreamEvent{Type: ""}, false},
	}

	for _, tc := range tests {
		result := IsEndOfStream(tc.event)
		if result != tc.expected {
			t.Errorf("IsEndOfStream(%s) = %v, expected %v", tc.event.Type, result, tc.expected)
		}
	}
}

func TestIsStreamError(t *testing.T) {
	if IsStreamError(nil) {
		t.Error("expected false for nil error")
	}
	if IsStreamError(io.EOF) {
		t.Error("expected false for io.EOF")
	}
	if !IsStreamError(io.ErrUnexpectedEOF) {
		t.Error("expected true for other errors")
	}
}
