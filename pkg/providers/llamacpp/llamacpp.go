// Package llamacpp provides a llama.cpp LLM client using native HTTP.
package llamacpp

import (
	"context"
	"os"
	"strings"
	"sync"

	llms "github.com/nocturnium/llm-go-sdk/v2"
	"github.com/nocturnium/llm-go-sdk/v2/internal/llamacppapi"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/openaicompat"
)

// providerConfig defines llama.cpp-specific configuration.
var providerConfig = openaicompat.ProviderConfig{
	Provider:              llms.ProviderLlamaCpp,
	ProviderName:          "llamacpp",
	DefaultEmbeddingModel: "", // Model-dependent
	Capabilities: llms.Capabilities{
		Streaming:        true,
		Tools:            true,
		Vision:           true, // Model-dependent (LLaVA, etc.)
		Embeddings:       true, // Model-dependent
		Batch:            false,
		JSONMode:         true, // Grammar-based JSON
		MaxContextTokens: 0,    // Discovered from /props
		MaxOutputTokens:  0,    // Discovered from /props
	},
}

// Client is a llama.cpp LLM client.
//
// Thread-safety: All methods are safe for concurrent use.
type Client struct {
	openaicompat.BaseProvider
	nativeClient *llamacppapi.Client
	options      *options

	// Lazy-loaded model properties
	propsOnce sync.Once
	props     *llamacppapi.PropsResponse
	propsErr  error
}

// New creates a new llama.cpp client with the given options.
func New(opts ...Option) (*Client, error) {
	options := apply(opts...)

	// Check for LLAMA_CPP_HOST environment variable
	if options.BaseURL == "http://localhost:8080" {
		if host := os.Getenv("LLAMA_CPP_HOST"); host != "" {
			options.BaseURL = normalizeURL(host)
		}
	}

	// Resolve API key from environment (optional)
	options.APIKey = llms.ResolveAPIKey(options.APIKey, "LLAMA_CPP_API_KEY")

	// Create OpenAI-compatible client (uses /v1 prefix)
	clientConfig := openaicompat.ClientConfig{
		BaseURL: ensureV1Suffix(options.BaseURL),
		APIKey:  options.APIKey,
	}

	if options.HTTPClient != nil {
		clientConfig.HTTPClient = options.HTTPClient
	}
	clientConfig.Timeout = options.Timeout
	clientConfig.AllowPrivateIPs = options.AllowPrivateIPs
	clientConfig.AllowHTTP = options.AllowHTTP

	client := openaicompat.NewClient(clientConfig)

	// Create native API client for /props, /slots, /health
	nativeConfig := llamacppapi.ClientConfig{
		BaseURL: options.BaseURL,
		APIKey:  options.APIKey,
	}
	if options.HTTPClient != nil {
		nativeConfig.HTTPClient = options.HTTPClient
	}
	nativeConfig.Timeout = options.Timeout
	nativeConfig.AllowPrivateIPs = options.AllowPrivateIPs
	nativeConfig.AllowHTTP = options.AllowHTTP
	nativeClient := llamacppapi.NewClient(nativeConfig)
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

// normalizeURL ensures the URL is properly formatted.
func normalizeURL(url string) string {
	url = strings.TrimSuffix(url, "/")
	url = strings.TrimSuffix(url, "/v1")
	return url
}

// ensureV1Suffix adds /v1 suffix for OpenAI-compatible endpoints.
func ensureV1Suffix(url string) string {
	url = strings.TrimSuffix(url, "/")
	if !strings.HasSuffix(url, "/v1") {
		url += "/v1"
	}
	return url
}

// Model returns the model name, discovering it lazily if needed.
func (c *Client) Model() string {
	if c.options.Model != "" {
		return c.options.Model
	}

	// Try to discover model from /props
	props, err := c.getPropsLazy(context.Background())
	if err == nil && props.DefaultGenerationSettings.Model != "" {
		return props.DefaultGenerationSettings.Model
	}

	return "default"
}

// Capabilities returns the provider's capabilities, enriched with /props data.
func (c *Client) Capabilities() llms.Capabilities {
	caps := providerConfig.Capabilities

	// Try to enrich with /props data
	props, err := c.getPropsLazy(context.Background())
	if err == nil {
		caps.MaxContextTokens = props.DefaultGenerationSettings.NCtx
		caps.MaxOutputTokens = props.DefaultGenerationSettings.NPredict
	}

	return caps
}

// getPropsLazy fetches /props once and caches the result.
func (c *Client) getPropsLazy(ctx context.Context) (*llamacppapi.PropsResponse, error) {
	c.propsOnce.Do(func() {
		c.props, c.propsErr = c.nativeClient.GetProps(ctx)
	})
	return c.props, c.propsErr
}

// Ensure Client implements the LLM interface.
var _ llms.LLM = (*Client)(nil)

// Ensure Client implements the Embedder interface.
var _ llms.Embedder = (*Client)(nil)

// Ensure Client implements the CapableProvider interface.
var _ llms.CapableProvider = (*Client)(nil)
