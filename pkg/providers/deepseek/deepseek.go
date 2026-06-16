// Package deepseek provides a DeepSeek LLM implementation using native HTTP.
// DeepSeek uses an OpenAI-compatible API.
package deepseek

import (
	llms "github.com/nocturnium/llm-go-sdk/v2"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/openaicompat"
)

// providerConfig defines DeepSeek-specific configuration.
var providerConfig = openaicompat.ProviderConfig{
	Provider:              llms.ProviderDeepSeek,
	ProviderName:          "deepseek",
	DefaultEmbeddingModel: "", // DeepSeek doesn't have a public embedding API
	Capabilities: llms.Capabilities{
		Streaming:        true,
		Tools:            true,
		Vision:           false, // Not supported yet
		Embeddings:       false,
		Batch:            false,
		JSONMode:         true,
		MaxContextTokens: 128000, // DeepSeek V3/R1 context window
		MaxOutputTokens:  8192,
	},
}

// Client is a DeepSeek LLM client.
//
// Thread-safety: All methods are safe for concurrent use.
type Client struct {
	openaicompat.BaseProvider
	options *options
}

// New creates a new DeepSeek client with the given options.
func New(opts ...Option) (*Client, error) {
	options := apply(opts...)

	// Resolve API key from options or environment
	apiKey, err := llms.RequireAPIKey("deepseek", options.APIKey, llms.EnvDeepSeekAPIKey)
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
