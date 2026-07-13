package mistral

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v5"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/openaicompat"
)

// mockModelsResponse mirrors the Mistral /models response (OpenAI-compatible).
var mockModelsResponse = `{
	"object": "list",
	"data": [
		{
			"id": "mistral-large-latest",
			"object": "model",
			"created": 1733011200,
			"display_name": "Mistral Large",
			"context_length": 131072
		},
		{
			"id": "codestral-latest",
			"object": "model",
			"created": 1702339200,
			"context_length": 32768
		},
		{
			"id": "pixtral-12b-2409",
			"object": "model",
			"created": 1709251200,
			"display_name": "Pixtral 12B"
		},
		{
			"id": "mistral-embed",
			"object": "model",
			"created": 1698883200
		}
	]
}`

func setupMockClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := New(
		WithAPIKey("test-api-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(),
		WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	return client
}

// TestWithOptions_Coverage exercises the construction options that set fields
// on the options struct directly (no network needed).
func TestWithOptions_Coverage(t *testing.T) {
	t.Run("WithBaseURL", func(t *testing.T) {
		opts := apply(WithBaseURL("https://example.test/v1"))
		if opts.BaseURL != "https://example.test/v1" {
			t.Errorf("expected base URL set, got %s", opts.BaseURL)
		}
	})

	t.Run("WithHTTPClient", func(t *testing.T) {
		hc := &http.Client{Timeout: 5 * time.Second}
		opts := apply(WithHTTPClient(hc))
		if opts.HTTPClient != hc {
			t.Error("expected custom HTTP client to be set")
		}
	})

	t.Run("WithTimeout", func(t *testing.T) {
		opts := apply(WithTimeout(42 * time.Second))
		if opts.Timeout != 42*time.Second {
			t.Errorf("expected timeout 42s, got %s", opts.Timeout)
		}
	})

	t.Run("WithAllowPrivateIPs", func(t *testing.T) {
		opts := apply(WithAllowPrivateIPs())
		if !opts.AllowPrivateIPs {
			t.Error("expected AllowPrivateIPs to be true")
		}
	})

	t.Run("WithAllowHTTP", func(t *testing.T) {
		opts := apply(WithAllowHTTP())
		if !opts.AllowHTTP {
			t.Error("expected AllowHTTP to be true")
		}
	})
}

// TestNewWithAllOptions verifies New threads every option through without error.
func TestNewWithAllOptions(t *testing.T) {
	hc := &http.Client{Timeout: 3 * time.Second}
	client, err := New(
		WithAPIKey("test-key"),
		WithModel("codestral-latest"),
		WithEmbeddingModel("mistral-embed"),
		WithBaseURL("https://api.mistral.ai/v1"),
		WithHTTPClient(hc),
		WithTimeout(10*time.Second),
		WithAllowPrivateIPs(),
		WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.options.Model != "codestral-latest" {
		t.Errorf("expected model codestral-latest, got %s", client.options.Model)
	}
	if client.options.HTTPClient != hc {
		t.Error("expected custom HTTP client to be threaded into options")
	}
	if client.options.Timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %s", client.options.Timeout)
	}
	if !client.options.AllowPrivateIPs {
		t.Error("expected AllowPrivateIPs true")
	}
	if !client.options.AllowHTTP {
		t.Error("expected AllowHTTP true")
	}
}

func TestListModels(t *testing.T) {
	client := setupMockClient(t, func(w http.ResponseWriter, r *http.Request) {
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

	if len(result.Models) != 4 {
		t.Fatalf("expected 4 models, got %d", len(result.Models))
	}
	if result.HasMore {
		t.Error("expected HasMore to be false")
	}

	large := result.Models[0]
	if large.ID != "mistral-large-latest" {
		t.Errorf("expected mistral-large-latest, got %s", large.ID)
	}
	if large.DisplayName != "Mistral Large" {
		t.Errorf("expected display name 'Mistral Large', got %s", large.DisplayName)
	}
	if large.Provider != llms.ProviderMistral {
		t.Errorf("expected ProviderMistral, got %s", large.Provider)
	}
	if large.Organization != "Mistral AI" {
		t.Errorf("expected organization 'Mistral AI', got %s", large.Organization)
	}
	if large.ContextLength != 131072 {
		t.Errorf("expected context length 131072, got %d", large.ContextLength)
	}
	if !large.HasType(llms.ModelTypeChat) {
		t.Error("expected large model to have chat type")
	}

	// codestral-latest has no display_name -> falls back to ID, and is code-typed.
	codestral := result.Models[1]
	if codestral.DisplayName != "codestral-latest" {
		t.Errorf("expected display name to fall back to ID, got %s", codestral.DisplayName)
	}
	if !codestral.HasType(llms.ModelTypeCode) {
		t.Error("expected codestral to have code type")
	}

	// mistral-embed is an embedding model.
	embed := result.Models[3]
	if !embed.HasType(llms.ModelTypeEmbedding) {
		t.Error("expected mistral-embed to have embedding type")
	}
}

func TestListModels_WithTypeFilter(t *testing.T) {
	client := setupMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockModelsResponse))
	})

	result, err := client.ListModels(context.Background(), llms.WithModelTypes(llms.ModelTypeEmbedding))
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	if len(result.Models) != 1 {
		t.Fatalf("expected 1 embedding model, got %d", len(result.Models))
	}
	if result.Models[0].ID != "mistral-embed" {
		t.Errorf("expected mistral-embed, got %s", result.Models[0].ID)
	}
}

