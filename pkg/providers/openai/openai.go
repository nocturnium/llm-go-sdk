// Package openai provides OpenAI chat and media clients using native HTTP.
// Media defaults are gpt-image-1.5, gpt-4o-mini-tts,
// gpt-4o-mini-transcribe, and sora-2. Override them with request options.
package openai

import (
	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

const defaultBaseURL = "https://api.openai.com/v1"

// defaultProviderConfig defines OpenAI-specific configuration.
var defaultProviderConfig = openaicompat.ProviderConfig{
	Provider:                  llms.ProviderOpenAI,
	Media:                     openaicompat.MediaCapabilities{Images: true, ImageEdits: true, Speech: true, SpeechStream: true, Transcription: true, Videos: true},
	DefaultImageModel:         "gpt-image-1.5",
	DefaultSpeechModel:        "gpt-4o-mini-tts",
	DefaultTranscriptionModel: "gpt-4o-mini-transcribe",
	DefaultVideoModel:         "sora-2",
	DefaultEmbeddingModel:     "text-embedding-3-small",
	Capabilities: llms.Capabilities{
		Streaming:        true,
		Tools:            true,
		Vision:           true, // GPT-4 vision models support images
		Embeddings:       true,
		Batch:            false, // OpenAI batch API is separate
		JSONMode:         true,
		MaxContextTokens: 128000, // GPT-4o context
		MaxOutputTokens:  16384,  // GPT-4o max output
	},
}

// Client is an OpenAI LLM client.
//
// Thread-safety: All methods are safe for concurrent use. The same client
// can be shared across multiple goroutines without additional synchronization.
type Client struct {
	openaicompat.BaseProvider
	options *options
}

// New creates a new OpenAI client with the given options.
func New(opts ...Option) (*Client, error) {
	options := apply(opts...)

	// Resolve API key from options or environment
	apiKey, err := llms.RequireAPIKey("openai", options.APIKey, llms.EnvOpenAIAPIKey)
	if err != nil {
		return nil, err
	}
	options.APIKey = apiKey

	baseURL := options.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	headers := make(map[string]string)
	if options.Organization != "" {
		headers["OpenAI-Organization"] = options.Organization
	}

	clientConfig := openaicompat.ClientConfig{
		BaseURL: baseURL,
		APIKey:  options.APIKey,
		Headers: headers,
	}

	if options.HTTPClient != nil {
		clientConfig.HTTPClient = options.HTTPClient
	}
	clientConfig.Timeout = options.Timeout
	clientConfig.AllowPrivateIPs = options.AllowPrivateIPs
	clientConfig.AllowHTTP = options.AllowHTTP

	client := openaicompat.NewClient(clientConfig)

	if options.ProviderConfig == nil {
		options.ProviderConfig = &defaultProviderConfig
	}
	providerConfig := *options.ProviderConfig
	providerConfig.DefaultModel = options.Model
	if options.EmbeddingModel != "" {
		providerConfig.DefaultEmbeddingModel = options.EmbeddingModel
	}
	if options.responsesAPI {
		providerConfig.UseResponsesAPI = true
	}

	return &Client{
		BaseProvider: openaicompat.NewBaseProvider(client, providerConfig),
		options:      options,
	}, nil
}

// Ensure Client implements the LLM interface.
var _ llms.LLM = (*Client)(nil)

// Ensure Client implements the Embedder interface.
var _ llms.Embedder = (*Client)(nil)

// Ensure Client implements the CapableProvider interface.
var _ llms.CapableProvider = (*Client)(nil)
