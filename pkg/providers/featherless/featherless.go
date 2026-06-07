// Package featherless provides a Featherless.ai LLM implementation using native HTTP
// Featherless.ai provides an OpenAI-compatible API with access to thousands of open-source models
package featherless

import (
	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/pkg/openaicompat"
)

// providerConfig defines Featherless-specific configuration.
var providerConfig = openaicompat.ProviderConfig{
	Provider:              llms.ProviderFeatherless,
	ProviderName:          "featherless",
	DefaultEmbeddingModel: "", // Featherless doesn't support embeddings by default
	Capabilities: llms.Capabilities{
		Streaming:        true,
		Tools:            true,  // Depends on model
		Vision:           false, // Most Featherless models don't support vision
		Embeddings:       false,
		Batch:            false,
		JSONMode:         true,
		MaxContextTokens: 0, // Model dependent; use capability registry/model metadata
		MaxOutputTokens:  0, // Model dependent; use capability registry/model metadata
	},
}

// Client is a Featherless LLM client.
//
// Thread-safety: All methods are safe for concurrent use. The same client
// can be shared across multiple goroutines without additional synchronization.
type Client struct {
	openaicompat.BaseProvider
	options *options
}

// New creates a new Featherless client with the given options.
func New(opts ...Option) (*Client, error) {
	options := apply(opts...)

	// Resolve API key from options or environment
	apiKey, err := llms.RequireAPIKey("featherless", options.APIKey, llms.EnvFeatherlessAPIKey)
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
