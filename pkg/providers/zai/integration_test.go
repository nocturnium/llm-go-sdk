package zai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v2"
)

// setupMockServer creates a test server and returns a client configured to use it.
func setupMockServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	return client
}

func TestClient_GenerateContent_Integration(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and path
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected /chat/completions, got %s", r.URL.Path)
		}

		// Verify Authorization header
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Error("missing or invalid Authorization header")
		}

		// Verify Accept-Language header (required by Z.AI)
		if r.Header.Get("Accept-Language") != "en-US,en" {
			t.Errorf("expected Accept-Language 'en-US,en', got %s", r.Header.Get("Accept-Language"))
		}

		// Verify Content-Type
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		// Parse request body
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		if req["model"] != ModelGLM47 {
			t.Errorf("expected model=%s, got %v", ModelGLM47, req["model"])
		}

		// Send response
		resp := map[string]any{
			"id":      "chatcmpl-zai-123",
			"object":  "chat.completion",
			"created": 1677652288,
			"model":   ModelGLM47,
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hello! I'm GLM-4.7. How can I help you today?",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 12,
				"total_tokens":      22,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	ctx := context.Background()
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Hello"},
	}

	resp, err := client.GenerateContent(ctx, messages)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	if resp.Content != "Hello! I'm GLM-4.7. How can I help you today?" {
		t.Errorf("unexpected content: %s", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("unexpected finish_reason: %s", resp.FinishReason)
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("unexpected prompt_tokens: %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 12 {
		t.Errorf("unexpected completion_tokens: %d", resp.Usage.CompletionTokens)
	}
}

func TestClient_GenerateContent_WithTools(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)

		// Verify tools were sent
		tools, ok := req["tools"].([]any)
		if !ok || len(tools) == 0 {
			t.Error("expected tools in request")
		}

		// Respond with tool call
		resp := map[string]any{
			"id":    "chatcmpl-zai-456",
			"model": ModelGLM47,
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "",
						"tool_calls": []map[string]any{
							{
								"id":   "call_zai_123",
								"type": "function",
								"function": map[string]any{
									"name":      "get_weather",
									"arguments": `{"location": "Beijing"}`,
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
	})

	weatherTool := llms.NewFunctionTool("get_weather", "Get weather for a location", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"location": map[string]any{"type": "string"},
		},
	})

	resp, err := client.GenerateContent(
		context.Background(),
		[]llms.Message{{Role: llms.RoleUser, Content: "What's the weather in Beijing?"}},
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

	if tc.ID != "call_zai_123" {
		t.Errorf("unexpected tool call ID: %s", tc.ID)
	}

	type Args struct {
		Location string `json:"location"`
	}
	args, err := llms.ParseToolArguments[Args](*tc)
	if err != nil {
		t.Fatalf("failed to parse arguments: %v", err)
	}
	if args.Location != "Beijing" {
		t.Errorf("unexpected location: %s", args.Location)
	}
}

func TestClient_GenerateContent_ErrorResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   map[string]any
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
		},
		{
			name:       "server error",
			statusCode: 500,
			response: map[string]any{
				"error": map[string]any{
					"message": "Internal server error",
					"type":    "server_error",
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := setupMockServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.statusCode)
				_ = json.NewEncoder(w).Encode(tc.response)
			})

			_, err := client.GenerateContent(
				context.Background(),
				[]llms.Message{{Role: llms.RoleUser, Content: "Hello"}},
			)

			if err == nil {
				t.Fatal("expected error, got nil")
			}

			// Verify the error contains status code information
			var apiErr *llms.APIError
			if errors.As(err, &apiErr) {
				if apiErr.StatusCode != tc.statusCode {
					t.Errorf("expected status %d, got %d", tc.statusCode, apiErr.StatusCode)
				}
			}
		})
	}
}

