package geminiapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v2"
	"github.com/nocturnium/llm-go-sdk/v2/internal/httpclient"
)

const (
	testGetWeather = "get_weather"
)

func TestNewClient_Defaults(t *testing.T) {
	client := NewClient(ClientConfig{
		APIKey:          "test-key",
		Model:           "gemini-pro",
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})

	if client.baseURL != defaultBaseURL {
		t.Errorf("expected baseURL=%s, got %s", defaultBaseURL, client.baseURL)
	}
	if client.apiKey != "test-key" {
		t.Errorf("expected apiKey=test-key, got %s", client.apiKey)
	}
	if client.model != "gemini-pro" {
		t.Errorf("expected model=gemini-pro, got %s", client.model)
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

func TestClient_GenerateContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request path contains model and action
		if !strings.Contains(r.URL.Path, "/models/gemini-pro:generateContent") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("x-goog-api-key") != "test-key" {
			t.Errorf("expected x-goog-api-key=test-key, got %s", r.Header.Get("x-goog-api-key"))
		}

		// Parse request
		var req GenerateContentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if len(req.Contents) != 1 {
			t.Errorf("expected 1 content, got %d", len(req.Contents))
		}

		// Send response
		resp := GenerateContentResponse{
			Candidates: []Candidate{
				{
					Content: &Content{
						Role:  "model",
						Parts: []Part{{Text: "Hello!"}},
					},
					FinishReason: "STOP",
				},
			},
			UsageMetadata: &UsageMetadata{
				PromptTokenCount:     10,
				CandidatesTokenCount: 5,
				TotalTokenCount:      15,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:         server.URL,
		APIKey:          "test-key",
		Model:           "gemini-pro",
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})

	resp, err := client.GenerateContent(context.Background(), "", &GenerateContentRequest{
		Contents: []Content{
			{Role: "user", Parts: []Part{{Text: "Hello"}}},
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(resp.Candidates))
	}
	if resp.Candidates[0].Content.Parts[0].Text != "Hello!" {
		t.Errorf("expected text=Hello!, got %s", resp.Candidates[0].Content.Parts[0].Text)
	}
}

func TestClient_GenerateContent_ModelOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the overridden model is used
		if !strings.Contains(r.URL.Path, "/models/gemini-1.5-pro:generateContent") {
			t.Errorf("expected model gemini-1.5-pro in path, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(GenerateContentResponse{
			Candidates: []Candidate{{Content: &Content{Parts: []Part{{Text: "ok"}}}}},
		})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:         server.URL,
		APIKey:          "test-key",
		Model:           "gemini-pro",
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})

	// Override with different model
	_, err := client.GenerateContent(context.Background(), "gemini-1.5-pro", &GenerateContentRequest{
		Contents: []Content{{Role: "user", Parts: []Part{{Text: "test"}}}},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClient_ModelNameSanitizedInURLPaths(t *testing.T) {
	tests := []struct {
		name     string
		call     func(context.Context, *Client) error
		wantPath string
	}{
		{
			name: "generate content",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.GenerateContent(ctx, "../bad/model", &GenerateContentRequest{
					Contents: []Content{{Role: "user", Parts: []Part{{Text: "test"}}}},
				})
				return err
			},
			wantPath: "/models/-bad-model:generateContent",
		},
		{
			name: "stream generate content",
			call: func(ctx context.Context, client *Client) error {
				stream, err := client.GenerateContentStream(ctx, "../bad/model", &GenerateContentRequest{
					Contents: []Content{{Role: "user", Parts: []Part{{Text: "test"}}}},
				})
				if err != nil {
					return err
				}
				return stream.Close()
			},
			wantPath: "/models/-bad-model:streamGenerateContent",
		},
		{
			name: "embed content",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.EmbedContent(ctx, "../bad/model", &EmbedContentRequest{
					Content: EmbeddingContent{Parts: []EmbeddingPart{{Text: "test"}}},
				})
				return err
			},
			wantPath: "/models/-bad-model:embedContent",
		},
		{
			name: "batch embed contents",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.BatchEmbedContents(ctx, "../bad/model", &BatchEmbedContentsRequest{})
				return err
			},
			wantPath: "/models/-bad-model:batchEmbedContents",
		},
		{
			name: "get model",
			call: func(ctx context.Context, client *Client) error {
				_, err := client.GetModel(ctx, "models/../bad/model")
				return err
			},
			wantPath: "/models/-bad-model",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.wantPath {
					t.Errorf("path = %q, want %q", r.URL.Path, tc.wantPath)
				}
				if strings.Contains(r.URL.Path, "..") {
					t.Errorf("path contains traversal segment: %q", r.URL.Path)
				}

				w.Header().Set("Content-Type", "application/json")
				switch tc.name {
				case "embed content":
					_ = json.NewEncoder(w).Encode(EmbedContentResponse{Embedding: EmbeddingValues{Values: []float32{1}}})
				case "batch embed contents":
					_ = json.NewEncoder(w).Encode(BatchEmbedContentsResponse{Embeddings: []EmbeddingValues{{Values: []float32{1}}}})
				case "get model":
					_ = json.NewEncoder(w).Encode(ModelInfo{Name: "models/-bad-model"})
				default:
					_ = json.NewEncoder(w).Encode(GenerateContentResponse{
						Candidates: []Candidate{{Content: &Content{Parts: []Part{{Text: "ok"}}}}},
					})
				}
			}))
			defer server.Close()

			client := NewClient(ClientConfig{
				BaseURL:         server.URL,
				APIKey:          "test-key",
				Model:           "gemini-pro",
				AllowPrivateIPs: true,
				AllowHTTP:       true,
			})

			if err := tc.call(context.Background(), client); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestClient_GenerateContent_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "Invalid API key",
				"code":    400,
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:         server.URL,
		APIKey:          "bad-key",
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})

	_, err := client.GenerateContent(context.Background(), "gemini-pro", &GenerateContentRequest{
		Contents: []Content{{Role: "user", Parts: []Part{{Text: "Hi"}}}},
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestWrapError_UnifiesAPIError verifies that WrapError converts an API error
// into a *llms.APIError discoverable via errors.As, so the SDK's retry,
// circuit-breaker, and fallback logic can classify Gemini errors. This is the
// FIX 4 acceptance test.
func TestWrapError_UnifiesAPIError(t *testing.T) {
	// Use a non-retryable status (400) so the httpclient surfaces a single
	// *httpclient.APIError rather than a "failed after N retries" wrapper.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "invalid request",
				"code":    400,
			},
		})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "k", AllowPrivateIPs: true, AllowHTTP: true})

	_, err := client.GenerateContent(context.Background(), "gemini-pro", &GenerateContentRequest{
		Contents: []Content{{Role: "user", Parts: []Part{{Text: "Hi"}}}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	wrapped := WrapError("generate content", err)

	var apiErr *llms.APIError
	if !errors.As(wrapped, &apiErr) {
		t.Fatalf("expected errors.As to find *llms.APIError, got %T: %v", wrapped, wrapped)
	}
	if apiErr.Provider != llms.ProviderGemini {
		t.Errorf("expected provider %q, got %q", llms.ProviderGemini, apiErr.Provider)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, apiErr.StatusCode)
	}
}

// TestWrapError_NilAndNonAPIError covers WrapError's non-API-error paths.
func TestWrapError_NilAndNonAPIError(t *testing.T) {
	if got := WrapError("op", nil); got != nil {
		t.Errorf("expected nil for nil error, got %v", got)
	}

	plain := errors.New("boom")
	wrapped := WrapError("op", plain)
	if !errors.Is(wrapped, plain) {
		t.Errorf("expected wrapped error to unwrap to the original, got %v", wrapped)
	}
}

func TestClient_GenerateContentStream(t *testing.T) {
	sseData := `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Hello"}]}}]}

data: {"candidates":[{"content":{"role":"model","parts":[{"text":" world"}]}}]}

data: {"candidates":[{"content":{"role":"model","parts":[{"text":"!"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3,"totalTokenCount":13}}

`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify streaming endpoint
		if !strings.Contains(r.URL.Path, ":streamGenerateContent") {
			t.Errorf("expected streamGenerateContent in path, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("alt") != "sse" {
			t.Errorf("expected alt=sse query param")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sseData))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:         server.URL,
		APIKey:          "test-key",
		Model:           "gemini-pro",
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})

	stream, err := client.GenerateContentStream(context.Background(), "", &GenerateContentRequest{
		Contents: []Content{{Role: "user", Parts: []Part{{Text: "Hello"}}}},
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
	if chunk.Candidates[0].Content.Parts[0].Text != "Hello" {
		t.Errorf("expected text=Hello, got %s", chunk.Candidates[0].Content.Parts[0].Text)
	}

	// Read second chunk
	chunk, err = stream.Read()
	if err != nil {
		t.Fatalf("unexpected error reading chunk: %v", err)
	}
	if chunk.Candidates[0].Content.Parts[0].Text != " world" {
		t.Errorf("expected text=' world', got %s", chunk.Candidates[0].Content.Parts[0].Text)
	}

	// Read third chunk with finish reason
	chunk, err = stream.Read()
	if err != nil {
		t.Fatalf("unexpected error reading chunk: %v", err)
	}
	if chunk.Candidates[0].FinishReason != "STOP" {
		t.Errorf("expected finishReason=STOP, got %s", chunk.Candidates[0].FinishReason)
	}
	if chunk.UsageMetadata == nil {
		t.Fatal("expected usageMetadata to be set")
	}
	if chunk.UsageMetadata.TotalTokenCount != 13 {
		t.Errorf("expected totalTokenCount=13, got %d", chunk.UsageMetadata.TotalTokenCount)
	}

	// Next read should return EOF
	_, err = stream.Read()
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestStreamReader_SkipEmptyData(t *testing.T) {
	sseData := `data:

data: {"candidates":[{"content":{"parts":[{"text":"test"}]}}]}

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
	if chunk.Candidates[0].Content.Parts[0].Text != "test" {
		t.Errorf("expected text=test, got %s", chunk.Candidates[0].Content.Parts[0].Text)
	}
}

func TestBuildTextContent(t *testing.T) {
	content := BuildTextContent("user", "Hello world")

	if content.Role != "user" {
		t.Errorf("expected role=user, got %s", content.Role)
	}
	if len(content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(content.Parts))
	}
	if content.Parts[0].Text != "Hello world" {
		t.Errorf("expected text='Hello world', got %s", content.Parts[0].Text)
	}
}

func TestBuildFunctionResponseContent(t *testing.T) {
	response := map[string]any{
		"temperature": 72,
		"condition":   "sunny",
	}
	content := BuildFunctionResponseContent(testGetWeather, response)

	if content.Role != "user" {
		t.Errorf("expected role=user, got %s", content.Role)
	}
	if len(content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(content.Parts))
	}
	if content.Parts[0].FunctionResponse == nil {
		t.Fatal("expected functionResponse to be set")
	}
	if content.Parts[0].FunctionResponse.Name != testGetWeather {
		t.Errorf("expected name=%s, got %s", testGetWeather, content.Parts[0].FunctionResponse.Name)
	}
	if content.Parts[0].FunctionResponse.Response["temperature"] != 72 {
		t.Errorf("unexpected response: %v", content.Parts[0].FunctionResponse.Response)
	}
}

// mockReadCloser wraps a strings.Reader to implement io.ReadCloser
type mockReadCloser struct {
	*strings.Reader
}

func (m *mockReadCloser) Close() error {
	return nil
}