func TestListModels_WithLimit(t *testing.T) {
	client := setupMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
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

func TestListModels_ContextCancelled(t *testing.T) {
	client := setupMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockModelsResponse))
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.ListModels(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestListModels_APIError(t *testing.T) {
	client := setupMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": {"message": "Invalid API key", "type": "authentication_error"}}`))
	})

	_, err := client.ListModels(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "mistral") {
		t.Errorf("expected error to contain provider name, got: %s", err.Error())
	}
}

func TestModelInfo(t *testing.T) {
	client := setupMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockModelsResponse))
	})

	t.Run("found", func(t *testing.T) {
		info, err := client.ModelInfo(context.Background(), "codestral-latest")
		if err != nil {
			t.Fatalf("ModelInfo failed: %v", err)
		}
		if info == nil {
			t.Fatal("expected model info, got nil")
		}
		if info.ID != "codestral-latest" {
			t.Errorf("expected codestral-latest, got %s", info.ID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		info, err := client.ModelInfo(context.Background(), "does-not-exist")
		if !errors.Is(err, llms.ErrModelNotFound) {
			t.Fatalf("ModelInfo error = %v, want %v", err, llms.ErrModelNotFound)
		}
		if info != nil {
			t.Errorf("expected nil for missing model, got %+v", info)
		}
	})

	t.Run("list error propagates", func(t *testing.T) {
		errClient := setupMockClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error": {"message": "boom"}}`))
		})
		if _, err := errClient.ModelInfo(context.Background(), "anything"); err == nil {
			t.Fatal("expected error from underlying ListModels, got nil")
		}
	})
}

func TestConvertModelResponse(t *testing.T) {
	t.Run("full model", func(t *testing.T) {
		resp := &openaicompat.ModelResponse{
			ID:            "pixtral-12b-2409",
			Object:        "model",
			Created:       1709251200,
			DisplayName:   "Pixtral 12B",
			ContextLength: 128000,
		}

		info := convertModelResponse(resp)

		if info.ID != resp.ID {
			t.Errorf("ID mismatch: %s != %s", info.ID, resp.ID)
		}
		if info.DisplayName != "Pixtral 12B" {
			t.Errorf("DisplayName mismatch: %s", info.DisplayName)
		}
		if info.Provider != llms.ProviderMistral {
			t.Errorf("expected ProviderMistral, got %s", info.Provider)
		}
		if info.Organization != "Mistral AI" {
			t.Errorf("expected organization 'Mistral AI', got %s", info.Organization)
		}
		if info.ContextLength != 128000 {
			t.Errorf("expected context length 128000, got %d", info.ContextLength)
		}
		if info.FromCache {
			t.Error("expected FromCache to be false")
		}
		if info.CreatedAt.IsZero() {
			t.Error("expected CreatedAt to be set from Created")
		}
		if !info.HasType(llms.ModelTypeVision) {
			t.Error("expected pixtral to be vision-typed")
		}
	})

	t.Run("missing display name falls back to ID", func(t *testing.T) {
		resp := &openaicompat.ModelResponse{
			ID: "mistral-small-latest",
		}
		info := convertModelResponse(resp)
		if info.DisplayName != "mistral-small-latest" {
			t.Errorf("expected display name to fall back to ID, got %s", info.DisplayName)
		}
		// Created == 0 -> CreatedAt should remain zero.
		if !info.CreatedAt.IsZero() {
			t.Error("expected CreatedAt to be zero when Created is unset")
		}
	})
}

// TestRegisteredFactory exercises the init() registration path by constructing
// a client through the global provider registry.
func TestRegisteredFactory(t *testing.T) {
	client, err := llms.New("mistral", llms.Config{
		APIKey:          "test-key",
		Model:           "mistral-medium-latest",
		BaseURL:         "https://api.mistral.ai/v1",
		Timeout:         7 * time.Second,
		HTTPClient:      &http.Client{},
		AllowPrivateIPs: true,
	})
	if err != nil {
		t.Fatalf("llms.New(mistral) failed: %v", err)
	}
	if client.Provider() != llms.ProviderMistral {
		t.Errorf("expected provider mistral, got %s", client.Provider())
	}
}