func TestClient_Stream_Integration(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Verify streaming request
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)

		if req["stream"] != true {
			t.Error("expected stream=true in request")
		}

		// Verify Accept-Language header
		if r.Header.Get("Accept-Language") != "en-US,en" {
			t.Errorf("expected Accept-Language 'en-US,en', got %s", r.Header.Get("Accept-Language"))
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
			`{"id":"chatcmpl-zai-stream","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
			`{"id":"chatcmpl-zai-stream","choices":[{"index":0,"delta":{"content":"你好"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-zai-stream","choices":[{"index":0,"delta":{"content":"！"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-zai-stream","choices":[{"index":0,"delta":{"content":"我是"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-zai-stream","choices":[{"index":0,"delta":{"content":"GLM"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-zai-stream","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":4,"total_tokens":9}}`,
		}

		for _, chunk := range chunks {
			_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
			flusher.Flush()
			time.Sleep(5 * time.Millisecond)
		}

		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	})

	ctx := context.Background()
	chunks, err := client.Stream(ctx, []llms.Message{{Role: llms.RoleUser, Content: "你好"}})
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

	if content.String() != "你好！我是GLM" {
		t.Errorf("unexpected content: %s", content.String())
	}
	if finalChunk.FinishReason != "stop" {
		t.Errorf("unexpected finish_reason: %s", finalChunk.FinishReason)
	}
}

func TestClient_Stream_ContextCancellation(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not implement http.Flusher")
		}

		// Send chunks slowly to allow cancellation
		for i := 0; i < 100; i++ {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			chunk := `{"id":"chatcmpl-zai-slow","choices":[{"index":0,"delta":{"content":"x"},"finish_reason":null}]}`
			_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
			flusher.Flush()
			time.Sleep(50 * time.Millisecond)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	chunks, err := client.Stream(ctx, []llms.Message{{Role: llms.RoleUser, Content: "Hi"}})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var count int
	for chunk := range chunks {
		if chunk.Done {
			// Error may be wrapped, so check for context deadline
			if chunk.Error != nil && !errors.Is(chunk.Error, context.DeadlineExceeded) {
				// Accept any context-related error
				if !strings.Contains(chunk.Error.Error(), "context") {
					t.Logf("got error: %v", chunk.Error)
				}
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
	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL("https://api.z.ai/api/paas/v4"),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

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

func TestClient_Call_Convenience(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		// Verify the prompt is in a user message
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)

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
		if msg["content"] != "Hello GLM" {
			t.Errorf("expected content='Hello GLM', got %v", msg["content"])
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "chatcmpl-zai-call",
			"model": ModelGLM47,
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hello! I'm GLM-4.7!",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     5,
				"completion_tokens": 6,
				"total_tokens":      11,
			},
		})
	})

	result, err := client.Call(context.Background(), "Hello GLM")
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if result != "Hello! I'm GLM-4.7!" {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestClient_AcceptLanguageHeader(t *testing.T) {
	headerReceived := ""

	client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		headerReceived = r.Header.Get("Accept-Language")

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "chatcmpl-header-test",
			"model": ModelGLM47,
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "OK",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     1,
				"completion_tokens": 1,
				"total_tokens":      2,
			},
		})
	})

	_, err := client.Call(context.Background(), "Test")
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if headerReceived != "en-US,en" {
		t.Errorf("expected Accept-Language 'en-US,en', got '%s'", headerReceived)
	}
}

func TestClient_JSONMode(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)

		// Verify response_format is set for JSON mode
		responseFormat, ok := req["response_format"].(map[string]any)
		if !ok {
			t.Error("expected response_format in request")
		} else if responseFormat["type"] != "json_object" {
			t.Errorf("expected response_format.type='json_object', got %v", responseFormat["type"])
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "chatcmpl-json",
			"model": ModelGLM47,
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": `{"name": "GLM", "version": "4.7"}`,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 15,
				"total_tokens":      25,
			},
		})
	})

	resp, err := client.GenerateContent(
		context.Background(),
		[]llms.Message{{Role: llms.RoleUser, Content: "Return JSON with name and version"}},
		llms.WithJSONMode(),
	)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	// Verify we got valid JSON back
	var result map[string]string
	if err := json.Unmarshal([]byte(resp.Content), &result); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}

	if result["name"] != "GLM" {
		t.Errorf("unexpected name: %s", result["name"])
	}
	if result["version"] != "4.7" {
		t.Errorf("unexpected version: %s", result["version"])
	}
}

func TestClient_WithSystemMessage(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)

		messages, ok := req["messages"].([]any)
		if !ok || len(messages) < 2 {
			t.Error("expected at least 2 messages (system + user)")
		}

		// Verify system message
		sysMsg, ok := messages[0].(map[string]any)
		if !ok {
			t.Fatal("first message is not a map")
		}
		if sysMsg["role"] != "system" {
			t.Errorf("expected first message role=system, got %v", sysMsg["role"])
		}
		if sysMsg["content"] != "You are a helpful assistant." {
			t.Errorf("unexpected system content: %v", sysMsg["content"])
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "chatcmpl-system",
			"model": ModelGLM47,
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "I'm here to help!",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     15,
				"completion_tokens": 5,
				"total_tokens":      20,
			},
		})
	})

	messages := []llms.Message{
		{Role: llms.RoleSystem, Content: "You are a helpful assistant."},
		{Role: llms.RoleUser, Content: "Hi"},
	}

	resp, err := client.GenerateContent(context.Background(), messages)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	if resp.Content != "I'm here to help!" {
		t.Errorf("unexpected content: %s", resp.Content)
	}
}
