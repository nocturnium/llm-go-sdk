package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/nocturnium/llm-go-sdk/v4/internal/httpclient"
)

func TestNewClient(t *testing.T) {
	client := NewClient(ClientConfig{
		BaseURL:         "https://api.example.com",
		APIKey:          "test-key",
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})

	if client.baseURL != "https://api.example.com" {
		t.Errorf("expected baseURL=https://api.example.com, got %s", client.baseURL)
	}
	if client.apiKey != "test-key" {
		t.Errorf("expected apiKey=test-key, got %s", client.apiKey)
	}
}

func TestNewClient_WithCustomHTTPClient(t *testing.T) {
	customHTTP := &http.Client{}
	client := NewClient(ClientConfig{
		BaseURL:         "https://api.example.com",
		APIKey:          "test-key",
		HTTPClient:      customHTTP,
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})

	if client.httpClient == nil {
		t.Error("expected HTTP client to be configured")
	}
}

func TestNewClient_WithCustomHeaders(t *testing.T) {
	client := NewClient(ClientConfig{
		BaseURL: "https://api.example.com",
		APIKey:  "test-key",
		Headers: map[string]string{
			"X-Custom-Header": "custom-value",
		},
	})

	if client.headers["X-Custom-Header"] != "custom-value" {
		t.Error("expected custom header to be set")
	}
}

