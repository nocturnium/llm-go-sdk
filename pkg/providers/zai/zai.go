// Package zai provides a Z.AI LLM implementation using native HTTP.
// Z.AI uses OpenAI-compatible APIs for their GLM models.
package zai

import (
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v5"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/openaicompat"
)

// providerConfig defines Z.AI-specific configuration.
var providerConfig = openaicompat.ProviderConfig{
	Provider:              llms.ProviderZAI,
	DefaultEmbeddingModel: "", // Z.AI does not provide embeddings in the documented API
	Capabilities: llms.Capabilities{
		Streaming:        true,
		Tools:            true,
		Vision:           true,
		Embeddings:       false,
		Batch:            false,
		JSONMode:         true,
		MaxContextTokens: 200000, // 200K context length
		MaxOutputTokens:  128000, // 128K max tokens
	},
}

// Client is a Z.AI LLM client.
//
// Thread-safety: All methods are safe for concurrent use. The same client
// can be shared across multiple goroutines without additional synchronization.
type Client struct {
	openaicompat.BaseProvider
	options *options
}

// New creates a new Z.AI client with the given options.
func New(opts ...Option) (*Client, error) {
	options := apply(opts...)

	// Resolve API key from options or environment
	apiKey, err := llms.RequireAPIKey("zai", options.APIKey, llms.EnvZAIAPIKey)
	if err != nil {
		return nil, err
	}
	options.APIKey = apiKey

	// Build the base URL for OpenAI-compatible API
	// Format: https://api.z.ai/api/paas/v4 (general) or https://api.z.ai/api/coding/paas/v4 (coding)
	baseURL := options.BaseURL

	// If UseCodingAPI is enabled and the user hasn't provided a custom BaseURL,
	// switch to the coding endpoint
	if options.UseCodingAPI && baseURL == "https://api.z.ai/api/paas/v4" {
		baseURL = "https://api.z.ai/api/coding/paas/v4"
	}

	baseURL = strings.TrimSuffix(baseURL, "/")

	clientConfig := openaicompat.ClientConfig{
		BaseURL: baseURL,
		APIKey:  options.APIKey,
		Headers: map[string]string{
			"Accept-Language": "en-US,en", // Required by Z.AI API
		},
	}

	if options.HTTPClient != nil {
		clientConfig.HTTPClient = options.HTTPClient
	}
	clientConfig.Timeout = options.Timeout
	clientConfig.AllowPrivateIPs = options.AllowPrivateIPs
	clientConfig.AllowHTTP = options.AllowHTTP

	client := openaicompat.NewClient(clientConfig)
	cfg := providerConfig
	cfg.DefaultModel = options.Model
	if options.EmbeddingModel != "" {
		cfg.DefaultEmbeddingModel = options.EmbeddingModel
	}

	return &Client{
		BaseProvider: openaicompat.NewBaseProvider(client, cfg),
		options:      options,
	}, nil
}

// Ensure Client implements the LLM interface.
var _ llms.LLM = (*Client)(nil)

// Ensure Client implements the CapableProvider interface.
var _ llms.CapableProvider = (*Client)(nil)

// Ensure Client implements the ModelLister interface.
var _ llms.ModelLister = (*Client)(nil)
