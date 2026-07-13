// Package cerebras provides a Cerebras LLM implementation using native HTTP.
// Cerebras uses an OpenAI-compatible API with wafer-scale inference.
package cerebras

import (
	llms "github.com/nocturnium/llm-go-sdk/v5"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/openaicompat"
)

// providerConfig defines Cerebras-specific configuration.
var providerConfig = openaicompat.ProviderConfig{
	Provider:              llms.ProviderCerebras,
	DefaultEmbeddingModel: "", // Cerebras doesn't have a public embedding API
	Capabilities: llms.Capabilities{
		Streaming:        true,
		Tools:            true,
		Vision:           false,
		Embeddings:       false,
		Batch:            false,
		JSONMode:         true,
		MaxContextTokens: 128000, // 128k context
		MaxOutputTokens:  8192,
	},
}

// Client is a Cerebras LLM client.
//
// Thread-safety: All methods are safe for concurrent use.
type Client struct {
	openaicompat.BaseProvider
	options *options
}

// New creates a new Cerebras client with the given options.
func New(opts ...Option) (*Client, error) {
	options := apply(opts...)

	// Resolve API key from options or environment
	apiKey, err := llms.RequireAPIKey("cerebras", options.APIKey, llms.EnvCerebrasAPIKey)
	if err != nil {
		return nil, err
	}
	options.APIKey = apiKey

	clientConfig := openaicompat.ClientConfig{
		BaseURL: options.BaseURL,
		APIKey:  options.APIKey,
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
