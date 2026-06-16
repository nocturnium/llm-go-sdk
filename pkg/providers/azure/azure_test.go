package azure

import (
	"errors"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v2"
)

func TestDefaultOptions(t *testing.T) {
	opts := defaultOptions()

	if opts.APIVersion != "2024-02-15-preview" {
		t.Errorf("unexpected default API version: %s", opts.APIVersion)
	}
}

func TestWithOptions(t *testing.T) {
	opts := apply(
		WithAPIKey("test-key"),
		WithEndpoint("https://myresource.openai.azure.com"),
		WithDeployment("gpt-4"),
		WithAPIVersion("2024-06-01"),
	)

	if opts.APIKey != "test-key" {
		t.Errorf("expected API key test-key, got %s", opts.APIKey)
	}

	if opts.Endpoint != "https://myresource.openai.azure.com" {
		t.Errorf("unexpected endpoint: %s", opts.Endpoint)
	}

	if opts.DeploymentName != "gpt-4" {
		t.Errorf("unexpected deployment: %s", opts.DeploymentName)
	}

	if opts.APIVersion != "2024-06-01" {
		t.Errorf("unexpected API version: %s", opts.APIVersion)
	}
}

func TestNewWithAllOptions(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithEndpoint("https://myresource.openai.azure.com"),
		WithDeployment("gpt-4"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.Provider() != llms.ProviderAzure {
		t.Errorf("expected provider azure, got %s", client.Provider())
	}
}

func TestNewWithoutAPIKey(t *testing.T) {
	t.Setenv("AZURE_OPENAI_API_KEY", "")
	t.Setenv("AZURE_OPENAI_KEY", "")
	t.Setenv("LLM_API_KEY", "")

	_, err := New(
		WithEndpoint("https://myresource.openai.azure.com"),
		WithDeployment("gpt-4"),
	)
	if !errors.Is(err, llms.ErrMissingAPIKey) {
		t.Errorf("expected ErrMissingAPIKey, got %v", err)
	}
}

func TestNewWithoutEndpoint(t *testing.T) {
	t.Setenv("AZURE_OPENAI_ENDPOINT", "")

	_, err := New(
		WithAPIKey("test-key"),
		WithDeployment("gpt-4"),
	)
	if !errors.Is(err, ErrMissingEndpoint) {
		t.Errorf("expected ErrMissingEndpoint, got %v", err)
	}
}

func TestNewWithoutDeployment(t *testing.T) {
	t.Setenv("AZURE_OPENAI_DEPLOYMENT", "")

	_, err := New(
		WithAPIKey("test-key"),
		WithEndpoint("https://myresource.openai.azure.com"),
	)
	if !errors.Is(err, ErrMissingDeployment) {
		t.Errorf("expected ErrMissingDeployment, got %v", err)
	}
}

func TestBuildAzureBaseURL(t *testing.T) {
	tests := []struct {
		endpoint   string
		deployment string
		version    string
		expected   string
	}{
		{
			endpoint:   "https://myresource.openai.azure.com",
			deployment: "gpt-4",
			version:    "2024-02-15-preview",
			expected:   "https://myresource.openai.azure.com/openai/deployments/gpt-4",
		},
		{
			endpoint:   "https://myresource.openai.azure.com/",
			deployment: "my-deployment",
			version:    "2024-02-15-preview",
			expected:   "https://myresource.openai.azure.com/openai/deployments/my-deployment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.deployment, func(t *testing.T) {
			result := buildAzureBaseURL(tt.endpoint, tt.deployment, tt.version)
			if result != tt.expected {
				t.Errorf("buildAzureBaseURL() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestCapabilities(t *testing.T) {
	client, err := New(
		WithAPIKey("test-key"),
		WithEndpoint("https://myresource.openai.azure.com"),
		WithDeployment("gpt-4"),
	)
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
