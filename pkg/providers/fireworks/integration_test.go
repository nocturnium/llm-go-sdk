package fireworks

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v5"
)

func TestClient_GenerateContent_Integration(t *testing.T) {
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

		// Parse request
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		// Send OpenAI-compatible response
		resp := map[string]any{
			"id":      "chatcmpl-123",
			"object":  "chat.completion",
			"created": 1677652288,
			"model":   "accounts/fireworks/models/llama-v3p1-70b-instruct",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hello! I'm running on Fireworks AI.",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 7,
				"total_tokens":      17,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Hello"},
	}

	resp, err := client.GenerateContent(ctx, messages)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	if resp.Content != "Hello! I'm running on Fireworks AI." {
		t.Errorf("unexpected content: %s", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("unexpected finish_reason: %s", resp.FinishReason)
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("unexpected prompt_tokens: %d", resp.Usage.PromptTokens)
	}
}

func TestClient_GenerateContent_WithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		// Verify tools were sent
		tools, ok := req["tools"].([]any)
		if !ok || len(tools) == 0 {
			t.Error("expected tools in request")
		}

		// Respond with tool call
		resp := map[string]any{
			"id":    "chatcmpl-123",
			"model": "accounts/fireworks/models/llama-v3p1-70b-instruct",
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

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

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
}

func TestClient_Call_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":    "chatcmpl-123",
			"model": "accounts/fireworks/models/llama-v3p1-70b-instruct",
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
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	result, err := client.Call(context.Background(), "Hello")
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if result != "Hi there!" {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestClient_Stream_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		if req["stream"] != true {
			t.Error("expected stream=true in request")
		}

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
			`{"id":"chatcmpl-123","choices":[{"index":0,"delta":{"content":" from"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-123","choices":[{"index":0,"delta":{"content":" Fireworks"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-123","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-123","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":5,"total_tokens":10}}`,
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

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

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

	if content.String() != "Hello from Fireworks!" {
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

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	chunks, err := client.Stream(ctx, []llms.Message{{Role: llms.RoleUser, Content: "Hi"}})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var count int
	for chunk := range chunks {
		if chunk.Done {
			break
		}
		count++
	}

	if count >= 100 {
		t.Error("expected stream to be canceled before all chunks")
	}
}

func TestClient_Embed_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("expected /embeddings, got %s", r.URL.Path)
		}

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		resp := map[string]any{
			"object": "list",
			"data": []map[string]any{
				{
					"object":    "embedding",
					"embedding": []float32{0.1, 0.2, 0.3, 0.4, 0.5},
					"index":     0,
				},
			},
			"model": "nomic-ai/nomic-embed-text-v1.5",
			"usage": map[string]any{
				"prompt_tokens": 5,
				"total_tokens":  5,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	resp, err := client.Embed(context.Background(), []string{"test text"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(resp.Embeddings) != 1 {
		t.Fatalf("expected 1 embedding, got %d", len(resp.Embeddings))
	}
	if len(resp.Embeddings[0].Vector) != 5 {
		t.Errorf("expected 5-dimensional vector, got %d", len(resp.Embeddings[0].Vector))
	}
}

func TestClient_EmbedBatch_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		// Verify batch input
		input, ok := req["input"].([]any)
		if !ok || len(input) != 3 {
			t.Errorf("expected 3 input texts, got %v", input)
		}

		resp := map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"object": "embedding", "embedding": []float32{0.1, 0.2}, "index": 0},
				{"object": "embedding", "embedding": []float32{0.3, 0.4}, "index": 1},
				{"object": "embedding", "embedding": []float32{0.5, 0.6}, "index": 2},
			},
			"model": "nomic-ai/nomic-embed-text-v1.5",
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	resp, err := client.Embed(context.Background(), []string{"text1", "text2", "text3"})
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(resp.Embeddings) != 3 {
		t.Fatalf("expected 3 embeddings, got %d", len(resp.Embeddings))
	}
}

func TestClient_EmbedQuery_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"object": "embedding", "embedding": []float32{0.1, 0.2, 0.3}, "index": 0},
			},
			"model": "nomic-ai/nomic-embed-text-v1.5",
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	vector, err := llms.EmbedQuery(context.Background(), client, "search query")
	if err != nil {
		t.Fatalf("EmbedQuery failed: %v", err)
	}

	if len(vector) != 3 {
		t.Errorf("expected 3-dimensional vector, got %d", len(vector))
	}
}

