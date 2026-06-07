// Package mistral provides a Mistral AI LLM implementation using native HTTP.
// Mistral AI uses an OpenAI-compatible API.
package mistral

import (
	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/pkg/openaicompat"
)

// providerConfig defines Mistral-specific configuration.
var providerConfig = openaicompat.ProviderConfig{
	Provider:              llms.ProviderMistral,
	ProviderName:          "mistral",
	DefaultEmbeddingModel: "mistral-embed",
	Capabilities: llms.Capabilities{
		Streaming:        true,
		Tools:            true,
		Vision:           true, // Pixtral models support vision
		Embeddings:       true, // mistral-embed
		Batch:            false,
		JSONMode:         true,
		MaxContextTokens: 131072, // 128k for latest models
		MaxOutputTokens:  8192,
	},
}

// Client is a Mistral AI LLM client.
//
// Thread-safety: All methods are safe for concurrent use.
type Client struct {
	openaicompat.BaseProvider
	options *options
}

// New creates a new Mistral client with the given options.
func New(opts ...Option) (*Client, error) {
	options := apply(opts...)

	// Resolve API key from options or environment
	apiKey, err := llms.RequireAPIKey("mistral", options.APIKey, llms.EnvMistralAPIKey)
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
