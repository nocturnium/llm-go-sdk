package groq

import (
	"errors"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v2"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.Model != "llama-3.3-70b-versatile" {
		t.Errorf("expected default model llama-3.3-70b-versatile, got %s", opts.Model)
	}

	if opts.BaseURL != "https://api.groq.com/openai/v1" {
		t.Errorf("expected default base URL https://api.groq.com/openai/v1, got %s", opts.BaseURL)
	}
}

func TestWithOptions(t *testing.T) {
	opts := apply(
		WithAPIKey("test-key"),
		WithModel("llama-3.1-8b-instant"),
		WithBaseURL("https://custom.groq.com/v1"),
	)

	if opts.APIKey != "test-key" {
		t.Errorf("expected API key test-key, got %s", opts.APIKey)
	}

	if opts.Model != "llama-3.1-8b-instant" {
		t.Errorf("expected model llama-3.1-8b-instant, got %s", opts.Model)
	}

	if opts.BaseURL != "https://custom.groq.com/v1" {
		t.Errorf("expected base URL https://custom.groq.com/v1, got %s", opts.BaseURL)
	}
}

func TestNewWithAPIKey(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderGroq {
		t.Errorf("expected provider groq, got %s", client.Provider())
	}

	if client.Model() != "llama-3.3-70b-versatile" {
		t.Errorf("expected model llama-3.3-70b-versatile, got %s", client.Model())
	}
}

func TestNewWithoutAPIKey(t *testing.T) {
	// Clear environment variables
	t.Setenv("GROQ_API_KEY", "")
	t.Setenv("LLM_API_KEY", "")

	_, err := New()
	if !errors.Is(err, llms.ErrMissingAPIKey) {
		t.Errorf("expected ErrMissingAPIKey, got %v", err)
	}
}

func TestClientImplementsInterfaces(t *testing.T) {
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test that client implements LLM interface
	var _ llms.LLM = client

	// Test that client implements CapableProvider interface
	var _ llms.CapableProvider = client

	// Test that client implements ModelLister interface
	var _ llms.ModelLister = client
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

	// Groq statically declares Vision:true (Llama vision models are supported).
	// The capability merge ensures this explicit declaration wins over the
	// conservative registry default (which reports Vision:false), so the provider
	// must report Vision:true.
	if !caps.Vision {
		t.Error("expected vision to be supported (static Vision:true must win over registry default)")
	}

	if caps.Embeddings {
		t.Error("expected embeddings to not be supported")
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
		{"llama-3.3-70b-versatile", "Meta"},
		{"mixtral-8x7b-32768", "Mistral AI"},
		{"gemma2-9b-it", "Google"},
		{"whisper-large-v3", "OpenAI"},
		{"unknown-model", "Groq"},
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
		{"llama-3.3-70b-versatile", []llms.ModelType{llms.ModelTypeChat}},
		{"llama-3.2-90b-vision-preview", []llms.ModelType{llms.ModelTypeChat, llms.ModelTypeVision}},
		{"whisper-large-v3", []llms.ModelType{llms.ModelTypeAudio}},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			result := inferModelTypes(tt.modelID)
			if len(result) != len(tt.expectedTypes) {
				t.Errorf("inferModelTypes(%s) returned %d types, want %d", tt.modelID, len(result), len(tt.expectedTypes))
				return
			}
			for i, mt := range result {
				if mt != tt.expectedTypes[i] {
					t.Errorf("inferModelTypes(%s)[%d] = %s, want %s", tt.modelID, i, mt, tt.expectedTypes[i])
				}
			}
		})
	}
}
