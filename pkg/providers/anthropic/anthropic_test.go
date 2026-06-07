package anthropic

import (
	"errors"
	"os"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected default model to be claude-sonnet-4-20250514, got %s", opts.Model)
	}
	if opts.APIKey != "" {
		t.Error("expected default API key to be empty")
	}
	if opts.BaseURL != "" {
		t.Error("expected default BaseURL to be empty")
	}
}

func TestApplyOptions(t *testing.T) {
	opts := apply(
		WithAPIKey("test-key"),
		WithModel("claude-opus-4-20250514"),
		WithBaseURL("https://custom.api.com"),
	)

	if opts.APIKey != "test-key" {
		t.Errorf("expected API key to be test-key, got %s", opts.APIKey)
	}
	if opts.Model != "claude-opus-4-20250514" {
		t.Errorf("expected model to be claude-opus-4-20250514, got %s", opts.Model)
	}
	if opts.BaseURL != "https://custom.api.com" {
		t.Errorf("expected BaseURL to be https://custom.api.com, got %s", opts.BaseURL)
	}
}

func TestNewClientMissingAPIKey(t *testing.T) {
	// Ensure env var is not set
	originalKey := os.Getenv("ANTHROPIC_API_KEY")
	_ = os.Unsetenv("ANTHROPIC_API_KEY")
	_ = os.Unsetenv("LLM_API_KEY")
	defer func() {
		if originalKey != "" {
			t.Setenv("ANTHROPIC_API_KEY", originalKey)
		}
	}()

	_, err := New()
	if !errors.Is(err, llms.ErrMissingAPIKey) {
		t.Errorf("expected ErrMissingAPIKey, got %v", err)
	}
}

func TestNewClientWithEnvAPIKey(t *testing.T) {
	// Set env var
	originalKey := os.Getenv("ANTHROPIC_API_KEY")
	t.Setenv("ANTHROPIC_API_KEY", "env-test-key")
	_ = os.Unsetenv("LLM_API_KEY")
	defer func() {
		if originalKey != "" {
			t.Setenv("ANTHROPIC_API_KEY", originalKey)
		} else {
			_ = os.Unsetenv("ANTHROPIC_API_KEY")
		}
	}()

	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderAnthropic {
		t.Errorf("expected provider to be anthropic, got %s", client.Provider())
	}
	if client.Model() != "claude-sonnet-4-20250514" {
		t.Errorf("expected default model, got %s", client.Model())
	}
}

func TestNewClientWithExplicitAPIKey(t *testing.T) {
	client, err := New(WithAPIKey("explicit-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderAnthropic {
		t.Errorf("expected provider to be anthropic, got %s", client.Provider())
	}
}

func TestNewClientWithCustomModel(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-opus-4-20250514"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Model() != "claude-opus-4-20250514" {
		t.Errorf("expected model to be claude-opus-4-20250514, got %s", client.Model())
	}
}

func TestNewClientWithLLMAPIKeyFallback(t *testing.T) {
	// Ensure provider-specific env var is not set but LLM_API_KEY is
	originalAnthropic := os.Getenv("ANTHROPIC_API_KEY")
	originalLLM := os.Getenv("LLM_API_KEY")
	_ = os.Unsetenv("ANTHROPIC_API_KEY")
	_ = os.Unsetenv("LLM_API_KEY")
	t.Setenv("LLM_API_KEY", "llm-fallback-key")
	defer func() {
		if originalAnthropic != "" {
			t.Setenv("ANTHROPIC_API_KEY", originalAnthropic)
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

	if client.Provider() != llms.ProviderAnthropic {
		t.Errorf("expected provider to be anthropic, got %s", client.Provider())
	}
}

// TestClientImplementsInterface verifies that Client implements llms.LLM.
func TestClientImplementsInterface(_ *testing.T) {
	var _ llms.LLM = (*Client)(nil)
}

// TestBuildRequest_PartsBasedSystemMessage verifies that a system message built
// from Parts (rather than the simple Content field) still reaches the request's
// System block. Previously buildRequest read msg.Content only, silently dropping
// a Parts-based system prompt. This is the FIX 5 acceptance test.
func TestBuildRequest_PartsBasedSystemMessage(t *testing.T) {
	client, err := New(WithAPIKey("test-key"), WithModel("claude-3-5-sonnet-20241022"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	messages := []llms.Message{
		{
			Role:  llms.RoleSystem,
			Parts: []llms.ContentPart{llms.NewTextPart("You are a pirate.")},
		},
		{Role: llms.RoleUser, Content: "Hello"},
	}

	req, err := client.buildRequest(messages, llms.DefaultCallOptions(), false)
	if err != nil {
		t.Fatalf("buildRequest returned error: %v", err)
	}

	if len(req.System) != 1 {
		t.Fatalf("expected 1 system block, got %d", len(req.System))
	}
	if req.System[0].Text != "You are a pirate." {
		t.Errorf("expected system text 'You are a pirate.', got %q", req.System[0].Text)
	}
}