func TestClient_EmbedDocuments_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"object": "embedding", "embedding": []float32{0.1, 0.2}, "index": 0},
				{"object": "embedding", "embedding": []float32{0.3, 0.4}, "index": 1},
			},
			"model": "nomic-ai/nomic-embed-text-v1.5",
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	vectors, err := llms.EmbedDocuments(context.Background(), client, []string{"doc1", "doc2"})
	if err != nil {
		t.Fatalf("EmbedDocuments failed: %v", err)
	}

	if len(vectors) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vectors))
	}
}

func TestClient_ErrorResponses(t *testing.T) {
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
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.statusCode)
				_ = json.NewEncoder(w).Encode(tc.response)
			}))
			defer server.Close()

			client, err := New(
				WithAPIKey("test-api-key"),
				WithBaseURL(server.URL),
				WithAllowPrivateIPs(), WithAllowHTTP(),
			)
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			_, err = client.GenerateContent(
				context.Background(),
				[]llms.Message{{Role: llms.RoleUser, Content: "Hello"}},
			)

			if err == nil {
				t.Fatal("expected error, got nil")
			}

			var apiErr *llms.APIError
			if errors.As(err, &apiErr) {
				if apiErr.StatusCode != tc.statusCode {
					t.Errorf("expected status %d, got %d", tc.statusCode, apiErr.StatusCode)
				}
			}
		})
	}
}

func TestClient_ValidationErrors(t *testing.T) {
	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL("https://api.fireworks.ai/inference/v1"),
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
}

func TestClient_JSONMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		responseFormat, ok := req["response_format"].(map[string]any)
		if !ok {
			t.Error("expected response_format in request")
		} else if responseFormat["type"] != "json_object" {
			t.Errorf("expected response_format.type=json_object, got %v", responseFormat["type"])
		}

		resp := map[string]any{
			"id":    "chatcmpl-123",
			"model": "accounts/fireworks/models/llama-v3p1-70b-instruct",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": `{"status": "ok"}`,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	resp, err := client.GenerateContent(
		context.Background(),
		[]llms.Message{{Role: llms.RoleUser, Content: "Give me JSON"}},
		llms.WithJSONMode(),
	)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	if resp.Content != `{"status": "ok"}` {
		t.Errorf("unexpected content: %s", resp.Content)
	}
}

func TestClient_EnvVarFallbacks(t *testing.T) {
	tests := []struct {
		name   string
		envVar string
	}{
		{"FIREWORKS_API_KEY", "FIREWORKS_API_KEY"},
		{"LLM_API_KEY", "LLM_API_KEY"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FIREWORKS_API_KEY", "")
			t.Setenv("LLM_API_KEY", "")

			t.Setenv(tc.envVar, "test-key-from-env")

			client, err := New()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if client.Provider() != llms.ProviderFireworks {
				t.Errorf("expected provider fireworks, got %s", client.Provider())
			}
		})
	}
}

func TestClient_GetModel(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithModel("accounts/fireworks/models/mixtral-8x22b-instruct"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Model() != "accounts/fireworks/models/mixtral-8x22b-instruct" {
		t.Errorf("unexpected model: %s", client.Model())
	}
}

func TestClient_WithEmbeddingModel(t *testing.T) {
	opts := apply(
		WithAPIKey("test-key"),
		WithEmbeddingModel("custom-embed-model"),
	)

	if opts.EmbeddingModel != "custom-embed-model" {
		t.Errorf("unexpected embedding model: %s", opts.EmbeddingModel)
	}
}

func TestClient_WithHTTPClient(t *testing.T) {
	// Just verify the option can be applied without error
	opts := apply(
		WithAPIKey("test-key"),
		WithHTTPClient(nil),
	)

	if opts.APIKey != "test-key" {
		t.Errorf("unexpected API key: %s", opts.APIKey)
	}
}
