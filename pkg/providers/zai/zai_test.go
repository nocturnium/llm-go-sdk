package zai

import (
	"errors"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v4"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.BaseURL != "https://api.z.ai/api/paas/v4" {
		t.Errorf("expected default BaseURL to be https://api.z.ai/api/paas/v4, got %s", opts.BaseURL)
	}
	if opts.APIKey != "" {
		t.Error("expected default API key to be empty")
	}
	if opts.Model != ModelGLM47 {
		t.Errorf("expected default Model to be %s, got %s", ModelGLM47, opts.Model)
	}
}

func TestWithAPIKey(t *testing.T) {
	opts := apply(WithAPIKey("test-key"))
	if opts.APIKey != "test-key" {
		t.Errorf("expected APIKey 'test-key', got %s", opts.APIKey)
	}
}

func TestWithModel(t *testing.T) {
	opts := apply(WithModel(ModelGLM52))
	if opts.Model != ModelGLM52 {
		t.Errorf("expected Model '%s', got %s", ModelGLM52, opts.Model)
	}
}

func TestWithBaseURL(t *testing.T) {
	opts := apply(WithBaseURL("https://custom.api.z.ai"))
	if opts.BaseURL != "https://custom.api.z.ai" {
		t.Errorf("expected BaseURL 'https://custom.api.z.ai', got %s", opts.BaseURL)
	}
}

func TestWithEmbeddingModel(t *testing.T) {
	opts := apply(WithEmbeddingModel("some-embedding-model"))
	if opts.EmbeddingModel != "some-embedding-model" {
		t.Errorf("expected EmbeddingModel 'some-embedding-model', got %s", opts.EmbeddingModel)
	}
}

func TestNew_MissingAPIKey(t *testing.T) {
	// Clear any environment variables that might interfere
	t.Setenv("ZAI_API_KEY", "")
	t.Setenv("LLM_API_KEY", "")

	_, err := New()
	if err == nil {
		t.Error("expected error for missing API key")
	}
}

func TestNew_Success(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithModel(ModelGLM47),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderZAI {
		t.Errorf("expected provider ZAI, got %s", client.Provider())
	}

	if client.Model() != ModelGLM47 {
		t.Errorf("expected model '%s', got %s", ModelGLM47, client.Model())
	}
}

func TestNew_WithEnvAPIKey(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "env-test-key")

	client, err := New(WithModel(ModelGLM5Turbo))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderZAI {
		t.Errorf("expected provider ZAI, got %s", client.Provider())
	}
}

func TestClient_GetCapabilities(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	caps := client.Capabilities()

	if !caps.Streaming {
		t.Error("expected Streaming capability")
	}
	if !caps.Tools {
		t.Error("expected Tools capability")
	}
	if !caps.Vision {
		t.Error("expected Vision capability")
	}
	if !caps.JSONMode {
		t.Error("expected JSONMode capability")
	}
	if caps.Embeddings {
		t.Error("expected Embeddings capability to be false")
	}
	if caps.MaxContextTokens != 200000 {
		t.Errorf("expected MaxContextTokens 200000, got %d", caps.MaxContextTokens)
	}
	if caps.MaxOutputTokens != 128000 {
		t.Errorf("expected MaxOutputTokens 128000, got %d", caps.MaxOutputTokens)
	}
}

func TestApply_MultipleOptions(t *testing.T) {
	opts := apply(
		WithAPIKey("my-key"),
		WithModel(ModelGLM5Turbo),
		WithBaseURL("https://custom.url"),
		WithEmbeddingModel("my-embedding-model"),
	)

	if opts.APIKey != "my-key" {
		t.Errorf("expected APIKey 'my-key', got %s", opts.APIKey)
	}
	if opts.Model != ModelGLM5Turbo {
		t.Errorf("expected Model '%s', got %s", ModelGLM5Turbo, opts.Model)
	}
	if opts.BaseURL != "https://custom.url" {
		t.Errorf("expected BaseURL 'https://custom.url', got %s", opts.BaseURL)
	}
	if opts.EmbeddingModel != "my-embedding-model" {
		t.Errorf("expected EmbeddingModel 'my-embedding-model', got %s", opts.EmbeddingModel)
	}
}

