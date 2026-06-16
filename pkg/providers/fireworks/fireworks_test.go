package fireworks

import (
	"errors"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v2"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.Model != "accounts/fireworks/models/llama-v3p1-70b-instruct" {
		t.Errorf("unexpected default model: %s", opts.Model)
	}

	if opts.BaseURL != "https://api.fireworks.ai/inference/v1" {
		t.Errorf("unexpected default base URL: %s", opts.BaseURL)
	}
}

func TestWithOptions(t *testing.T) {
	opts := apply(
		WithAPIKey("test-key"),
		WithModel("accounts/fireworks/models/mixtral-8x22b-instruct"),
		WithBaseURL("https://custom.fireworks.ai/v1"),
	)

	if opts.APIKey != "test-key" {
		t.Errorf("expected API key test-key, got %s", opts.APIKey)
	}

	if opts.Model != "accounts/fireworks/models/mixtral-8x22b-instruct" {
		t.Errorf("unexpected model: %s", opts.Model)
	}
}

func TestNewWithAPIKey(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderFireworks {
		t.Errorf("expected provider fireworks, got %s", client.Provider())
	}
}

func TestNewWithoutAPIKey(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "")
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

	if !caps.JSONMode {
		t.Error("expected JSON mode to be supported")
	}
}

func TestInferOrganization(t *testing.T) {
	tests := []struct {
		modelID  string
		expected string
	}{
		{"accounts/fireworks/models/llama-v3p1-70b-instruct", "Meta"},
		{"accounts/fireworks/models/mixtral-8x22b-instruct", "Mistral AI"},
		{"accounts/fireworks/models/qwen2p5-72b-instruct", "Alibaba"},
		{"nomic-ai/nomic-embed-text-v1.5", "Nomic AI"},
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
