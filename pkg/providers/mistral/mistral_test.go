package mistral

import (
	"errors"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v3"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.Model != "mistral-large-latest" {
		t.Errorf("unexpected default model: %s", opts.Model)
	}

	if opts.BaseURL != "https://api.mistral.ai/v1" {
		t.Errorf("unexpected default base URL: %s", opts.BaseURL)
	}

	if opts.EmbeddingModel != "mistral-embed" {
		t.Errorf("unexpected default embedding model: %s", opts.EmbeddingModel)
	}
}

func TestWithOptions(t *testing.T) {
	opts := apply(
		WithAPIKey("test-key"),
		WithModel("codestral-latest"),
		WithEmbeddingModel("mistral-embed"),
	)

	if opts.APIKey != "test-key" {
		t.Errorf("expected API key test-key, got %s", opts.APIKey)
	}

	if opts.Model != "codestral-latest" {
		t.Errorf("unexpected model: %s", opts.Model)
	}
}

func TestNewWithAPIKey(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderMistral {
		t.Errorf("expected provider mistral, got %s", client.Provider())
	}
}

func TestNewWithoutAPIKey(t *testing.T) {
	t.Setenv("MISTRAL_API_KEY", "")
	t.Setenv("LLM_API_KEY", "")

	_, err := New()
	if !errors.Is(err, llms.ErrMissingAPIKey) {
		t.Errorf("expected ErrMissingAPIKey, got %v", err)
	}
}

func TestCapabilities(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	caps := client.Capabilities()

	if !caps.Streaming {
		t.Error("expected streaming to be supported")
	}

	if !caps.Tools {
		t.Error("expected tools to be supported")
	}

	if !caps.Embeddings {
		t.Error("expected embeddings to be supported")
	}

	// Mistral statically declares Vision:true (Pixtral models support vision).
	// The capability merge ensures this explicit declaration wins over the
	// conservative registry default (which reports Vision:false), so the provider
	// must report Vision:true.
	if !caps.Vision {
		t.Error("expected vision to be supported (static Vision:true must win over registry default)")
	}

	if !caps.JSONMode {
		t.Error("expected JSON mode to be supported")
	}
}

func TestInferModelTypes(t *testing.T) {
	tests := []struct {
		modelID       string
		expectedTypes []llms.ModelType
	}{
		{"mistral-large-latest", []llms.ModelType{llms.ModelTypeChat}},
		{"codestral-latest", []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode}},
		{"pixtral-12b-2409", []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision}},
		{"mistral-embed", []llms.ModelType{llms.ModelTypeEmbedding}},
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
