// Package togetherai provides a TogetherAI LLM implementation using native HTTP
// TogetherAI uses an OpenAI-compatible API
// Media defaults: black-forest-labs/FLUX.1-schnell, hexgrad/Kokoro-82M, openai/whisper-large-v3, ByteDance/Seedance-2.5.
// Override media models with per-request options. Media routes are documentation-verified only.
package togetherai

import (
	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

// providerConfig defines TogetherAI-specific configuration
var providerConfig = openaicompat.ProviderConfig{
	Media:                     openaicompat.MediaCapabilities{Images: true, Speech: true, SpeechStream: true, Transcription: true, Videos: true, VideosPath: "/v2/videos"},
	DefaultImageModel:         "black-forest-labs/FLUX.1-schnell",
	DefaultSpeechModel:        "hexgrad/Kokoro-82M",
	DefaultTranscriptionModel: "openai/whisper-large-v3",
	// Verified in https://docs.together.ai/docs/serverless/models and the video guide, 2026-09-05.
	DefaultVideoModel: "ByteDance/Seedance-2.5",

	Provider:              llms.ProviderTogetherAI,
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
	options   *options
	mediaHTTP *httpclient.Client
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
		mediaHTTP:    newMediaHTTP(options),
	}, nil
}

// Ensure Client implements the LLM interface
var _ llms.LLM = (*Client)(nil)

// Ensure Client implements the Embedder interface
var _ llms.Embedder = (*Client)(nil)

// Ensure Client implements the CapableProvider interface
var _ llms.CapableProvider = (*Client)(nil)

var (
	_ llms.SpeechSynthesizer = (*Client)(nil)
	_ llms.Transcriber       = (*Client)(nil)
	_ llms.ImageGenerator    = (*Client)(nil)
	_ llms.VideoGenerator    = (*Client)(nil)
)
