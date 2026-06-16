// Package synthetic provides a Synthetic.new LLM implementation using native HTTP
// Synthetic.new is a privacy-focused AI platform with an OpenAI-compatible API
// offering access to open-source coding LLMs like Qwen, GLM, Kimi, and DeepSeek
package synthetic

import (
	llms "github.com/nocturnium/llm-go-sdk/v2"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/openaicompat"
)

// providerConfig defines Synthetic-specific configuration
var providerConfig = openaicompat.ProviderConfig{
	Provider:              llms.ProviderSynthetic,
	ProviderName:          "synthetic",
	DefaultEmbeddingModel: "", // Set if embeddings are supported
	Capabilities: llms.Capabilities{
		Streaming:        true,
		Tools:            true,
		Vision:           true,
		Embeddings:       false,
		Batch:            false,
		JSONMode:         true,
		MaxContextTokens: 0, // Model dependent; use capability registry/model metadata
		MaxOutputTokens:  0, // Model dependent; use capability registry/model metadata
	},
}

// Client is a Synthetic LLM client.
//
// Thread-safety: All methods are safe for concurrent use. The same client
// can be shared across multiple goroutines without additional synchronization.
type Client struct {
	openaicompat.BaseProvider
	options *options
}

// New creates a new Synthetic client with the given options
func New(opts ...Option) (*Client, error) {
	options := apply(opts...)

	// Resolve API key from options or environment
	apiKey, err := llms.RequireAPIKey("synthetic", options.APIKey, llms.EnvSyntheticAPIKey)
	if err != nil {
		return nil, err
	}
	options.APIKey = apiKey

	clientConfig := openaicompat.ClientConfig{
		BaseURL: options.BaseURL,
		APIKey:  options.APIKey,
	}

	// apply custom HTTP client if provided
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
