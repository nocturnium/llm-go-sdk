// Package ollama provides a local Ollama LLM implementation using native HTTP.
// Ollama uses an OpenAI-compatible API for local inference.
package ollama

import (
	"os"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/internal/ollamaapi"
	"github.com/nocturnium/llm-go-sdk/pkg/openaicompat"
)

// providerConfig defines Ollama-specific configuration.
var providerConfig = openaicompat.ProviderConfig{
	Provider:              llms.ProviderOllama,
	ProviderName:          "ollama",
	DefaultEmbeddingModel: "nomic-embed-text",
	Capabilities: llms.Capabilities{
		Streaming:        true,
		Tools:            false, // Model dependent
		Vision:           false, // Model dependent (llava, etc.)
		Embeddings:       true,  // nomic-embed-text, etc.
		Batch:            false,
		JSONMode:         true,
		MaxContextTokens: 0, // Local model dependent; discovered from metadata when available
		MaxOutputTokens:  0,
	},
}

// Client is an Ollama LLM client.
//
// Thread-safety: All methods are safe for concurrent use.
type Client struct {
	openaicompat.BaseProvider
	nativeClient *ollamaapi.Client
	options      *options
}

// New creates a new Ollama client with the given options.
func New(opts ...Option) (*Client, error) {
	options := apply(opts...)

	// Check for OLLAMA_HOST environment variable
	if options.BaseURL == "http://localhost:11434/v1" {
		if host := os.Getenv("OLLAMA_HOST"); host != "" {
			// Ensure the URL ends with /v1 for OpenAI compatibility
			if !strings.HasSuffix(host, "/v1") {
				host = strings.TrimSuffix(host, "/") + "/v1"
			}
			options.BaseURL = host
		}
	}

	// Resolve API key from environment if not provided (optional for local Ollama)
	options.APIKey = llms.ResolveAPIKey(options.APIKey, "OLLAMA_API_KEY")

	clientConfig := openaicompat.ClientConfig{
		BaseURL: options.BaseURL,
		APIKey:  options.APIKey, // May be empty for local Ollama
	}

	if options.HTTPClient != nil {
		clientConfig.HTTPClient = options.HTTPClient
	}
	clientConfig.Timeout = options.Timeout
	clientConfig.AllowPrivateIPs = options.AllowPrivateIPs
	clientConfig.AllowHTTP = options.AllowHTTP

	client := openaicompat.NewClient(clientConfig)

	// Create native API client for model management
	nativeConfig := ollamaapi.ClientConfig{
		BaseURL: options.BaseURL,
	}
	if options.HTTPClient != nil {
		nativeConfig.HTTPClient = options.HTTPClient
	}
	nativeConfig.Timeout = options.Timeout
	nativeConfig.AllowPrivateIPs = options.AllowPrivateIPs
	nativeConfig.AllowHTTP = options.AllowHTTP
	nativeClient := ollamaapi.NewClient(nativeConfig)
	cfg := providerConfig
	cfg.DefaultModel = options.Model
	if options.EmbeddingModel != "" {
		cfg.DefaultEmbeddingModel = options.EmbeddingModel
	}

	return &Client{
		BaseProvider: openaicompat.NewBaseProvider(client, cfg),
		nativeClient: nativeClient,
		options:      options,
	}, nil
}

// Ensure Client implements the LLM interface.
var _ llms.LLM = (*Client)(nil)

// Ensure Client implements the Embedder interface.
var _ llms.Embedder = (*Client)(nil)

// Ensure Client implements the CapableProvider interface.
var _ llms.CapableProvider = (*Client)(nil)
