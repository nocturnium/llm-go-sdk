package perplexity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v4"
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
			"model":   "llama-3.1-sonar-large-128k-online",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Hello! I'm Perplexity AI.",
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

	ctx := context.Background()
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Hello"},
	}

	resp, err := client.GenerateContent(ctx, messages)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	if resp.Content != "Hello! I'm Perplexity AI." {
		t.Errorf("unexpected content: %s", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("unexpected finish_reason: %s", resp.FinishReason)
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("unexpected prompt_tokens: %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 5 {
		t.Errorf("unexpected completion_tokens: %d", resp.Usage.CompletionTokens)
	}
}

func TestClient_Call_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		// Verify the messages were sent correctly
		messages, ok := req["messages"].([]any)
		if !ok || len(messages) != 1 {
			t.Errorf("expected 1 message, got %v", messages)
		}

		resp := map[string]any{
			"id":    "chatcmpl-123",
			"model": "llama-3.1-sonar-large-128k-online",
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

			// Verify the error contains the expected status code
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
		WithBaseURL("https://api.perplexity.ai"),
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

func TestClient_JSONMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		// Verify JSON mode was requested
		responseFormat, ok := req["response_format"].(map[string]any)
		if !ok {
			t.Error("expected response_format in request")
		} else if responseFormat["type"] != "json_object" {
			t.Errorf("expected response_format.type=json_object, got %v", responseFormat["type"])
		}

		resp := map[string]any{
			"id":    "chatcmpl-123",
			"model": "llama-3.1-sonar-large-128k-online",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": `{"result": "success"}`,
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

	if resp.Content != `{"result": "success"}` {
		t.Errorf("unexpected content: %s", resp.Content)
	}
}

func TestClient_WithCallOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		// Verify call options were passed
		if temp, ok := req["temperature"].(float64); !ok || temp != 0.7 {
			t.Errorf("expected temperature=0.7, got %v", req["temperature"])
		}
		if maxTokens, ok := req["max_tokens"].(float64); !ok || maxTokens != 1000 {
			t.Errorf("expected max_tokens=1000, got %v", req["max_tokens"])
		}
		if topP, ok := req["top_p"].(float64); !ok || topP != 0.9 {
			t.Errorf("expected top_p=0.9, got %v", req["top_p"])
		}

		resp := map[string]any{
			"id":    "chatcmpl-123",
			"model": "llama-3.1-sonar-large-128k-online",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Response",
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

	_, err = client.GenerateContent(
		context.Background(),
		[]llms.Message{{Role: llms.RoleUser, Content: "Hi"}},
		llms.WithTemperature(0.7),
		llms.WithMaxTokens(1000),
		llms.WithTopP(0.9),
	)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}
}

func TestClient_EnvVarFallbacks(t *testing.T) {
	tests := []struct {
		name   string
		envVar string
	}{
		{"PERPLEXITY_API_KEY", "PERPLEXITY_API_KEY"},
		{"PPLX_API_KEY", "PPLX_API_KEY"},
		{"LLM_API_KEY", "LLM_API_KEY"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Clear all API key env vars
			t.Setenv("PERPLEXITY_API_KEY", "")
			t.Setenv("PPLX_API_KEY", "")
			t.Setenv("LLM_API_KEY", "")

			// Set only the one we're testing
			t.Setenv(tc.envVar, "test-key-from-env")

			client, err := New()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if client.Provider() != llms.ProviderPerplexity {
				t.Errorf("expected provider perplexity, got %s", client.Provider())
			}
		})
	}
}

func TestClient_GetModel(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithModel("llama-3.1-sonar-small-128k-online"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Model() != "llama-3.1-sonar-small-128k-online" {
		t.Errorf("unexpected model: %s", client.Model())
	}
}

func TestClient_SystemMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		messages, ok := req["messages"].([]any)
		if !ok {
			t.Fatal("expected messages array")
		}

		if len(messages) != 2 {
			t.Errorf("expected 2 messages, got %d", len(messages))
		}

		// First message should be system
		firstMsg, ok := messages[0].(map[string]any)
		if !ok {
			t.Fatal("first message is not a map")
		}
		if firstMsg["role"] != "system" {
			t.Errorf("expected first message role=system, got %v", firstMsg["role"])
		}

		resp := map[string]any{
			"id":    "chatcmpl-123",
			"model": "llama-3.1-sonar-large-128k-online",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "Response with system context",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     15,
				"completion_tokens": 5,
				"total_tokens":      20,
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

	messages := []llms.Message{
		{Role: llms.RoleSystem, Content: "You are a helpful assistant."},
		{Role: llms.RoleUser, Content: "Hello"},
	}

	resp, err := client.GenerateContent(context.Background(), messages)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	if resp.Content != "Response with system context" {
		t.Errorf("unexpected content: %s", resp.Content)
	}
}
