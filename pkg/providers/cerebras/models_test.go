package cerebras

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/openaicompat"
)

// mockModelsResponse mirrors the OpenAI-compatible /models payload Cerebras returns.
var mockModelsResponse = `{
	"object": "list",
	"data": [
		{
			"id": "llama3.1-70b",
			"object": "model",
			"created": 1733011200,
			"owned_by": "Meta",
			"context_length": 128000
		},
		{
			"id": "llama3.1-8b",
			"object": "model",
			"created": 1733011200,
			"owned_by": "Meta",
			"context_length": 128000
		},
		{
			"id": "qwen-3-32b",
			"object": "model",
			"created": 1733011200,
			"owned_by": "Cerebras"
		}
	]
}`

// setupMockServer wires a Cerebras client to a local httptest server so model
// helpers can be exercised fully offline.
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

func TestWithOptionSetters(t *testing.T) {
	httpClient := &http.Client{}

	opts := apply(
		WithAPIKey("key-123"),
		WithModel("llama3.1-8b"),
		WithBaseURL("https://example.test/v1"),
		WithEmbeddingModel("embed-model"),
		WithHTTPClient(httpClient),
		WithTimeout(42*time.Second),
		WithAllowPrivateIPs(),
		WithAllowHTTP(),
	)

	if opts.APIKey != "key-123" {
		t.Errorf("APIKey = %q, want key-123", opts.APIKey)
	}
	if opts.Model != "llama3.1-8b" {
		t.Errorf("Model = %q, want llama3.1-8b", opts.Model)
	}
	if opts.BaseURL != "https://example.test/v1" {
		t.Errorf("BaseURL = %q, want https://example.test/v1", opts.BaseURL)
	}
	if opts.EmbeddingModel != "embed-model" {
		t.Errorf("EmbeddingModel = %q, want embed-model", opts.EmbeddingModel)
	}
	if opts.HTTPClient != httpClient {
		t.Error("HTTPClient not set to provided client")
	}
	if opts.Timeout != 42*time.Second {
		t.Errorf("Timeout = %v, want 42s", opts.Timeout)
	}
	if !opts.AllowPrivateIPs {
		t.Error("AllowPrivateIPs = false, want true")
	}
	if !opts.AllowHTTP {
		t.Error("AllowHTTP = false, want true")
	}
}

func TestNewAppliesOptions(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithModel("llama3.1-8b"),
		WithBaseURL("https://example.test/v1"),
		WithEmbeddingModel("embed-model"),
		WithHTTPClient(&http.Client{Timeout: 5 * time.Second}),
		WithTimeout(10*time.Second),
		WithAllowPrivateIPs(),
		WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Model() != "llama3.1-8b" {
		t.Errorf("Model() = %q, want llama3.1-8b", client.Model())
	}
	if client.Client() == nil {
		t.Error("expected underlying client to be set")
	}
	if client.options.BaseURL != "https://example.test/v1" {
		t.Errorf("BaseURL = %q, want https://example.test/v1", client.options.BaseURL)
	}
	if client.options.EmbeddingModel != "embed-model" {
		t.Errorf("EmbeddingModel = %q, want embed-model", client.options.EmbeddingModel)
	}
}

func TestListModels(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/models" {
			t.Errorf("expected /models, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-api-key" {
			t.Errorf("expected Bearer test-api-key, got %s", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockModelsResponse))
	})

	result, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(result.Models) != 3 {
		t.Fatalf("expected 3 models, got %d", len(result.Models))
	}
	if result.HasMore {
		t.Error("expected HasMore to be false")
	}

	first := result.Models[0]
	if first.ID != "llama3.1-70b" {
		t.Errorf("expected llama3.1-70b, got %s", first.ID)
	}
	if first.Provider != llms.ProviderCerebras {
		t.Errorf("expected ProviderCerebras, got %s", first.Provider)
	}
	if first.Organization != "Meta" {
		t.Errorf("expected Meta organization, got %s", first.Organization)
	}
	if first.ContextLength != 128000 {
		t.Errorf("expected context length 128000, got %d", first.ContextLength)
	}
	if !first.HasType(llms.ModelTypeChat) {
		t.Error("expected model to have chat type")
	}
}

