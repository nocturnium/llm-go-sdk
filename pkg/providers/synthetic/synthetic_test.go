package synthetic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

const (
	testQwenModel = "hf:Qwen/Qwen3-Coder-480B-A35B-Instruct"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.Model != testQwenModel {
		t.Errorf("expected default model to be hf:Qwen/Qwen3-Coder-480B-A35B-Instruct, got %s", opts.Model)
	}
	if opts.BaseURL != "https://api.synthetic.new/openai/v1" {
		t.Errorf("expected default BaseURL to be https://api.synthetic.new/openai/v1, got %s", opts.BaseURL)
	}
	if opts.APIKey != "" {
		t.Error("expected default API key to be empty")
	}
}

func TestApplyOptions(t *testing.T) {
	opts := apply(
		WithAPIKey("test-key"),
		WithModel("hf:THUDM/GLM-4.5-9B-0414-Chat"),
		WithBaseURL("https://custom.api.com"),
	)

	if opts.APIKey != "test-key" {
		t.Errorf("expected API key to be test-key, got %s", opts.APIKey)
	}
	if opts.Model != "hf:THUDM/GLM-4.5-9B-0414-Chat" {
		t.Errorf("expected model to be hf:THUDM/GLM-4.5-9B-0414-Chat, got %s", opts.Model)
	}
	if opts.BaseURL != "https://custom.api.com" {
		t.Errorf("expected BaseURL to be https://custom.api.com, got %s", opts.BaseURL)
	}
}

func TestNewClientMissingAPIKey(t *testing.T) {
	// Ensure env var is not set
	originalKey := os.Getenv("SYNTHETIC_API_KEY")
	t.Setenv("SYNTHETIC_API_KEY", "")
	t.Setenv("LLM_API_KEY", "")
	defer func() {
		if originalKey != "" {
			t.Setenv("SYNTHETIC_API_KEY", originalKey)
		}
	}()

	_, err := New()
	if !errors.Is(err, llms.ErrMissingAPIKey) {
		t.Errorf("expected ErrMissingAPIKey, got %v", err)
	}
}

func TestNewClientWithEnvAPIKey(t *testing.T) {
	// Set env var
	originalKey := os.Getenv("SYNTHETIC_API_KEY")
	t.Setenv("SYNTHETIC_API_KEY", "env-test-key")
	defer func() {
		if originalKey != "" {
			t.Setenv("SYNTHETIC_API_KEY", originalKey)
		} else {
			t.Setenv("SYNTHETIC_API_KEY", "")
		}
	}()

	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderSynthetic {
		t.Errorf("expected provider to be synthetic, got %s", client.Provider())
	}
	if client.Model() != testQwenModel {
		t.Errorf("expected default model, got %s", client.Model())
	}
}

func TestNewClientWithExplicitAPIKey(t *testing.T) {
	client, err := New(WithAPIKey("explicit-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderSynthetic {
		t.Errorf("expected provider to be synthetic, got %s", client.Provider())
	}
}

func TestNewClientWithCustomModel(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithModel("hf:deepseek-ai/DeepSeek-V3-0324"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Model() != "hf:deepseek-ai/DeepSeek-V3-0324" {
		t.Errorf("expected model to be hf:deepseek-ai/DeepSeek-V3-0324, got %s", client.Model())
	}
}

func TestNewClientPreservesBaseURL(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.options.BaseURL != "https://api.synthetic.new/openai/v1" {
		t.Errorf("expected BaseURL to be https://api.synthetic.new/openai/v1, got %s", client.options.BaseURL)
	}
}

func TestNewClientWithLLMAPIKeyFallback(t *testing.T) {
	// Ensure provider-specific env var is not set but LLM_API_KEY is
	originalSynthetic := os.Getenv("SYNTHETIC_API_KEY")
	originalLLM := os.Getenv("LLM_API_KEY")
	t.Setenv("SYNTHETIC_API_KEY", "")
	t.Setenv("LLM_API_KEY", "llm-fallback-key")
	defer func() {
		if originalSynthetic != "" {
			t.Setenv("SYNTHETIC_API_KEY", originalSynthetic)
		}
		if originalLLM != "" {
			t.Setenv("LLM_API_KEY", originalLLM)
		} else {
			t.Setenv("LLM_API_KEY", "")
		}
	}()

	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderSynthetic {
		t.Errorf("expected provider to be synthetic, got %s", client.Provider())
	}
}

// TestClientImplementsInterface verifies that Client implements llms.LLM
func TestClientImplementsInterface(_ *testing.T) {
	var _ llms.LLM = (*Client)(nil)
}

// TestClientImplementsEmbedder verifies that Client implements llms.Embedder
func TestClientImplementsEmbedder(_ *testing.T) {
	var _ llms.Embedder = (*Client)(nil)
}

func TestWithEmbeddingModel(t *testing.T) {
	opts := apply(
		WithAPIKey("test-key"),
		WithEmbeddingModel("hf:BAAI/bge-large-en-v1.5"),
	)

	if opts.EmbeddingModel != "hf:BAAI/bge-large-en-v1.5" {
		t.Errorf("expected embedding model to be hf:BAAI/bge-large-en-v1.5, got %s", opts.EmbeddingModel)
	}
}

// TestEmbedUsesDefaultModel verifies that a Synthetic client sends the catalog
// embedding model when the caller does not pick one.
func TestEmbedUsesDefaultModel(t *testing.T) {
	var gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotModel, _ = req["model"].(string)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"object": "embedding", "embedding": []float32{0.1, 0.2}, "index": 0},
			},
			"model": "hf:nomic-ai/nomic-embed-text-v1.5",
			"usage": map[string]any{"prompt_tokens": 1, "total_tokens": 1},
		})
	}))
	defer server.Close()

	client, err := New(
		WithAPIKey("test-key"),
		WithBaseURL(server.URL),
		WithAllowPrivateIPs(), WithAllowHTTP(),
	)
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}

	if _, err := client.Embed(context.Background(), []string{"test"}); err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if gotModel != "hf:nomic-ai/nomic-embed-text-v1.5" {
		t.Errorf("model = %q, want the default embedding model", gotModel)
	}
}

func TestWithHTTPClient(t *testing.T) {
	custom := &http.Client{Timeout: 3 * time.Second}
	opts := apply(
		WithAPIKey("test-key"),
		WithHTTPClient(custom),
	)

	if opts.HTTPClient != custom {
		t.Errorf("HTTPClient = %v, want the client passed in", opts.HTTPClient)
	}
}

func TestNewClientWithCustomBaseURL(t *testing.T) {
	customURL := "https://custom.synthetic.api/v1"
	client, err := New(
		WithAPIKey("test-key"),
		WithBaseURL(customURL),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.options.BaseURL != customURL {
		t.Errorf("expected BaseURL to be %s, got %s", customURL, client.options.BaseURL)
	}
}

func TestEmbedWithExplicitModel(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithEmbeddingModel("hf:BAAI/bge-large-en-v1.5"),
	)
	if err != nil {
		t.Fatalf("unexpected error creating client: %v", err)
	}

	if client.options.EmbeddingModel != "hf:BAAI/bge-large-en-v1.5" {
		t.Errorf("expected embedding model to be set, got %s", client.options.EmbeddingModel)
	}
}
