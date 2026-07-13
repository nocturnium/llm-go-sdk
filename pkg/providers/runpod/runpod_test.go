package runpod

import (
	"errors"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v5"
)

const (
	testLlamaModel = "meta-llama/Meta-Llama-3.1-8B-Instruct"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.BaseURL != "https://api.runpod.ai/v2" {
		t.Errorf("expected default BaseURL to be https://api.runpod.ai/v2, got %s", opts.BaseURL)
	}
	if opts.APIKey != "" {
		t.Error("expected default API key to be empty")
	}
	if opts.EndpointID != "" {
		t.Error("expected default EndpointID to be empty")
	}
	if opts.Model != "" {
		t.Error("expected default Model to be empty")
	}
}

func TestWithAPIKey(t *testing.T) {
	opts := apply(WithAPIKey("test-key"))
	if opts.APIKey != "test-key" {
		t.Errorf("expected APIKey 'test-key', got %s", opts.APIKey)
	}
}

func TestWithEndpointID(t *testing.T) {
	opts := apply(WithEndpointID("abc123"))
	if opts.EndpointID != "abc123" {
		t.Errorf("expected EndpointID 'abc123', got %s", opts.EndpointID)
	}
}

func TestWithModel(t *testing.T) {
	opts := apply(WithModel(testLlamaModel))
	if opts.Model != testLlamaModel {
		t.Errorf("expected Model '%s', got %s", testLlamaModel, opts.Model)
	}
}

func TestWithBaseURL(t *testing.T) {
	opts := apply(WithBaseURL("https://custom.api.runpod.ai"))
	if opts.BaseURL != "https://custom.api.runpod.ai" {
		t.Errorf("expected BaseURL 'https://custom.api.runpod.ai', got %s", opts.BaseURL)
	}
}

func TestWithEmbeddingModel(t *testing.T) {
	opts := apply(WithEmbeddingModel("BAAI/bge-small-en-v1.5"))
	if opts.EmbeddingModel != "BAAI/bge-small-en-v1.5" {
		t.Errorf("expected EmbeddingModel 'BAAI/bge-small-en-v1.5', got %s", opts.EmbeddingModel)
	}
}

func TestNew_MissingAPIKey(t *testing.T) {
	// Clear any environment variables that might interfere
	t.Setenv("RUNPOD_API_KEY", "")
	t.Setenv("LLM_API_KEY", "")

	_, err := New(WithEndpointID("abc123"))
	if err == nil {
		t.Error("expected error for missing API key")
	}
}

func TestNew_MissingEndpointID(t *testing.T) {
	_, err := New(WithAPIKey("test-key"))
	if err == nil {
		t.Error("expected error for missing endpoint ID")
	}
	if !errors.Is(err, ErrMissingEndpointID) {
		t.Errorf("expected ErrMissingEndpointID, got %v", err)
	}
}

func TestNew_Success(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithEndpointID("abc123"),
		WithModel(testLlamaModel),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderRunPod {
		t.Errorf("expected provider RunPod, got %s", client.Provider())
	}

	if client.Model() != testLlamaModel {
		t.Errorf("expected model '%s', got %s", testLlamaModel, client.Model())
	}
}

func TestNew_WithEnvAPIKey(t *testing.T) {
	t.Setenv("RUNPOD_API_KEY", "env-test-key")

	client, err := New(
		WithEndpointID("abc123"),
		WithModel(testLlamaModel),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderRunPod {
		t.Errorf("expected provider RunPod, got %s", client.Provider())
	}
}

func TestClient_GetCapabilities(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithEndpointID("abc123"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	caps := client.Capabilities()

	if !caps.Streaming {
		t.Error("expected Streaming capability")
	}
	if caps.Tools {
		t.Error("expected Tools capability to be unknown for arbitrary deployment")
	}
	if !caps.JSONMode {
		t.Error("expected JSONMode capability")
	}
}

func TestApply_MultipleOptions(t *testing.T) {
	opts := apply(
		WithAPIKey("my-key"),
		WithEndpointID("my-endpoint"),
		WithModel("my-model"),
		WithBaseURL("https://custom.url"),
		WithEmbeddingModel("my-embedding-model"),
	)

	if opts.APIKey != "my-key" {
		t.Errorf("expected APIKey 'my-key', got %s", opts.APIKey)
	}
	if opts.EndpointID != "my-endpoint" {
		t.Errorf("expected EndpointID 'my-endpoint', got %s", opts.EndpointID)
	}
	if opts.Model != "my-model" {
		t.Errorf("expected Model 'my-model', got %s", opts.Model)
	}
	if opts.BaseURL != "https://custom.url" {
		t.Errorf("expected BaseURL 'https://custom.url', got %s", opts.BaseURL)
	}
	if opts.EmbeddingModel != "my-embedding-model" {
		t.Errorf("expected EmbeddingModel 'my-embedding-model', got %s", opts.EmbeddingModel)
	}
}
