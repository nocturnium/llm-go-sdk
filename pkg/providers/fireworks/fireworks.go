// Package fireworks provides a Fireworks AI LLM implementation using native HTTP.
// Fireworks AI uses an OpenAI-compatible API with fast inference.
package fireworks

import (
	llms "github.com/nocturnium/llm-go-sdk/v5"
	"github.com/nocturnium/llm-go-sdk/v5/pkg/openaicompat"
)

// providerConfig defines Fireworks-specific configuration.
var providerConfig = openaicompat.ProviderConfig{
	Provider:              llms.ProviderFireworks,
	DefaultEmbeddingModel: "nomic-ai/nomic-embed-text-v1.5",
	Capabilities: llms.Capabilities{
		Streaming:        true,
		Tools:            true,
		Vision:           true, // Llama 3.2 Vision supported
		Embeddings:       true, // Supported
		Batch:            false,
		JSONMode:         true,
		MaxContextTokens: 0, // Model dependent; use capability registry/model metadata
		MaxOutputTokens:  0, // Model dependent; use capability registry/model metadata
	},
}

// Client is a Fireworks AI LLM client.
//
// Thread-safety: All methods are safe for concurrent use.
type Client struct {
	openaicompat.BaseProvider
	options *options
}

// New creates a new Fireworks client with the given options.
func New(opts ...Option) (*Client, error) {
	options := apply(opts...)

	// Resolve API key from options or environment
	apiKey, err := llms.RequireAPIKey("fireworks", options.APIKey, llms.EnvFireworksAPIKey)
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

// Ensure Client implements the Embedder interface.
var _ llms.Embedder = (*Client)(nil)

// Ensure Client implements the CapableProvider interface.
var _ llms.CapableProvider = (*Client)(nil)
