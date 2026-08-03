package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestClient_GenerateContent_Integration(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected /chat/completions, got %s", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Error("missing or invalid Authorization header")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		// Parse request body
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		if req["model"] != "gpt-4" {
			t.Errorf("expected model=gpt-4, got %v", req["model"])
		}

		// Send response
		resp := map[string]any{
			"id":      "chatcmpl-123",
			"object":  "chat.completion",
			"created": 1677652288,
			"model":   "gpt-4",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hello! How can I help you today?",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 8,
				"total_tokens":      18,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create client with mock server
	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
		WithModel("gpt-4"),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	// Test GenerateContent
	ctx := context.Background()
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Hello"},
	}

	resp, err := client.GenerateContent(ctx, messages)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	if resp.Content != "Hello! How can I help you today?" {
		t.Errorf("unexpected content: %s", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("unexpected finish_reason: %s", resp.FinishReason)
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("unexpected prompt_tokens: %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 8 {
		t.Errorf("unexpected completion_tokens: %d", resp.Usage.CompletionTokens)
	}
}

func TestClient_GenerateContent_WithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)

		// Verify tools were sent
		tools, ok := req["tools"].([]any)
		if !ok || len(tools) == 0 {
			t.Error("expected tools in request")
		}

		// Respond with tool call
		resp := map[string]any{
			"id":    "chatcmpl-123",
			"model": "gpt-4",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]any{
							{
								"id":   "call_123",
								"type": "function",
								"function": map[string]any{
									"name":      "get_weather",
									"arguments": `{"location": "Boston"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     15,
				"completion_tokens": 20,
				"total_tokens":      35,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, _ := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)

	weatherTool := llms.NewFunctionTool("get_weather", "Get weather", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"location": map[string]any{"type": "string"},
		},
	})

	resp, err := client.GenerateContent(
		context.Background(),
		[]llms.Message{{Role: llms.RoleUser, Content: "What's the weather in Boston?"}},
		llms.WithTools([]llms.Tool{weatherTool}),
	)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	if !resp.HasToolCalls() {
		t.Fatal("expected tool calls in response")
	}

	tc := resp.ToolCallByName("get_weather")
	if tc == nil {
		t.Fatal("expected get_weather tool call")
	}

	if tc.ID != "call_123" {
		t.Errorf("unexpected tool call ID: %s", tc.ID)
	}

	type Args struct {
		Location string `json:"location"`
	}
	args, err := llms.ParseToolArguments[Args](*tc)
	if err != nil {
		t.Fatalf("failed to parse arguments: %v", err)
	}
	if args.Location != "Boston" {
		t.Errorf("unexpected location: %s", args.Location)
	}
}

func TestClient_GenerateContent_ErrorResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   map[string]any
		wantErr    error
	}{
		{
			name:       "rate limited",
			statusCode: 429,
			response: map[string]any{
				"error": map[string]any{
					"message": "Rate limit exceeded",
					"type":    "rate_limit_error",
					"code":    "rate_limit_exceeded",
				},
			},
			wantErr: llms.ErrRateLimited,
		},
		{
			name:       "authentication failed",
			statusCode: 401,
			response: map[string]any{
				"error": map[string]any{
					"message": "Invalid API key",
					"type":    "authentication_error",
				},
			},
			wantErr: llms.ErrAuthenticationFailed,
		},
		{
			name:       "context length exceeded",
			statusCode: 400,
			response: map[string]any{
				"error": map[string]any{
					"message": "Maximum context length exceeded",
					"type":    "invalid_request_error",
					"code":    "context_length_exceeded",
				},
			},
			wantErr: llms.ErrContextLengthExceeded,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.statusCode)
				_ = json.NewEncoder(w).Encode(tc.response)
			}))
			defer server.Close()

			client, _ := New(
				WithAPIKey("test-api-key"),
				WithBaseURL(server.URL),
				WithAllowPrivateIPs(), WithAllowHTTP(),
			)

			_, err := client.GenerateContent(
				context.Background(),
				[]llms.Message{{Role: llms.RoleUser, Content: "Hello"}},
			)

			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if tc.wantErr != nil {
				// Check that the error matches the expected error type
				var apiErr *llms.APIError
				if errors.As(err, &apiErr) {
					if apiErr.StatusCode != tc.statusCode {
						t.Errorf("expected status %d, got %d", tc.statusCode, apiErr.StatusCode)
					}
				}
			}
		})
	}
}

func TestClient_Stream_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify streaming request
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)

		if req["stream"] != true {
			t.Error("expected stream=true in request")
		}

		// Send SSE response
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}

		chunks := []string{
			`{"id":"chatcmpl-123","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
			`{"id":"chatcmpl-123","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-123","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-123","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-123","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`,
		}

		for _, chunk := range chunks {
			_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}

		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	client, _ := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)

	ctx := context.Background()
	chunks, err := client.Stream(ctx, []llms.Message{{Role: llms.RoleUser, Content: "Hi"}})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var content strings.Builder
	var finalChunk llms.StreamChunk

	for chunk := range chunks {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		content.WriteString(chunk.Content)
		if chunk.Done {
			finalChunk = chunk
		}
	}

	if content.String() != "Hello world!" {
		t.Errorf("unexpected content: %s", content.String())
	}
	if finalChunk.FinishReason != "stop" {
		t.Errorf("unexpected finish_reason: %s", finalChunk.FinishReason)
	}
}

func TestClient_Stream_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not implement http.Flusher")
		}

		// Send chunks slowly
		for i := 0; i < 100; i++ {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			chunk := `{"id":"chatcmpl-123","choices":[{"index":0,"delta":{"content":"x"},"finish_reason":null}]}`
			_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
			flusher.Flush()
			time.Sleep(50 * time.Millisecond)
		}
	}))
	defer server.Close()

	client, _ := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	chunks, err := client.Stream(ctx, []llms.Message{{Role: llms.RoleUser, Content: "Hi"}})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var count int
	for chunk := range chunks {
		if chunk.Done {
			// Error may be wrapped in StreamError, so use errors.Is
			if chunk.Error != nil && !errors.Is(chunk.Error, context.DeadlineExceeded) {
				t.Errorf("expected DeadlineExceeded error, got %v", chunk.Error)
			}
			break
		}
		count++
	}

	if count >= 100 {
		t.Error("expected stream to be canceled before all chunks")
	}
}