func TestClient_CreateChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected path=/chat/completions, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Error("expected Authorization header with Bearer token")
		}

		// Parse request body
		var req ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Model != "gpt-4" {
			t.Errorf("expected model=gpt-4, got %s", req.Model)
		}

		// Send response
		resp := ChatCompletionResponse{
			ID:    "chatcmpl-123",
			Model: "gpt-4",
			Choices: []Choice{
				{
					Index: 0,
					Message: &ChatMessage{
						Role:         "assistant",
						ContentValue: "Hello!",
					},
					FinishReason: "stop",
				},
			},
			Usage: &Usage{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
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

	resp, err := client.CreateChatCompletion(context.Background(), &ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []ChatMessage{
			{Role: "user", ContentValue: "Hello"},
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.ID != "chatcmpl-123" {
		t.Errorf("expected id=chatcmpl-123, got %s", resp.ID)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.ContentValue != "Hello!" {
		t.Errorf("expected content=Hello!, got %s", resp.Choices[0].Message.ContentValue)
	}
}

func TestClient_CreateChatCompletion_WithCustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "custom-value" {
			t.Errorf("expected X-Custom=custom-value, got %s", r.Header.Get("X-Custom"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ChatCompletionResponse{
			ID:      "chatcmpl-123",
			Choices: []Choice{{Message: &ChatMessage{ContentValue: "ok"}}},
		})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:         server.URL,
		APIKey:          "test-key",
		Headers:         map[string]string{"X-Custom": "custom-value"},
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})

	_, err := client.CreateChatCompletion(context.Background(), &ChatCompletionRequest{
		Model:    "gpt-4",
		Messages: []ChatMessage{{Role: "user", ContentValue: "test"}},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_CreateChatCompletionStream(t *testing.T) {
	sseData := `data: {"id":"chatcmpl-123","choices":[{"delta":{"content":"Hello"}}]}

data: {"id":"chatcmpl-123","choices":[{"delta":{"content":" world"}}]}

data: {"id":"chatcmpl-123","choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ChatCompletionRequest
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

	stream, err := client.CreateChatCompletionStream(context.Background(), &ChatCompletionRequest{
		Model:    "gpt-4",
		Messages: []ChatMessage{{Role: "user", ContentValue: "Hello"}},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = stream.Close() }()

	// Read first chunk
	chunk, err := stream.Read()
	if err != nil {
		t.Fatalf("unexpected error reading chunk: %v", err)
	}
	if chunk.Choices[0].Delta.ContentValue != "Hello" {
		t.Errorf("expected content=Hello, got %s", chunk.Choices[0].Delta.ContentValue)
	}

	// Read second chunk
	chunk, err = stream.Read()
	if err != nil {
		t.Fatalf("unexpected error reading chunk: %v", err)
	}
	if chunk.Choices[0].Delta.ContentValue != " world" {
		t.Errorf("expected content=' world', got %s", chunk.Choices[0].Delta.ContentValue)
	}

	// Read third chunk
	chunk, err = stream.Read()
	if err != nil {
		t.Fatalf("unexpected error reading chunk: %v", err)
	}
	if chunk.Choices[0].FinishReason != "stop" {
		t.Errorf("expected finish_reason=stop, got %s", chunk.Choices[0].FinishReason)
	}

	// Read [DONE] - should return EOF
	_, err = stream.Read()
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestClient_CreateChatCompletion_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "Invalid model",
				"type":    "invalid_request_error",
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:         server.URL,
		APIKey:          "test-key",
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})

	_, err := client.CreateChatCompletion(context.Background(), &ChatCompletionRequest{
		Model:    "invalid-model",
		Messages: []ChatMessage{{Role: "user", ContentValue: "Hello"}},
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !strings.Contains(err.Error(), "Invalid model") {
		t.Errorf("expected error to contain 'Invalid model', got %s", err.Error())
	}
}

func TestClient_ListModels_UsesCustomHTTPClient(t *testing.T) {
	var sawRequest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest = true
		if r.URL.Path != "/v1/models" {
			t.Errorf("expected path=/v1/models, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("expected Authorization header, got %q", auth)
		}
		_ = json.NewEncoder(w).Encode(ModelsListResponse{
			Object: "list",
			Data:   []ModelResponse{{ID: "test-model"}},
		})
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}

	customHTTP := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = serverURL.Scheme
			req.URL.Host = serverURL.Host
			return http.DefaultTransport.RoundTrip(req)
		}),
	}
	client := NewClient(ClientConfig{
		BaseURL:         "https://api.example.invalid/v1",
		APIKey:          "test-key",
		HTTPClient:      customHTTP,
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})

	resp, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if !sawRequest {
		t.Fatal("custom HTTP transport was not used")
	}
	if len(resp.Data) != 1 || resp.Data[0].ID != "test-model" {
		t.Fatalf("unexpected models response: %+v", resp.Data)
	}
}

func TestClient_ListModelsWithQuery_PreservesAzureAPIVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if got := query.Get("api-version"); got != "2024-02-15-preview" {
			t.Errorf("api-version=%q, want 2024-02-15-preview", got)
		}
		if got := query.Get("type"); got != "chat model" {
			t.Errorf("type=%q, want chat model", got)
		}
		_ = json.NewEncoder(w).Encode(ModelsListResponse{
			Object: "list",
			Data:   []ModelResponse{{ID: "test-model"}},
		})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:         server.URL + "/openai/deployments/deployment",
		APIKey:          "test-key",
		AzureAPIKey:     true,
		AzureVersion:    "2024-02-15-preview",
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})

	if _, err := client.ListModelsWithQuery(context.Background(), map[string]string{"type": "chat model"}); err != nil {
		t.Fatalf("ListModelsWithQuery() error = %v", err)
	}
}

func TestClient_ListModels_RawErrorMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "invalid model",
				"type":    "invalid_request_error",
				"code":    "model_not_found",
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "test-key", AllowPrivateIPs: true, AllowHTTP: true})
	_, err := client.ListModels(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *httpclient.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected httpclient.APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusBadRequest || apiErr.Code != "model_not_found" {
		t.Fatalf("unexpected API error: %+v", apiErr)
	}
}

func TestStreamReader_SkipEmptyData(t *testing.T) {
	sseData := `data:

data: {"id":"chatcmpl-123","choices":[{"delta":{"content":"test"}}]}

data: [DONE]

`
	reader := &mockReadCloser{strings.NewReader(sseData)}
	stream := &StreamReader{
		sseReader: httpclient.NewSSEReader(reader),
	}
	defer func() { _ = stream.Close() }()

	// Should skip empty data and get first real chunk
	chunk, err := stream.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chunk.Choices[0].Delta.ContentValue != "test" {
		t.Errorf("expected content=test, got %s", chunk.Choices[0].Delta.ContentValue)
	}
}

// mockReadCloser wraps a strings.Reader to implement io.ReadCloser
type mockReadCloser struct {
	*strings.Reader
}

func (m *mockReadCloser) Close() error {
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestParseModelsResponse_OpenAIFormat(t *testing.T) {
	// Standard OpenAI format with wrapper object
	data := []byte(`{
		"object": "list",
		"data": [
			{"id": "gpt-4", "object": "model", "created": 1687882411, "owned_by": "openai"},
			{"id": "gpt-3.5-turbo", "object": "model", "created": 1677610602, "owned_by": "openai"}
		]
	}`)

	client := &Client{}
	resp, err := client.parseModelsResponse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Object != "list" {
		t.Errorf("expected object=list, got %s", resp.Object)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 models, got %d", len(resp.Data))
	}
	if resp.Data[0].ID != "gpt-4" {
		t.Errorf("expected first model id=gpt-4, got %s", resp.Data[0].ID)
	}
}

func TestParseModelsResponse_RawArrayFormat(t *testing.T) {
	// Raw array format returned by TogetherAI and some other providers
	data := []byte(`[
		{"id": "meta-llama/Llama-3.3-70B", "object": "model", "type": "chat", "context_length": 131072},
		{"id": "mistralai/Mixtral-8x7B", "object": "model", "type": "chat", "context_length": 32768}
	]`)

	client := &Client{}
	resp, err := client.parseModelsResponse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should normalize to standard format
	if resp.Object != "list" {
		t.Errorf("expected object=list, got %s", resp.Object)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 models, got %d", len(resp.Data))
	}
	if resp.Data[0].ID != "meta-llama/Llama-3.3-70B" {
		t.Errorf("expected first model id=meta-llama/Llama-3.3-70B, got %s", resp.Data[0].ID)
	}
	if resp.Data[0].ContextLength != 131072 {
		t.Errorf("expected context_length=131072, got %d", resp.Data[0].ContextLength)
	}
}

func TestParseModelsResponse_EmptyResponse(t *testing.T) {
	client := &Client{}
	_, err := client.parseModelsResponse([]byte(""))
	if err == nil {
		t.Error("expected error for empty response")
	}
}

func TestParseModelsResponse_WhitespaceArray(t *testing.T) {
	// Array with leading/trailing whitespace
	data := []byte(`  [{"id": "test-model"}]  `)

	client := &Client{}
	resp, err := client.parseModelsResponse(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 model, got %d", len(resp.Data))
	}
	if resp.Data[0].ID != "test-model" {
		t.Errorf("expected id=test-model, got %s", resp.Data[0].ID)
	}
}
