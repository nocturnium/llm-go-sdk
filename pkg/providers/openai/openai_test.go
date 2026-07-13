package openai

import (
	"errors"
	"os"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v5"
)

const (
	testGPT4o = "gpt-4o"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.Model != testGPT4o {
		t.Errorf("expected default model to be gpt-4o, got %s", opts.Model)
	}
	if opts.APIKey != "" {
		t.Error("expected default API key to be empty")
	}
	if opts.BaseURL != "" {
		t.Error("expected default BaseURL to be empty")
	}
	if opts.Organization != "" {
		t.Error("expected default Organization to be empty")
	}
}

func TestApplyOptions(t *testing.T) {
	opts := apply(
		WithAPIKey("test-key"),
		WithModel("gpt-4-turbo"),
		WithBaseURL("https://custom.api.com"),
		WithOrganization("org-123"),
	)

	if opts.APIKey != "test-key" {
		t.Errorf("expected API key to be test-key, got %s", opts.APIKey)
	}
	if opts.Model != "gpt-4-turbo" {
		t.Errorf("expected model to be gpt-4-turbo, got %s", opts.Model)
	}
	if opts.BaseURL != "https://custom.api.com" {
		t.Errorf("expected BaseURL to be https://custom.api.com, got %s", opts.BaseURL)
	}
	if opts.Organization != "org-123" {
		t.Errorf("expected Organization to be org-123, got %s", opts.Organization)
	}
}

func TestNewClientMissingAPIKey(t *testing.T) {
	// Ensure env var is not set
	originalKey := os.Getenv("OPENAI_API_KEY")
	_ = os.Unsetenv("OPENAI_API_KEY")
	_ = os.Unsetenv("LLM_API_KEY")
	defer func() {
		if originalKey != "" {
			t.Setenv("OPENAI_API_KEY", originalKey)
		}
	}()

	_, err := New()
	if !errors.Is(err, llms.ErrMissingAPIKey) {
		t.Errorf("expected ErrMissingAPIKey, got %v", err)
	}
}

func TestNewClientWithEnvAPIKey(t *testing.T) {
	// Set env var
	originalKey := os.Getenv("OPENAI_API_KEY")
	t.Setenv("OPENAI_API_KEY", "env-test-key")
	_ = os.Unsetenv("LLM_API_KEY")
	defer func() {
		if originalKey != "" {
			t.Setenv("OPENAI_API_KEY", originalKey)
		} else {
			_ = os.Unsetenv("OPENAI_API_KEY")
		}
	}()

	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderOpenAI {
		t.Errorf("expected provider to be openai, got %s", client.Provider())
	}
	if client.Model() != testGPT4o {
		t.Errorf("expected default model, got %s", client.Model())
	}
}

func TestNewClientWithExplicitAPIKey(t *testing.T) {
	client, err := New(WithAPIKey("explicit-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderOpenAI {
		t.Errorf("expected provider to be openai, got %s", client.Provider())
	}
}

func TestNewClientWithCustomModel(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithModel("gpt-4-turbo"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Model() != "gpt-4-turbo" {
		t.Errorf("expected model to be gpt-4-turbo, got %s", client.Model())
	}
}

func TestNewClientWithOrganization(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithOrganization("org-test"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.options.Organization != "org-test" {
		t.Errorf("expected organization to be org-test, got %s", client.options.Organization)
	}
}

func TestNewClientWithLLMAPIKeyFallback(t *testing.T) {
	// Ensure provider-specific env var is not set but LLM_API_KEY is
	originalOpenAI := os.Getenv("OPENAI_API_KEY")
	originalLLM := os.Getenv("LLM_API_KEY")
	_ = os.Unsetenv("OPENAI_API_KEY")
	t.Setenv("LLM_API_KEY", "llm-fallback-key")
	defer func() {
		if originalOpenAI != "" {
			t.Setenv("OPENAI_API_KEY", originalOpenAI)
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

	if client.Provider() != llms.ProviderOpenAI {
		t.Errorf("expected provider to be openai, got %s", client.Provider())
	}
}

// TestClientImplementsInterface verifies that Client implements llms.LLM
func TestClientImplementsInterface(_ *testing.T) {
	var _ llms.LLM = (*Client)(nil)
}
