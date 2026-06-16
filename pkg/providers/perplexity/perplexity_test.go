package perplexity

import (
	"errors"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v2"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.Model != "sonar" {
		t.Errorf("unexpected default model: %s", opts.Model)
	}

	if opts.BaseURL != "https://api.perplexity.ai" {
		t.Errorf("unexpected default base URL: %s", opts.BaseURL)
	}
}

func TestWithOptions(t *testing.T) {
	opts := apply(
		WithAPIKey("test-key"),
		WithModel("llama-3.1-sonar-small-128k-online"),
	)

	if opts.APIKey != "test-key" {
		t.Errorf("expected API key test-key, got %s", opts.APIKey)
	}

	if opts.Model != "llama-3.1-sonar-small-128k-online" {
		t.Errorf("unexpected model: %s", opts.Model)
	}
}

func TestNewWithAPIKey(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderPerplexity {
		t.Errorf("expected provider perplexity, got %s", client.Provider())
	}
}

func TestNewWithoutAPIKey(t *testing.T) {
	t.Setenv("PERPLEXITY_API_KEY", "")
	t.Setenv("PPLX_API_KEY", "")
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

	if caps.Tools {
		t.Error("expected tools to not be supported")
	}

	if caps.Embeddings {
		t.Error("expected embeddings to not be supported")
	}

	if !caps.JSONMode {
		t.Error("expected JSON mode to be supported")
	}
}
