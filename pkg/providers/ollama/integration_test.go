//go:build integration

package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/internal/ollamaapi"
)

// TestGenerateContent tests basic content generation.
func TestGenerateContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			resp := map[string]any{
				"id":      "test-123",
				"object":  "chat.completion",
				"created": 1234567890,
				"model":   "llama3.2",
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role":    "assistant",
							"content": "Hello! I'm Llama, your AI assistant.",
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
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Hello!"},
	}

	resp, err := client.GenerateContent(ctx, messages)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	if resp.Content == "" {
		t.Error("expected non-empty content")
	}

	if resp.Usage.TotalTokens != 18 {
		t.Errorf("expected 18 total tokens, got %d", resp.Usage.TotalTokens)
	}
}

// TestCall tests the simple Call interface.
func TestCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			resp := map[string]any{
				"id":      "test-123",
				"object":  "chat.completion",
				"created": 1234567890,
				"model":   "llama3.2",
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role":    "assistant",
							"content": "42",
						},
						"finish_reason": "stop",
					},
				},
				"usage": map[string]any{
					"prompt_tokens":     5,
					"completion_tokens": 1,
					"total_tokens":      6,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	resp, err := client.Call(ctx, "What is 6 times 7?")
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if resp != "42" {
		t.Errorf("expected '42', got %q", resp)
	}
}

// TestStream tests streaming responses.
func TestStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			chunks := []string{
				`{"id":"1","object":"chat.completion.chunk","model":"llama3.2","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}`,
				`{"id":"1","object":"chat.completion.chunk","model":"llama3.2","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`,
				`{"id":"1","object":"chat.completion.chunk","model":"llama3.2","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":"stop"}]}`,
			}

			for _, chunk := range chunks {
				w.Write([]byte("data: " + chunk + "\n\n"))
				w.(http.Flusher).Flush()
			}
			w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Say hello"},
	}

	stream, err := client.Stream(ctx, messages)
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var content strings.Builder
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		content.WriteString(chunk.Content)
	}

	if content.String() != "Hello world!" {
		t.Errorf("expected 'Hello world!', got %q", content.String())
	}
}

