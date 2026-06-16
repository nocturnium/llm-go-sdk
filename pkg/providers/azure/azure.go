// Package azure provides an Azure OpenAI LLM implementation using native HTTP.
// Azure OpenAI uses an OpenAI-compatible API with custom deployments.
package azure

import (
	"errors"
	"net/url"
	"os"
	"path"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v2"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/openaicompat"
)

// Environment variable names for Azure OpenAI.
const (
	EnvAzureEndpoint   = "AZURE_OPENAI_ENDPOINT"
	EnvAzureDeployment = "AZURE_OPENAI_DEPLOYMENT"
)

// ErrMissingEndpoint is returned when no endpoint is provided.
var ErrMissingEndpoint = errors.New("azure: endpoint is required (set AZURE_OPENAI_ENDPOINT or use WithEndpoint)")

// ErrMissingDeployment is returned when no deployment is provided.
var ErrMissingDeployment = errors.New("azure: deployment name is required (set AZURE_OPENAI_DEPLOYMENT or use WithDeployment)")

// providerConfig defines Azure OpenAI-specific configuration.
var providerConfig = openaicompat.ProviderConfig{
	Provider:              llms.ProviderAzure,
	ProviderName:          "azure",
	DefaultEmbeddingModel: "", // Deployment dependent
	Capabilities: llms.Capabilities{
		Streaming:        true,
		Tools:            true,
		Vision:           true, // Model dependent
		Embeddings:       true, // Deployment dependent
		Batch:            false,
		JSONMode:         true,
		MaxContextTokens: 0, // Deployment dependent; use capability registry/model metadata
		MaxOutputTokens:  0, // Deployment dependent; use capability registry/model metadata
	},
}

// Client is an Azure OpenAI LLM client.
//
// Thread-safety: All methods are safe for concurrent use.
type Client struct {
	openaicompat.BaseProvider
	options *options
}

// New creates a new Azure OpenAI client with the given options.
func New(opts ...Option) (*Client, error) {
	options := apply(opts...)

	// Resolve API key from options or environment (supports both AZURE_OPENAI_API_KEY and AZURE_OPENAI_KEY)
	apiKey, err := llms.RequireAPIKey("azure", options.APIKey, llms.EnvAzureAPIKey, llms.EnvAzureKey)
	if err != nil {
		return nil, err
	}
	options.APIKey = apiKey

	// Resolve endpoint from options or environment
	if options.Endpoint == "" {
		options.Endpoint = os.Getenv(EnvAzureEndpoint)
	}
	if options.Endpoint == "" {
		return nil, ErrMissingEndpoint
	}

	// Resolve deployment name from options or environment
	if options.DeploymentName == "" {
		options.DeploymentName = os.Getenv(EnvAzureDeployment)
	}
	if options.DeploymentName == "" {
		return nil, ErrMissingDeployment
	}

	// Build the base URL for Azure OpenAI
	// Format: https://{resource}.openai.azure.com/openai/deployments/{deployment}
	baseURL := buildAzureBaseURL(options.Endpoint, options.DeploymentName, options.APIVersion)

	clientConfig := openaicompat.ClientConfig{
		BaseURL:      baseURL,
		APIKey:       options.APIKey,
		AzureAPIKey:  true, // Use api-key header instead of Authorization
		AzureVersion: options.APIVersion,
	}

	if options.HTTPClient != nil {
		clientConfig.HTTPClient = options.HTTPClient
	}
	clientConfig.Timeout = options.Timeout
	clientConfig.AllowPrivateIPs = options.AllowPrivateIPs
	clientConfig.AllowHTTP = options.AllowHTTP

	client := openaicompat.NewClient(clientConfig)
	cfg := providerConfig
	cfg.DefaultModel = options.DeploymentName
	if options.EmbeddingDeployment != "" {
		cfg.DefaultEmbeddingModel = options.EmbeddingDeployment
	}

	return &Client{
		BaseProvider: openaicompat.NewBaseProvider(client, cfg),
		options:      options,
	}, nil
}

// buildAzureBaseURL constructs the Azure OpenAI base URL.
func buildAzureBaseURL(endpoint, deployment, _ string) string {
	u, err := url.Parse(strings.TrimSuffix(endpoint, "/"))
	if err != nil {
		return strings.TrimSuffix(endpoint, "/") + "/openai/deployments/" + url.PathEscape(deployment)
	}
	u.Path = path.Join(strings.TrimRight(u.Path, "/"), "openai", "deployments", deployment)
	return u.String()
}

// Ensure Client implements the LLM interface.
var _ llms.LLM = (*Client)(nil)

// Ensure Client implements the Embedder interface.
var _ llms.Embedder = (*Client)(nil)

// Ensure Client implements the CapableProvider interface.
var _ llms.CapableProvider = (*Client)(nil)
