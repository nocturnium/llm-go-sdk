package infinity

import (
	"net/http"
	"time"
)

// Option is a function that configures an Infinity client.
type Option func(*options)

// options contains configuration options for the Infinity client.
type options struct {
	APIKey         string
	EmbeddingModel string
	RerankModel    string
	BaseURL        string
	HTTPClient     *http.Client
	Timeout        time.Duration
	RunPodMode     bool // Use RunPod serverless API format
	// AllowPrivateIPs allows requests to private/loopback IPs.
	// Defaults to true for Infinity since it targets http://localhost.
	AllowPrivateIPs bool
	// AllowHTTP allows plain-HTTP (non-HTTPS) requests.
	AllowHTTP bool
}

// defaultOptions returns the default options for Infinity.
func defaultOptions() *options {
	return &options{
		EmbeddingModel: "michaelfeil/bge-small-en-v1.5",
		RerankModel:    "mixedbread-ai/mxbai-rerank-xsmall-v1",
		BaseURL:        "http://localhost:7997/v1",
		// Infinity runs locally over plain HTTP by default, so relax SSRF
		// validation out of the box.
		AllowPrivateIPs: true,
		AllowHTTP:       true,
	}
}

// WithAPIKey sets the API key for Infinity (optional for local deployments).
func WithAPIKey(key string) Option {
	return func(o *options) {
		o.APIKey = key
	}
}

// WithEmbeddingModel sets the default embedding model.
func WithEmbeddingModel(model string) Option {
	return func(o *options) {
		o.EmbeddingModel = model
	}
}

// WithRerankModel sets the default reranking model.
func WithRerankModel(model string) Option {
	return func(o *options) {
		o.RerankModel = model
	}
}

// WithBaseURL sets a custom base URL for the Infinity server.
func WithBaseURL(url string) Option {
	return func(o *options) {
		o.BaseURL = url
	}
}

// WithHTTPClient sets a custom HTTP client for advanced configuration.
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

// WithRunPodMode enables RunPod serverless API format.
// RunPod wraps the OpenAI-compatible API with /runsync endpoint
// and requires specific request/response formatting.
func WithRunPodMode() Option {
	return func(o *options) {
		o.RunPodMode = true
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