// TestWithTools tests function calling.
func TestWithTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			var req map[string]any
			json.NewDecoder(r.Body).Decode(&req)

			// Verify tools were included
			tools, ok := req["tools"].([]any)
			if !ok || len(tools) == 0 {
				t.Error("expected tools in request")
			}

			resp := map[string]any{
				"id":      "test-123",
				"object":  "chat.completion",
				"created": 1234567890,
				"model":   "llama3.2",
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
										"arguments": `{"location": "Tokyo"}`,
									},
								},
							},
						},
						"finish_reason": "tool_calls",
					},
				},
				"usage": map[string]any{
					"prompt_tokens":     20,
					"completion_tokens": 15,
					"total_tokens":      35,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "What's the weather in Tokyo?"},
	}

	tools := []llms.Tool{
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "get_weather",
				Description: "Get the current weather",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`),
			},
		},
	}

	resp, err := client.GenerateContent(ctx, messages, llms.WithTools(tools))
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	if len(resp.ToolCalls) == 0 {
		t.Error("expected tool calls in response")
	}

	if resp.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("expected tool call to get_weather, got %s", resp.ToolCalls[0].Function.Name)
	}
}

// TestJSONMode tests JSON output mode.
func TestJSONMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			var req map[string]any
			json.NewDecoder(r.Body).Decode(&req)

			// Verify response_format was included
			if _, ok := req["response_format"]; !ok {
				t.Error("expected response_format in request")
			}

			resp := map[string]any{
				"id":      "test-123",
				"object":  "chat.completion",
				"created": 1234567890,
				"model":   "llama3.2",
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role":    "assistant",
							"content": `{"answer": 42}`,
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
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Return JSON with the answer to 6*7"},
	}

	resp, err := client.GenerateContent(ctx, messages, llms.WithJSONMode())
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	// Verify it's valid JSON
	var result map[string]any
	if err := json.Unmarshal([]byte(resp.Content), &result); err != nil {
		t.Errorf("response is not valid JSON: %v", err)
	}
}

// TestEmbed tests embedding generation.
func TestEmbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" {
			var req map[string]any
			json.NewDecoder(r.Body).Decode(&req)

			resp := map[string]any{
				"object": "list",
				"model":  "nomic-embed-text",
				"data": []map[string]any{
					{
						"object":    "embedding",
						"index":     0,
						"embedding": []float32{0.1, 0.2, 0.3, 0.4, 0.5},
					},
				},
				"usage": map[string]any{
					"prompt_tokens": 5,
					"total_tokens":  5,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	resp, err := client.Embed(ctx, []string{"Hello world"})
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

// TestEmbedQuery tests single query embedding.
func TestEmbedQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" {
			resp := map[string]any{
				"object": "list",
				"model":  "nomic-embed-text",
				"data": []map[string]any{
					{
						"object":    "embedding",
						"index":     0,
						"embedding": []float32{0.1, 0.2, 0.3},
					},
				},
				"usage": map[string]any{
					"prompt_tokens": 3,
					"total_tokens":  3,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	vector, err := llms.EmbedQuery(ctx, client, "Test query")
	if err != nil {
		t.Fatalf("EmbedQuery failed: %v", err)
	}

	if len(vector) != 3 {
		t.Errorf("expected 3-dimensional vector, got %d", len(vector))
	}
}

// TestListModels tests model listing via OpenAI-compatible endpoint.
func TestListModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			resp := map[string]any{
				"object": "list",
				"data": []map[string]any{
					{
						"id":       "llama3.2",
						"object":   "model",
						"created":  1234567890,
						"owned_by": "meta",
					},
					{
						"id":       "mistral",
						"object":   "model",
						"created":  1234567891,
						"owned_by": "mistral",
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	result, err := client.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(result.Models) != 2 {
		t.Errorf("expected 2 models, got %d", len(result.Models))
	}
}

// TestListLocalModels tests the native API model listing.
func TestListLocalModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			resp := ollamaapi.ListModelsResponse{
				Models: []ollamaapi.Model{
					{
						Name:       "llama3.2:latest",
						Model:      "llama3.2:latest",
						ModifiedAt: time.Now(),
						Size:       4000000000,
						Digest:     "sha256:abc123",
						Details: &ollamaapi.ModelDetails{
							Format:            "gguf",
							Family:            "llama",
							ParameterSize:     "8B",
							QuantizationLevel: "Q4_0",
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	models, err := client.ListLocalModels(ctx)
	if err != nil {
		t.Fatalf("ListLocalModels failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}

	if models[0].Name != "llama3.2:latest" {
		t.Errorf("expected model name 'llama3.2:latest', got %q", models[0].Name)
	}

	if models[0].Details.ParameterSize != "8B" {
		t.Errorf("expected parameter size '8B', got %q", models[0].Details.ParameterSize)
	}
}

// TestShowModel tests retrieving model details.
func TestShowModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			var req ollamaapi.ShowRequest
			json.NewDecoder(r.Body).Decode(&req)

			resp := ollamaapi.ShowResponse{
				License:    "Meta Llama License",
				Modelfile:  "FROM llama3.2",
				Parameters: "temperature 0.8",
				Template:   "{{ .System }}\n{{ .Prompt }}",
				Details: &ollamaapi.ModelDetails{
					Format:            "gguf",
					Family:            "llama",
					Families:          []string{"llama"},
					ParameterSize:     "8B",
					QuantizationLevel: "Q4_0",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	details, err := client.ShowModel(ctx, "llama3.2")
	if err != nil {
		t.Fatalf("ShowModel failed: %v", err)
	}

	if details.Family != "llama" {
		t.Errorf("expected family 'llama', got %q", details.Family)
	}

	if details.ParameterSize != "8B" {
		t.Errorf("expected parameter size '8B', got %q", details.ParameterSize)
	}

	if !strings.Contains(details.License, "Meta") {
		t.Errorf("expected license to contain 'Meta', got %q", details.License)
	}
}

// TestListRunningModels tests listing currently loaded models.
func TestListRunningModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ps" {
			resp := ollamaapi.ListRunningResponse{
				Models: []ollamaapi.RunningModel{
					{
						Name:      "llama3.2:latest",
						Model:     "llama3.2:latest",
						Size:      4000000000,
						Digest:    "sha256:abc123",
						ExpiresAt: time.Now().Add(5 * time.Minute),
						SizeVRAM:  3000000000,
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	models, err := client.ListRunningModels(ctx)
	if err != nil {
		t.Fatalf("ListRunningModels failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("expected 1 running model, got %d", len(models))
	}

	if models[0].Name != "llama3.2:latest" {
		t.Errorf("expected model name 'llama3.2:latest', got %q", models[0].Name)
	}

	if models[0].SizeVRAM != 3000000000 {
		t.Errorf("expected VRAM size 3000000000, got %d", models[0].SizeVRAM)
	}
}

// TestVersion tests retrieving Ollama version.
func TestVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			resp := ollamaapi.VersionResponse{
				Version: "0.5.4",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	version, err := client.Version(ctx)
	if err != nil {
		t.Fatalf("Version failed: %v", err)
	}

	if version != "0.5.4" {
		t.Errorf("expected version '0.5.4', got %q", version)
	}
}

// TestPullModel tests model pulling with progress.
func TestPullModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/pull" {
			var req ollamaapi.PullRequest
			json.NewDecoder(r.Body).Decode(&req)

			// Send streaming progress
			w.Header().Set("Content-Type", "application/x-ndjson")

			responses := []ollamaapi.PullResponse{
				{Status: "pulling manifest"},
				{Status: "downloading", Digest: "sha256:abc", Total: 1000, Completed: 500},
				{Status: "downloading", Digest: "sha256:abc", Total: 1000, Completed: 1000},
				{Status: "success"},
			}

			for _, resp := range responses {
				data, _ := json.Marshal(resp)
				w.Write(data)
				w.Write([]byte("\n"))
				w.(http.Flusher).Flush()
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	var progressUpdates []PullProgress

	err = client.PullModel(ctx, "llama3.2", func(p PullProgress) {
		progressUpdates = append(progressUpdates, p)
	})
	if err != nil {
		t.Fatalf("PullModel failed: %v", err)
	}

	if len(progressUpdates) < 2 {
		t.Errorf("expected at least 2 progress updates, got %d", len(progressUpdates))
	}

	// Check for success status
	found := false
	for _, p := range progressUpdates {
		if p.Status == "success" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected success status in progress updates")
	}
}

// TestDeleteModel tests model deletion.
func TestDeleteModel(t *testing.T) {
	deleted := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/delete" && r.Method == http.MethodDelete {
			var req ollamaapi.DeleteRequest
			json.NewDecoder(r.Body).Decode(&req)

			if req.Name == "test-model" {
				deleted = true
				w.WriteHeader(http.StatusOK)
				return
			}
			http.Error(w, "model not found", http.StatusNotFound)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	err = client.DeleteModel(ctx, "test-model")
	if err != nil {
		t.Fatalf("DeleteModel failed: %v", err)
	}

	if !deleted {
		t.Error("expected model to be deleted")
	}
}

// TestCopyModel tests model copying.
func TestCopyModel(t *testing.T) {
	copied := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/copy" && r.Method == http.MethodPost {
			var req ollamaapi.CopyRequest
			json.NewDecoder(r.Body).Decode(&req)

			if req.Source == "llama3.2" && req.Destination == "my-llama" {
				copied = true
				w.WriteHeader(http.StatusOK)
				return
			}
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	err = client.CopyModel(ctx, "llama3.2", "my-llama")
	if err != nil {
		t.Fatalf("CopyModel failed: %v", err)
	}

	if !copied {
		t.Error("expected model to be copied")
	}
}

// TestContextCancellation tests that operations respect context cancellation.
func TestContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = client.Call(ctx, "Hello")
	if err == nil {
		t.Error("expected error due to context cancellation")
	}
}

// TestErrorHandling tests error responses.
func TestErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "Invalid model specified",
				"type":    "invalid_request_error",
			},
		})
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	_, err = client.Call(ctx, "Hello")
	if err == nil {
		t.Error("expected error for bad request")
	}
}

// TestGetCapabilities tests capability reporting.
func TestGetCapabilities_Integration(t *testing.T) {
	client, err := New()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	caps := client.Capabilities()

	if !caps.Streaming {
		t.Error("expected streaming capability")
	}

	if caps.Tools {
		t.Error("expected tools capability to be unknown for default local model")
	}

	if !caps.Embeddings {
		t.Error("expected embeddings capability")
	}

	if !caps.JSONMode {
		t.Error("expected JSON mode capability")
	}
}

// TestGetProvider tests provider identification.
func TestGetProvider(t *testing.T) {
	client, err := New()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	if client.Provider() != llms.ProviderOllama {
		t.Errorf("expected provider ollama, got %s", client.Provider())
	}
}

// TestGetModel tests model name retrieval.
func TestGetModel(t *testing.T) {
	client, err := New(WithModel("mistral"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	if client.Model() != "mistral" {
		t.Errorf("expected model 'mistral', got %q", client.Model())
	}
}

// TestSystemMessage tests system message handling.
func TestSystemMessage(t *testing.T) {
	var receivedMessages []any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			var req map[string]any
			json.NewDecoder(r.Body).Decode(&req)
			receivedMessages, _ = req["messages"].([]any)

			resp := map[string]any{
				"id":      "test-123",
				"object":  "chat.completion",
				"created": 1234567890,
				"model":   "llama3.2",
				"choices": []map[string]any{
					{
						"index": 0,
						"message": map[string]any{
							"role":    "assistant",
							"content": "I am a pirate assistant!",
						},
						"finish_reason": "stop",
					},
				},
				"usage": map[string]any{
					"prompt_tokens":     15,
					"completion_tokens": 6,
					"total_tokens":      21,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client, err := New(WithBaseURL(server.URL + "/v1"))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	messages := []llms.Message{
		{Role: llms.RoleSystem, Content: "You are a pirate assistant."},
		{Role: llms.RoleUser, Content: "Hello!"},
	}

	_, err = client.GenerateContent(ctx, messages)
	if err != nil {
		t.Fatalf("GenerateContent failed: %v", err)
	}

	// Verify system message was included
	if len(receivedMessages) < 2 {
		t.Error("expected at least 2 messages including system")
	}
}
