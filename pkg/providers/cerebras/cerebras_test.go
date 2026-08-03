package cerebras

import (
	"errors"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.Model != "llama3.1-70b" {
		t.Errorf("unexpected default model: %s", opts.Model)
	}

	if opts.BaseURL != "https://api.cerebras.ai/v1" {
		t.Errorf("unexpected default base URL: %s", opts.BaseURL)
	}
}

func TestWithOptions(t *testing.T) {
	opts := apply(
		WithAPIKey("test-key"),
		WithModel("llama3.1-8b"),
	)

	if opts.APIKey != "test-key" {
		t.Errorf("expected API key test-key, got %s", opts.APIKey)
	}

	if opts.Model != "llama3.1-8b" {
		t.Errorf("unexpected model: %s", opts.Model)
	}
}

func TestNewWithAPIKey(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderCerebras {
		t.Errorf("expected provider cerebras, got %s", client.Provider())
	}
}

func TestNewWithoutAPIKey(t *testing.T) {
	t.Setenv("CEREBRAS_API_KEY", "")
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

func TestInferOrganization(t *testing.T) {
	tests := []struct {
		modelID  string
		expected string
	}{
		{"llama3.1-70b", "Meta"},
		{"llama3.1-8b", "Meta"},
		{"some-model", "Cerebras"},
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
