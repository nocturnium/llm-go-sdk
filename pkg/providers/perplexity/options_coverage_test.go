package perplexity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// TestOptionSetters exercises each WithX option setter against the options
// struct via apply, asserting the field it is expected to mutate.
func TestOptionSetters(t *testing.T) {
	custom := &http.Client{Timeout: 9 * time.Second}

	t.Run("WithEmbeddingModel", func(t *testing.T) {
		opts := apply(WithEmbeddingModel("text-embedding-3-small"))
		if opts.EmbeddingModel != "text-embedding-3-small" {
			t.Errorf("expected embedding model text-embedding-3-small, got %q", opts.EmbeddingModel)
		}
	})

	t.Run("WithHTTPClient", func(t *testing.T) {
		opts := apply(WithHTTPClient(custom))
		if opts.HTTPClient != custom {
			t.Errorf("expected HTTP client to be set to the custom client")
		}
	})

	t.Run("WithTimeout", func(t *testing.T) {
		opts := apply(WithTimeout(42 * time.Second))
		if opts.Timeout != 42*time.Second {
			t.Errorf("expected timeout 42s, got %s", opts.Timeout)
		}
	})

	t.Run("WithBaseURL", func(t *testing.T) {
		opts := apply(WithBaseURL("https://example.test"))
		if opts.BaseURL != "https://example.test" {
			t.Errorf("expected base URL https://example.test, got %q", opts.BaseURL)
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

// TestNew_AllOptions ensures New threads the timeout, HTTP client and embedding
// model options through to a successfully constructed client.
func TestNew_AllOptions(t *testing.T) {
	custom := &http.Client{Timeout: 11 * time.Second}

	client, err := New(
		WithAPIKey("test-key"),
		WithModel("sonar-pro"),
		WithEmbeddingModel("custom-embed"),
		WithHTTPClient(custom),
		WithTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.options.EmbeddingModel != "custom-embed" {
		t.Errorf("expected embedding model custom-embed, got %q", client.options.EmbeddingModel)
	}
	if client.options.HTTPClient != custom {
		t.Error("expected custom HTTP client to be retained on the client")
	}
	if client.options.Timeout != 5*time.Second {
		t.Errorf("expected timeout 5s, got %s", client.options.Timeout)
	}
	if client.Model() != "sonar-pro" {
		t.Errorf("expected model sonar-pro, got %s", client.Model())
	}
}

// modelsHandler returns an httptest server that responds to GET /models with an
// OpenAI-compatible model list.
func modelsServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/models" {
			t.Errorf("expected /models, got %s", r.URL.Path)
		}
		resp := map[string]any{
			"object": "list",
			"data": []map[string]any{
				{
					"id":             "sonar",
					"object":         "model",
					"created":        1677610602,
					"context_length": 127072,
				},
				{
					"id":           "sonar-reasoning-online",
					"object":       "model",
					"display_name": "Sonar Reasoning Online",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestClient_ListModels(t *testing.T) {
	server := modelsServer(t)
	defer server.Close()

	client, err := New(
		WithAPIKey("test-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	result, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(result.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(result.Models))
	}
	if result.HasMore {
		t.Error("expected HasMore to be false")
	}

	first := result.Models[0]
	if first.ID != "sonar" {
		t.Errorf("expected first model id sonar, got %s", first.ID)
	}
	// DisplayName falls back to ID when not provided by the API.
	if first.DisplayName != "sonar" {
		t.Errorf("expected display name to fall back to id, got %s", first.DisplayName)
	}
	if first.Provider != llms.ProviderPerplexity {
		t.Errorf("expected provider perplexity, got %s", first.Provider)
	}
	if first.ContextLength != 127072 {
		t.Errorf("expected context length 127072, got %d", first.ContextLength)
	}
	if first.Organization != "Perplexity AI" {
		t.Errorf("expected organization Perplexity AI, got %s", first.Organization)
	}
	if first.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be populated from created timestamp")
	}
	if len(first.Types) == 0 || first.Types[0] != llms.ModelTypeChat {
		t.Errorf("expected chat model type, got %v", first.Types)
	}

	// Second model supplies an explicit display name.
	if result.Models[1].DisplayName != "Sonar Reasoning Online" {
		t.Errorf("unexpected display name: %s", result.Models[1].DisplayName)
	}
}

func TestClient_ListModels_TypeFilterAndLimit(t *testing.T) {
	server := modelsServer(t)
	defer server.Close()

	client, err := New(
		WithAPIKey("test-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	t.Run("type filter keeps chat models", func(t *testing.T) {
		result, err := client.ListModels(context.Background(), llms.WithModelTypes(llms.ModelTypeChat))
		if err != nil {
			t.Fatalf("ListModels failed: %v", err)
		}
		if len(result.Models) != 2 {
			t.Errorf("expected 2 chat models, got %d", len(result.Models))
		}
	})

	t.Run("limit truncates", func(t *testing.T) {
		result, err := client.ListModels(context.Background(), llms.WithModelLimit(1))
		if err != nil {
			t.Fatalf("ListModels failed: %v", err)
		}
		if len(result.Models) != 1 {
			t.Errorf("expected 1 model after limit, got %d", len(result.Models))
		}
	})
}

func TestClient_ListModels_ContextCanceled(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithBaseURL("https://api.perplexity.ai"),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = client.ListModels(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestClient_ModelInfo(t *testing.T) {
	server := modelsServer(t)
	defer server.Close()

	client, err := New(
		WithAPIKey("test-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	t.Run("found", func(t *testing.T) {
		info, err := client.ModelInfo(context.Background(), "sonar")
		if err != nil {
			t.Fatalf("ModelInfo failed: %v", err)
		}
		if info.ID != "sonar" {
			t.Errorf("expected model sonar, got %s", info.ID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := client.ModelInfo(context.Background(), "does-not-exist")
		if !errors.Is(err, llms.ErrModelNotFound) {
			t.Errorf("expected ErrModelNotFound, got %v", err)
		}
	})
}

// TestRegisterFactory exercises the init-registered factory closure with a
// fully populated Config so every conditional option append is covered.
func TestRegisterFactory(t *testing.T) {
	custom := &http.Client{Timeout: 7 * time.Second}

	llm, err := llms.New("perplexity", llms.Config{
		APIKey:          "cfg-key",
		Model:           "sonar-pro",
		BaseURL:         "https://api.perplexity.ai",
		Timeout:         3 * time.Second,
		HTTPClient:      custom,
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	})
	if err != nil {
		t.Fatalf("llms.New(perplexity) failed: %v", err)
	}

	if llm.Provider() != llms.ProviderPerplexity {
		t.Errorf("expected provider perplexity, got %s", llm.Provider())
	}

	client, ok := llm.(*Client)
	if !ok {
		t.Fatalf("expected *Client, got %T", llm)
	}
	if client.options.Model != "sonar-pro" {
		t.Errorf("expected model sonar-pro, got %s", client.options.Model)
	}
	if client.options.Timeout != 3*time.Second {
		t.Errorf("expected timeout 3s, got %s", client.options.Timeout)
	}
	if client.options.HTTPClient != custom {
		t.Error("expected custom HTTP client from config")
	}
	if !client.options.AllowPrivateIPs || !client.options.AllowHTTP {
		t.Error("expected AllowPrivateIPs and AllowHTTP to be enabled from config")
	}
}
