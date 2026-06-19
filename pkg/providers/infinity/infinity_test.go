package infinity

import (
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v3"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.EmbeddingModel != "michaelfeil/bge-small-en-v1.5" {
		t.Errorf("unexpected default embedding model: %s", opts.EmbeddingModel)
	}

	if opts.RerankModel != "mixedbread-ai/mxbai-rerank-xsmall-v1" {
		t.Errorf("unexpected default rerank model: %s", opts.RerankModel)
	}

	if opts.BaseURL != "http://localhost:7997/v1" {
		t.Errorf("unexpected default base URL: %s", opts.BaseURL)
	}
}

func TestWithOptions(t *testing.T) {
	opts := apply(
		WithEmbeddingModel("custom-embed-model"),
		WithRerankModel("custom-rerank-model"),
		WithBaseURL("http://custom:8080/v1"),
		WithAPIKey("test-key"),
	)

	if opts.EmbeddingModel != "custom-embed-model" {
		t.Errorf("unexpected embedding model: %s", opts.EmbeddingModel)
	}

	if opts.RerankModel != "custom-rerank-model" {
		t.Errorf("unexpected rerank model: %s", opts.RerankModel)
	}

	if opts.BaseURL != "http://custom:8080/v1" {
		t.Errorf("unexpected base URL: %s", opts.BaseURL)
	}

	if opts.APIKey != "test-key" {
		t.Errorf("unexpected API key: %s", opts.APIKey)
	}
}

func TestNewWithoutAPIKey(t *testing.T) {
	// Infinity should work without an API key (local mode)
	t.Setenv("INFINITY_API_KEY", "")

	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error for local Infinity: %v", err)
	}

	if client.Provider() != llms.ProviderInfinity {
		t.Errorf("expected provider infinity, got %s", client.Provider())
	}
}

func TestCapabilities(t *testing.T) {
	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	caps := client.Capabilities()

	if !caps.Embeddings {
		t.Error("expected embeddings to be supported")
	}

	if !caps.Batch {
		t.Error("expected batch to be supported")
	}

	if caps.Streaming {
		t.Error("expected streaming to NOT be supported")
	}

	if caps.Tools {
		t.Error("expected tools to NOT be supported")
	}

	if caps.Vision {
		t.Error("expected vision to NOT be supported")
	}
}

func TestImplementsEmbedder(t *testing.T) {
	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, ok := llms.AsEmbedder(client)
	if !ok {
		t.Error("expected client to implement Embedder interface")
	}
}

func TestImplementsReranker(t *testing.T) {
	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, ok := llms.AsReranker(client)
	if !ok {
		t.Error("expected client to implement Reranker interface")
	}
}

func TestAPIKeyFromEnv(t *testing.T) {
	t.Setenv("INFINITY_API_KEY", "env-api-key")

	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.options.APIKey != "env-api-key" {
		t.Errorf("expected API key from env, got %q", client.options.APIKey)
	}
}

func TestAPIKeyOption(t *testing.T) {
	t.Setenv("INFINITY_API_KEY", "env-api-key")

	client, err := New(WithAPIKey("option-api-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Option should take precedence
	if client.options.APIKey != "option-api-key" {
		t.Errorf("expected API key from option, got %q", client.options.APIKey)
	}
}
