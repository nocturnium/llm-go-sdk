// Package runpod provides a RunPod LLM implementation using native HTTP.
// RunPod uses OpenAI-compatible APIs for vLLM-based serverless endpoints.
package runpod

import (
	"errors"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v3"
	"github.com/nocturnium/llm-go-sdk/v3/pkg/openaicompat"
)

// ErrMissingEndpointID is returned when no endpoint ID is provided.
var ErrMissingEndpointID = errors.New("runpod: endpoint_id is required (use WithEndpointID)")

// providerConfig defines RunPod-specific configuration.
var providerConfig = openaicompat.ProviderConfig{
	Provider:              llms.ProviderRunPod,
	ProviderName:          "runpod",
	DefaultEmbeddingModel: "", // Depends on deployed model
	Capabilities: llms.Capabilities{
		Streaming:        true,
		Tools:            false, // Depends on deployed model
		Vision:           false, // Depends on deployed model
		Embeddings:       false, // Depends on deployed model
		Batch:            false,
		JSONMode:         true,
		MaxContextTokens: 0, // Deployment dependent; use capability registry/model metadata
		MaxOutputTokens:  0, // Deployment dependent; use capability registry/model metadata
	},
}

// Client is a RunPod LLM client.
//
// Thread-safety: All methods are safe for concurrent use. The same client
// can be shared across multiple goroutines without additional synchronization.
type Client struct {
	openaicompat.BaseProvider
	options *options
}

// New creates a new RunPod client with the given options.
// The endpoint ID is required and can be set via WithEndpointID.
func New(opts ...Option) (*Client, error) {
	options := apply(opts...)

	// Resolve API key from options or environment
	apiKey, err := llms.RequireAPIKey("runpod", options.APIKey, llms.EnvRunPodAPIKey)
	if err != nil {
		return nil, err
	}
	options.APIKey = apiKey

	// Endpoint ID is required
	if options.EndpointID == "" {
		return nil, ErrMissingEndpointID
	}

	// Build the base URL for OpenAI-compatible API
	// Format: https://api.runpod.ai/v2/<ENDPOINT_ID>/openai/v1
	baseURL := options.BaseURL
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	baseURL = baseURL + options.EndpointID + "/openai/v1"

	clientConfig := openaicompat.ClientConfig{
		BaseURL: baseURL,
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
