package openai

import (
	"net/http"
	"time"

	"github.com/nocturnium/llm-go-sdk/v2/pkg/openaicompat"
)

// Option is a function that configures an OpenAI client.
type Option func(*options)

// options contains configuration options for the OpenAI client.
type options struct {
	APIKey         string
	Model          string
	EmbeddingModel string
	BaseURL        string
	Organization   string
	HTTPClient     *http.Client
	Timeout        time.Duration
	// AllowPrivateIPs allows requests to private/loopback IPs and plain HTTP.
	// Off by default for SSRF safety; see WithAllowPrivateIPs.
	AllowPrivateIPs bool
	// AllowHTTP allows plain-HTTP (non-HTTPS) requests.
	AllowHTTP      bool
	ProviderConfig *openaicompat.ProviderConfig
	// responsesAPI routes non-streaming GenerateContent through the Responses API.
	responsesAPI bool
}

// defaultOptions returns the default options for OpenAI.
func defaultOptions() *options {
	return &options{
		Model: "gpt-4o",
	}
}

// WithProviderConfig sets a custom provider configuration for the OpenAI client.
func WithProviderConfig(config *openaicompat.ProviderConfig) Option {
	return func(o *options) {
		o.ProviderConfig = config
	}
}

// WithResponsesAPI routes non-streaming GenerateContent (and Call) through the
// OpenAI Responses API (POST /responses) instead of /chat/completions. Streaming
// continues to use the chat-completions endpoint. Pass server-side conversation
// state via call options: llms.WithExtraBodyParam("previous_response_id", id) and
// ("store", true).
func WithResponsesAPI() Option {
	return func(o *options) {
		o.responsesAPI = true
	}
}

// WithAPIKey sets the API key.
func WithAPIKey(key string) Option {
	return func(o *options) {
		o.APIKey = key
	}
}

// WithModel sets the construction-time default model for this OpenAI client.
// For a single-call override, pass llms.WithModel to GenerateContent, Stream,
// or llms.Call instead.
func WithModel(model string) Option {
	return func(o *options) {
		o.Model = model
	}
}

// WithBaseURL sets a custom base URL (useful for Azure or proxies).
func WithBaseURL(url string) Option {
	return func(o *options) {
		o.BaseURL = url
	}
}

// WithOrganization sets the OpenAI organization ID.
func WithOrganization(org string) Option {
	return func(o *options) {
		o.Organization = org
	}
}

// WithEmbeddingModel sets the default embedding model to use.
func WithEmbeddingModel(model string) Option {
	return func(o *options) {
		o.EmbeddingModel = model
	}
}

// WithHTTPClient sets a custom HTTP client for advanced configuration
// (timeouts, retries, custom transport, etc.)
func WithHTTPClient(client *http.Client) Option {
	return func(o *options) {
		o.HTTPClient = client
	}
}

// WithTimeout sets the HTTP client timeout for requests.
func WithTimeout(timeout time.Duration) Option {
	return func(o *options) {
		o.Timeout = timeout
	}
}

// WithAllowPrivateIPs allows requests to private/loopback IPs.
func WithAllowPrivateIPs() Option {
	return func(o *options) {
		o.AllowPrivateIPs = true
	}
}

// WithAllowHTTP allows plain-HTTP (non-HTTPS) requests.
func WithAllowHTTP() Option {
	return func(o *options) {
		o.AllowHTTP = true
	}
}

// apply applies the options to the default options.
func apply(opts ...Option) *options {
	options := defaultOptions()
	for _, opt := range opts {
		opt(options)
	}
	return options
}
