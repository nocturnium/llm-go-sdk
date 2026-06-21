// Package perplexity provides a Perplexity AI LLM implementation using native HTTP.
// Perplexity uses an OpenAI-compatible API with search-augmented generation.
package perplexity

import (
	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/openaicompat"
)

// providerConfig defines Perplexity-specific configuration.
var providerConfig = openaicompat.ProviderConfig{
	Provider:              llms.ProviderPerplexity,
	ProviderName:          "perplexity",
	DefaultEmbeddingModel: "", // Perplexity doesn't support embeddings
	Capabilities: llms.Capabilities{
		Streaming:        true,
		Tools:            false, // Perplexity doesn't support tool calling
		Vision:           false,
		Embeddings:       false,
		Batch:            false,
		JSONMode:         true,
		MaxContextTokens: 127072, // 128k context
		MaxOutputTokens:  4096,
	},
}

// Client is a Perplexity AI LLM client.
//
// Thread-safety: All methods are safe for concurrent use.
type Client struct {
	openaicompat.BaseProvider
	options *options
}

// New creates a new Perplexity client with the given options.
func New(opts ...Option) (*Client, error) {
	options := apply(opts...)

	// Resolve API key from options or environment (supports both PERPLEXITY_API_KEY and PPLX_API_KEY)
	apiKey, err := llms.RequireAPIKey("perplexity", options.APIKey, llms.EnvPerplexityAPIKey, llms.EnvPPLXAPIKey)
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
