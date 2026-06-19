package featherless

import (
	"errors"
	"os"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v3"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.Model != "Qwen/Qwen3-32B" {
		t.Errorf("expected default model to be Qwen/Qwen3-32B, got %s", opts.Model)
	}
	if opts.BaseURL != "https://api.featherless.ai/v1" {
		t.Errorf("expected default BaseURL to be https://api.featherless.ai/v1, got %s", opts.BaseURL)
	}
	if opts.APIKey != "" {
		t.Error("expected default API key to be empty")
	}
}

func TestApplyOptions(t *testing.T) {
	opts := apply(
		WithAPIKey("test-key"),
		WithModel("meta-llama/Meta-Llama-3.1-70B-Instruct"),
		WithBaseURL("https://custom.api.com"),
	)

	if opts.APIKey != "test-key" {
		t.Errorf("expected API key to be test-key, got %s", opts.APIKey)
	}
	if opts.Model != "meta-llama/Meta-Llama-3.1-70B-Instruct" {
		t.Errorf("expected model to be meta-llama/Meta-Llama-3.1-70B-Instruct, got %s", opts.Model)
	}
	if opts.BaseURL != "https://custom.api.com" {
		t.Errorf("expected BaseURL to be https://custom.api.com, got %s", opts.BaseURL)
	}
}

func TestNewClientMissingAPIKey(t *testing.T) {
	// Ensure env var is not set
	originalKey := os.Getenv("FEATHERLESS_API_KEY")
	_ = os.Unsetenv("FEATHERLESS_API_KEY")
	_ = os.Unsetenv("LLM_API_KEY")
	defer func() {
		if originalKey != "" {
			t.Setenv("FEATHERLESS_API_KEY", originalKey)
		}
	}()

	_, err := New()
	if !errors.Is(err, llms.ErrMissingAPIKey) {
		t.Errorf("expected ErrMissingAPIKey, got %v", err)
	}
}

func TestNewClientWithEnvAPIKey(t *testing.T) {
	// Set env var
	originalKey := os.Getenv("FEATHERLESS_API_KEY")
	t.Setenv("FEATHERLESS_API_KEY", "env-test-key")
	defer func() {
		if originalKey != "" {
			t.Setenv("FEATHERLESS_API_KEY", originalKey)
		} else {
			_ = os.Unsetenv("FEATHERLESS_API_KEY")
		}
	}()

	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderFeatherless {
		t.Errorf("expected provider to be featherless, got %s", client.Provider())
	}
	if client.Model() != "Qwen/Qwen3-32B" {
		t.Errorf("expected default model, got %s", client.Model())
	}
}

func TestNewClientWithExplicitAPIKey(t *testing.T) {
	client, err := New(WithAPIKey("explicit-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderFeatherless {
		t.Errorf("expected provider to be featherless, got %s", client.Provider())
	}
}

func TestNewClientWithCustomModel(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithModel("mistralai/Mistral-7B-Instruct-v0.2"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Model() != "mistralai/Mistral-7B-Instruct-v0.2" {
		t.Errorf("expected model to be mistralai/Mistral-7B-Instruct-v0.2, got %s", client.Model())
	}
}

func TestNewClientPreservesBaseURL(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the default base URL is preserved in options
	if client.options.BaseURL != "https://api.featherless.ai/v1" {
		t.Errorf("expected BaseURL to be https://api.featherless.ai/v1, got %s", client.options.BaseURL)
	}
}

func TestNewClientWithLLMAPIKeyFallback(t *testing.T) {
	// Ensure provider-specific env var is not set but LLM_API_KEY is
	originalFeatherless := os.Getenv("FEATHERLESS_API_KEY")
	originalLLM := os.Getenv("LLM_API_KEY")
	_ = os.Unsetenv("FEATHERLESS_API_KEY")
	t.Setenv("LLM_API_KEY", "llm-fallback-key")
	defer func() {
		if originalFeatherless != "" {
			t.Setenv("FEATHERLESS_API_KEY", originalFeatherless)
		}
		if originalLLM != "" {
			t.Setenv("LLM_API_KEY", originalLLM)
		} else {
			_ = os.Unsetenv("LLM_API_KEY")
		}
	}()

	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderFeatherless {
		t.Errorf("expected provider to be featherless, got %s", client.Provider())
	}
}

// TestClientImplementsInterface verifies that Client implements llms.LLM.
func TestClientImplementsInterface(_ *testing.T) {
	var _ llms.LLM = (*Client)(nil)
}