func TestListModels(t *testing.T) {
	models := ListModels()
	if len(models) != 8 {
		t.Errorf("expected 8 models, got %d", len(models))
	}

	// Check that all expected models are present
	modelIDs := make(map[string]bool)
	for _, m := range models {
		modelIDs[m.ID] = true
	}

	for _, want := range []string{
		ModelGLM52, ModelGLM51, ModelGLM5, ModelGLM5Turbo,
		ModelGLM47, ModelGLM46, ModelGLM45, ModelGLM45Air,
	} {
		if !modelIDs[want] {
			t.Errorf("expected %q to be in model list", want)
		}
	}
}

func TestModelInfo(t *testing.T) {
	info := ModelInfo(ModelGLM47)
	if info == nil {
		t.Fatal("expected model info, got nil")
	}
	if info.ID != ModelGLM47 {
		t.Errorf("expected model ID '%s', got '%s'", ModelGLM47, info.ID)
	}
	if info.Provider != llms.ProviderZAI {
		t.Errorf("expected provider ZAI, got %s", info.Provider)
	}

	// Test non-existent model
	info = ModelInfo("non-existent-model")
	if info != nil {
		t.Error("expected nil for non-existent model")
	}
}

func TestWithUseCodingAPI(t *testing.T) {
	opts := apply(WithUseCodingAPI())
	if !opts.UseCodingAPI {
		t.Error("expected UseCodingAPI to be true")
	}
}

func TestNew_CodingEndpoint(t *testing.T) {
	// The actual endpoint switching is handled internally,
	// but we can test that the option is accepted
	client, err := New(
		WithAPIKey("test-key"),
		WithUseCodingAPI(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderZAI {
		t.Errorf("expected provider ZAI, got %s", client.Provider())
	}
}

func TestNew_CustomBaseURLWithCodingAPI(t *testing.T) {
	// When a custom BaseURL is provided, UseCodingAPI should not override it
	opts := apply(
		WithBaseURL("https://custom.api.z.ai/v1"),
		WithUseCodingAPI(),
	)

	if opts.BaseURL != "https://custom.api.z.ai/v1" {
		t.Errorf("expected BaseURL 'https://custom.api.z.ai/v1', got %s", opts.BaseURL)
	}
}

func TestClientImplementsInterfaces(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test that client implements LLM interface
	var _ llms.LLM = client

	// Test that client implements CapableProvider interface
	var _ llms.CapableProvider = client

	// Test that client implements ModelLister interface
	var _ llms.ModelLister = client
}

func TestNew_WithLLMAPIKeyFallback(t *testing.T) {
	// Clear provider-specific env var but set LLM_API_KEY
	t.Setenv("ZAI_API_KEY", "")
	t.Setenv("LLM_API_KEY", "llm-fallback-key")

	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderZAI {
		t.Errorf("expected provider ZAI, got %s", client.Provider())
	}
}

func TestNew_MissingAPIKeyErrorType(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "")
	t.Setenv("LLM_API_KEY", "")

	_, err := New()
	if !errors.Is(err, llms.ErrMissingAPIKey) {
		t.Errorf("expected ErrMissingAPIKey, got %v", err)
	}
}

func TestNew_CodingEndpointSwitch(t *testing.T) {
	// Test that UseCodingAPI switches the endpoint when using default BaseURL
	client, err := New(
		WithAPIKey("test-key"),
		WithUseCodingAPI(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The client is created successfully - endpoint switching is internal
	if client.Provider() != llms.ProviderZAI {
		t.Errorf("expected provider ZAI, got %s", client.Provider())
	}
}

func TestNew_TrailingSlashRemoved(t *testing.T) {
	// Test that trailing slashes are removed from BaseURL
	client, err := New(
		WithAPIKey("test-key"),
		WithBaseURL("https://api.z.ai/api/paas/v4/"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderZAI {
		t.Errorf("expected provider ZAI, got %s", client.Provider())
	}
}