func TestListModels_WithTypeFilter(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockModelsResponse))
	})

	// All Cerebras models are chat; filtering to chat keeps them all.
	result, err := client.ListModels(context.Background(), llms.WithModelTypes(llms.ModelTypeChat))
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	if len(result.Models) != 3 {
		t.Errorf("expected 3 chat models, got %d", len(result.Models))
	}

	// Filtering to embeddings (none exist) yields zero models.
	result, err = client.ListModels(context.Background(), llms.WithModelTypes(llms.ModelTypeEmbedding))
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	if len(result.Models) != 0 {
		t.Errorf("expected 0 embedding models, got %d", len(result.Models))
	}
}

func TestListModels_WithLimit(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockModelsResponse))
	})

	result, err := client.ListModels(context.Background(), llms.WithModelLimit(2))
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	if len(result.Models) != 2 {
		t.Errorf("expected 2 models (limited), got %d", len(result.Models))
	}
}

func TestListModels_ContextCanceled(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockModelsResponse))
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.ListModels(ctx)
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
}

func TestListModels_APIError(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": {"message": "Invalid API key", "type": "authentication_error"}}`))
	})

	_, err := client.ListModels(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "cerebras") {
		t.Errorf("expected error to mention provider, got: %s", err.Error())
	}
}

func TestModelInfo(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockModelsResponse))
	})

	t.Run("found", func(t *testing.T) {
		info, err := client.ModelInfo(context.Background(), "llama3.1-8b")
		if err != nil {
			t.Fatalf("ModelInfo failed: %v", err)
		}
		if info == nil {
			t.Fatal("expected model info, got nil")
		}
		if info.ID != "llama3.1-8b" {
			t.Errorf("expected llama3.1-8b, got %s", info.ID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		info, err := client.ModelInfo(context.Background(), "nonexistent-model")
		if err == nil {
			t.Fatal("expected error for unknown model, got nil")
		}
		if info != nil {
			t.Errorf("expected nil info, got %+v", info)
		}
	})
}

func TestModelInfo_ListError(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": {"message": "boom"}}`))
	})

	_, err := client.ModelInfo(context.Background(), "llama3.1-70b")
	if err == nil {
		t.Fatal("expected error when ListModels fails, got nil")
	}
}

func TestConvertModelResponse(t *testing.T) {
	t.Run("full model", func(t *testing.T) {
		resp := &openaicompat.ModelResponse{
			ID:            "llama3.1-70b",
			Object:        "model",
			Created:       1733011200,
			DisplayName:   "Llama 3.1 70B",
			ContextLength: 128000,
		}

		info := convertModelResponse(resp)

		if info.ID != "llama3.1-70b" {
			t.Errorf("ID mismatch: %s", info.ID)
		}
		if info.DisplayName != "Llama 3.1 70B" {
			t.Errorf("DisplayName mismatch: %s", info.DisplayName)
		}
		if info.Provider != llms.ProviderCerebras {
			t.Errorf("expected ProviderCerebras, got %s", info.Provider)
		}
		if info.Organization != "Meta" {
			t.Errorf("expected Meta, got %s", info.Organization)
		}
		if info.ContextLength != 128000 {
			t.Errorf("ContextLength mismatch: %d", info.ContextLength)
		}
		if !info.HasType(llms.ModelTypeChat) {
			t.Error("expected chat type")
		}
		if info.FromCache {
			t.Error("expected FromCache false")
		}
		if info.CreatedAt.IsZero() {
			t.Error("expected CreatedAt to be set from Created")
		}
	})

	t.Run("display name falls back to ID", func(t *testing.T) {
		resp := &openaicompat.ModelResponse{
			ID: "qwen-3-32b",
		}

		info := convertModelResponse(resp)

		if info.DisplayName != "qwen-3-32b" {
			t.Errorf("expected display name to fall back to ID, got %s", info.DisplayName)
		}
		if info.Organization != "Cerebras" {
			t.Errorf("expected Cerebras org for non-llama model, got %s", info.Organization)
		}
		if !info.CreatedAt.IsZero() {
			t.Error("expected zero CreatedAt when Created is unset")
		}
	})
}

func TestClientImplementsModelLister(t *testing.T) {
	client := setupMockServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object": "list", "data": []}`))
	})

	var _ llms.ModelLister = client
}
