// Package groq provides a Groq LLM implementation using native HTTP.
// Groq uses an OpenAI-compatible API with ultra-fast inference.
package groq

import (
	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/pkg/openaicompat"
)

// providerConfig defines Groq-specific configuration.
var providerConfig = openaicompat.ProviderConfig{
	Provider:              llms.ProviderGroq,
	DefaultEmbeddingModel: "", // Groq doesn't support embeddings yet
	Capabilities: llms.Capabilities{
		Streaming:        true,
		Tools:            true,
		Vision:           true,  // Llama 3.2 Vision models supported
		Embeddings:       false, // Not supported yet
		Batch:            false,
		JSONMode:         true,
		MaxContextTokens: 0, // Model dependent; use capability registry/model metadata
		MaxOutputTokens:  0, // Model dependent; use capability registry/model metadata
	},
}

// Client is a Groq LLM client.
//
// Thread-safety: All methods are safe for concurrent use. The same client
// can be shared across multiple goroutines without additional synchronization.
type Client struct {
	openaicompat.BaseProvider
	options *options
}

// New creates a new Groq client with the given options.
func New(opts ...Option) (*Client, error) {
	options := apply(opts...)

	// Resolve API key from options or environment
	apiKey, err := llms.RequireAPIKey("groq", options.APIKey, llms.EnvGroqAPIKey)
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
