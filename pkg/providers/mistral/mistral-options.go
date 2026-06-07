package mistral

import (
	"net/http"
	"time"
)

// Option is a function that configures a Mistral client.
type Option func(*options)

// options contains configuration options for the Mistral client.
type options struct {
	APIKey         string
	Model          string
	EmbeddingModel string
	BaseURL        string
	HTTPClient     *http.Client
	Timeout        time.Duration
	// AllowPrivateIPs allows requests to private/loopback IPs and plain HTTP.
	// Off by default for SSRF safety; see WithAllowPrivateIPs.
	AllowPrivateIPs bool
	// AllowHTTP allows plain-HTTP (non-HTTPS) requests.
	AllowHTTP bool
}

// defaultOptions returns the default options for Mistral.
func defaultOptions() *options {
	return &options{
		Model:          "mistral-large-latest",
		EmbeddingModel: "mistral-embed",
		BaseURL:        "https://api.mistral.ai/v1",
	}
}

// WithAPIKey sets the API key.
func WithAPIKey(key string) Option {
	return func(o *options) {
		o.APIKey = key
	}
}

// WithModel sets the model to use.
func WithModel(model string) Option {
	return func(o *options) {
		o.Model = model
	}
}

// WithBaseURL sets a custom base URL.
func WithBaseURL(url string) Option {
	return func(o *options) {
		o.BaseURL = url
	}
}

// WithEmbeddingModel sets the default embedding model to use.
func WithEmbeddingModel(model string) Option {
	return func(o *options) {
		o.EmbeddingModel = model
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