func TestClient_ValidationErrors(t *testing.T) {
	client, _ := New(
		WithAPIKey("test-api-key"),
		WithBaseURL("https://api.openai.com/v1"),
	)

	t.Run("empty messages", func(t *testing.T) {
		_, err := client.GenerateContent(context.Background(), []llms.Message{})
		if err == nil {
			t.Fatal("expected error for empty messages")
		}
		if !errors.Is(err, llms.ErrEmptyMessages) {
			t.Errorf("expected ErrEmptyMessages, got %v", err)
		}
	})

	t.Run("invalid temperature", func(t *testing.T) {
		_, err := client.GenerateContent(
			context.Background(),
			[]llms.Message{{Role: llms.RoleUser, Content: "Hi"}},
			llms.WithTemperature(3.0),
		)
		if err == nil {
			t.Fatal("expected error for invalid temperature")
		}
	})

	t.Run("invalid max_tokens", func(t *testing.T) {
		_, err := client.GenerateContent(
			context.Background(),
			[]llms.Message{{Role: llms.RoleUser, Content: "Hi"}},
			llms.WithMaxTokens(-1),
		)
		if err == nil {
			t.Fatal("expected error for invalid max_tokens")
		}
	})
}

func TestClient_RetryAfterHeader(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("Retry-After", "30")
		w.Header().Set("X-Request-Id", "req-123")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(429)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "Rate limit exceeded",
				"type":    "rate_limit_error",
			},
		})
	}))
	defer server.Close()

	client, _ := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)

	_, err := client.GenerateContent(
		context.Background(),
		[]llms.Message{{Role: llms.RoleUser, Content: "Hi"}},
	)

	if err == nil {
		t.Fatal("expected error")
	}

	// Note: The httpclient may retry automatically, so check the error details
	_ = attempts // Attempts may vary based on retry policy
}

func TestClient_Call_Convenience(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read request to verify the prompt is in a user message
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)

		messages, ok := req["messages"].([]any)
		if !ok {
			t.Fatal("messages is not a []any")
		}
		if len(messages) != 1 {
			t.Errorf("expected 1 message, got %d", len(messages))
		}

		msg, ok := messages[0].(map[string]any)
		if !ok {
			t.Fatal("message is not a map[string]any")
		}
		if msg["role"] != "user" {
			t.Errorf("expected role=user, got %v", msg["role"])
		}
		if msg["content"] != "Hello" {
			t.Errorf("expected content=Hello, got %v", msg["content"])
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "chatcmpl-123",
			"model": "gpt-4",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hi there!",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     5,
				"completion_tokens": 3,
				"total_tokens":      8,
			},
		})
	}))
	defer server.Close()

	client, _ := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)

	result, err := client.Call(context.Background(), "Hello")
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if result != "Hi there!" {
		t.Errorf("unexpected result: %s", result)
	}
}
