package gemini

import (
	"errors"
	"os"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk"
)

const (
	testGemini15Pro = "gemini-1.5-pro"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.Model != "gemini-2.0-flash" {
		t.Errorf("expected default model to be gemini-2.0-flash, got %s", opts.Model)
	}
	if opts.APIKey != "" {
		t.Error("expected default API key to be empty")
	}
}

func TestApplyOptions(t *testing.T) {
	opts := apply(
		WithAPIKey("test-key"),
		WithModel(testGemini15Pro),
	)

	if opts.APIKey != "test-key" {
		t.Errorf("expected API key to be test-key, got %s", opts.APIKey)
	}
	if opts.Model != testGemini15Pro {
		t.Errorf("expected model to be gemini-1.5-pro, got %s", opts.Model)
	}
}

func TestNewClientMissingAPIKey(t *testing.T) {
	// Ensure env vars are not set
	originalGemini := os.Getenv("GEMINI_API_KEY")
	originalGoogle := os.Getenv("GOOGLE_API_KEY")
	_ = os.Unsetenv("GEMINI_API_KEY")
	_ = os.Unsetenv("GOOGLE_API_KEY")
	_ = os.Unsetenv("LLM_API_KEY")
	defer func() {
		if originalGemini != "" {
			_ = os.Setenv("GEMINI_API_KEY", originalGemini)
		}
		if originalGoogle != "" {
			_ = os.Setenv("GOOGLE_API_KEY", originalGoogle)
		}
	}()

	_, err := New()
	if !errors.Is(err, llms.ErrMissingAPIKey) {
		t.Errorf("expected ErrMissingAPIKey, got %v", err)
	}
}

func TestNewClientWithGeminiEnvAPIKey(t *testing.T) {
	// Set GEMINI_API_KEY env var
	originalGemini := os.Getenv("GEMINI_API_KEY")
	originalGoogle := os.Getenv("GOOGLE_API_KEY")
	_ = os.Setenv("GEMINI_API_KEY", "gemini-env-key")
	_ = os.Unsetenv("GOOGLE_API_KEY")
	_ = os.Unsetenv("LLM_API_KEY")
	defer func() {
		if originalGemini != "" {
			_ = os.Setenv("GEMINI_API_KEY", originalGemini)
		} else {
			_ = os.Unsetenv("GEMINI_API_KEY")
		}
		if originalGoogle != "" {
			_ = os.Setenv("GOOGLE_API_KEY", originalGoogle)
		}
	}()

	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderGemini {
		t.Errorf("expected provider to be gemini, got %s", client.Provider())
	}
	if client.Model() != "gemini-2.0-flash" {
		t.Errorf("expected default model, got %s", client.Model())
	}
}

func TestNewClientWithGoogleEnvAPIKey(t *testing.T) {
	// Set GOOGLE_API_KEY env var (fallback)
	originalGemini := os.Getenv("GEMINI_API_KEY")
	originalGoogle := os.Getenv("GOOGLE_API_KEY")
	_ = os.Unsetenv("GEMINI_API_KEY")
	_ = os.Setenv("GOOGLE_API_KEY", "google-env-key")
	_ = os.Unsetenv("LLM_API_KEY")
	defer func() {
		if originalGemini != "" {
			_ = os.Setenv("GEMINI_API_KEY", originalGemini)
		}
		if originalGoogle != "" {
			_ = os.Setenv("GOOGLE_API_KEY", originalGoogle)
		} else {
			_ = os.Unsetenv("GOOGLE_API_KEY")
		}
	}()

	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderGemini {
		t.Errorf("expected provider to be gemini, got %s", client.Provider())
	}
}

func TestNewClientWithExplicitAPIKey(t *testing.T) {
	client, err := New(WithAPIKey("explicit-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderGemini {
		t.Errorf("expected provider to be gemini, got %s", client.Provider())
	}
}

func TestNewClientWithCustomModel(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithModel(testGemini15Pro),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Model() != testGemini15Pro {
		t.Errorf("expected model to be gemini-1.5-pro, got %s", client.Model())
	}
}

func TestNewClientWithLLMAPIKeyFallback(t *testing.T) {
	// Ensure provider-specific env vars are not set but LLM_API_KEY is
	originalGemini := os.Getenv("GEMINI_API_KEY")
	originalGoogle := os.Getenv("GOOGLE_API_KEY")
	originalLLM := os.Getenv("LLM_API_KEY")
	_ = os.Unsetenv("GEMINI_API_KEY")
	_ = os.Unsetenv("GOOGLE_API_KEY")
	_ = os.Setenv("LLM_API_KEY", "llm-fallback-key")
	defer func() {
		if originalGemini != "" {
			_ = os.Setenv("GEMINI_API_KEY", originalGemini)
		}
		if originalGoogle != "" {
			_ = os.Setenv("GOOGLE_API_KEY", originalGoogle)
		}
		if originalLLM != "" {
			_ = os.Setenv("LLM_API_KEY", originalLLM)
		} else {
			_ = os.Unsetenv("LLM_API_KEY")
		}
	}()

	client, err := New()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderGemini {
		t.Errorf("expected provider to be gemini, got %s", client.Provider())
	}
}

// TestClientImplementsInterface verifies that Client implements llms.LLM
func TestClientImplementsInterface(_ *testing.T) {
	var _ llms.LLM = (*Client)(nil)
}
