package deepseek

import (
	"errors"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v4"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.Model != "deepseek-chat" {
		t.Errorf("unexpected default model: %s", opts.Model)
	}

	if opts.BaseURL != "https://api.deepseek.com/v1" {
		t.Errorf("unexpected default base URL: %s", opts.BaseURL)
	}
}

func TestWithOptions(t *testing.T) {
	opts := apply(
		WithAPIKey("test-key"),
		WithModel("deepseek-coder"),
	)

	if opts.APIKey != "test-key" {
		t.Errorf("expected API key test-key, got %s", opts.APIKey)
	}

	if opts.Model != "deepseek-coder" {
		t.Errorf("unexpected model: %s", opts.Model)
	}
}

func TestNewWithAPIKey(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderDeepSeek {
		t.Errorf("expected provider deepseek, got %s", client.Provider())
	}
}

func TestNewWithoutAPIKey(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
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

	if caps.Embeddings {
		t.Error("expected embeddings to not be supported")
	}

	if caps.Vision {
		t.Error("expected vision to not be supported")
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
		{"deepseek-chat", []llms.ModelType{llms.ModelTypeChat}},
		{"deepseek-coder", []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeCode}},
		{"deepseek-reasoner", []llms.ModelType{llms.ModelTypeChat}},
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
