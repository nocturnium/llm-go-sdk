package ollama

import (
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.Model != "llama3.2" {
		t.Errorf("unexpected default model: %s", opts.Model)
	}

	if opts.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("unexpected default base URL: %s", opts.BaseURL)
	}

	if opts.EmbeddingModel != "nomic-embed-text" {
		t.Errorf("unexpected default embedding model: %s", opts.EmbeddingModel)
	}
}

func TestWithOptions(t *testing.T) {
	opts := apply(
		WithModel("mistral"),
		WithBaseURL("http://remote-ollama:11434/v1"),
		WithAPIKey("optional-key"),
	)

	if opts.Model != "mistral" {
		t.Errorf("unexpected model: %s", opts.Model)
	}

	if opts.BaseURL != "http://remote-ollama:11434/v1" {
		t.Errorf("unexpected base URL: %s", opts.BaseURL)
	}

	if opts.APIKey != "optional-key" {
		t.Errorf("unexpected API key: %s", opts.APIKey)
	}
}

func TestNewWithoutAPIKey(t *testing.T) {
	// Ollama should work without an API key (local mode)
	t.Setenv("OLLAMA_API_KEY", "")

	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error for local Ollama: %v", err)
	}

	if client.Provider() != llms.ProviderOllama {
		t.Errorf("expected provider ollama, got %s", client.Provider())
	}
}

func TestOllamaHostEnvVar(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "http://custom-host:11434")

	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The client should have used the custom host
	if client.Provider() != llms.ProviderOllama {
		t.Errorf("expected provider ollama, got %s", client.Provider())
	}
}

func TestCapabilities(t *testing.T) {
	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	caps := client.Capabilities()

	if !caps.Streaming {
		t.Error("expected streaming to be supported")
	}

	if caps.Tools {
		t.Error("expected tools capability to be unknown for default local model")
	}

	if !caps.Embeddings {
		t.Error("expected embeddings to be supported")
	}

	if caps.Vision {
		t.Error("expected vision capability to be unknown for default local model")
	}

	if !caps.JSONMode {
		t.Error("expected JSON mode to be supported")
	}
}

func TestInferOrganization(t *testing.T) {
	tests := []struct {
		modelID  string
		expected string
	}{
		{"llama3.2", "Meta"},
		{"mistral", "Mistral AI"},
		{"gemma2", "Google"},
		{"phi3", "Microsoft"},
		{"qwen2", "Alibaba"},
		{"nomic-embed-text", "Nomic AI"},
		{"unknown-model", "Ollama"},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			result := inferOrganization(tt.modelID)
			if result != tt.expected {
				t.Errorf("inferOrganization(%s) = %s, want %s", tt.modelID, result, tt.expected)
			}
		})
	}
}

func TestInferModelTypes(t *testing.T) {
	tests := []struct {
		modelID       string
		expectedTypes []llms.ModelType
	}{
		{"llama3.2", []llms.ModelType{llms.ModelTypeChat}},
		{"llava", []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision}},
		{"codellama", []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode}},
		{"nomic-embed-text", []llms.ModelType{llms.ModelTypeEmbedding}},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			result := inferModelTypes(tt.modelID)
			if len(result) != len(tt.expectedTypes) {
				t.Errorf("inferModelTypes(%s) returned %d types, want %d", tt.modelID, len(result), len(tt.expectedTypes))
			}
		})
	}
}
