// Package togetherai provides a TogetherAI LLM implementation using native HTTP
// TogetherAI uses an OpenAI-compatible API
package togetherai

import (
	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/pkg/openaicompat"
)

// providerConfig defines TogetherAI-specific configuration
var providerConfig = openaicompat.ProviderConfig{
	Provider:              llms.ProviderTogetherAI,
	ProviderName:          "togetherai",
	DefaultEmbeddingModel: "togethercomputer/m2-bert-80M-8k-retrieval",
	Capabilities: llms.Capabilities{
		Streaming:        true,
		Tools:            true, // Depends on model
		Vision:           true, // Depends on model (Llama 3.2 Vision)
		Embeddings:       true,
		Batch:            false,
		JSONMode:         true,
		MaxContextTokens: 0, // Model dependent; use capability registry/model metadata
		MaxOutputTokens:  0, // Model dependent; use capability registry/model metadata
	},
}

// Client is a TogetherAI LLM client.
//
// Thread-safety: All methods are safe for concurrent use. The same client
// can be shared across multiple goroutines without additional synchronization.
type Client struct {
	openaicompat.BaseProvider
	options *options
}

// New creates a new TogetherAI client with the given options
func New(opts ...Option) (*Client, error) {
	options := apply(opts...)

	// Resolve API key from options or environment
	apiKey, err := llms.RequireAPIKey("togetherai", options.APIKey, llms.EnvTogetherAPIKey)
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

// Ensure Client implements the LLM interface
var _ llms.LLM = (*Client)(nil)

// Ensure Client implements the Embedder interface
var _ llms.Embedder = (*Client)(nil)

// Ensure Client implements the CapableProvider interface
var _ llms.CapableProvider = (*Client)(nil)
