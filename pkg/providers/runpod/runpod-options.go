package runpod

import (
	"net/http"
	"time"
)

// Option is a function that configures a RunPod client.
type Option func(*options)

// options contains configuration options for the RunPod client.
type options struct {
	APIKey         string
	EndpointID     string // Required: The RunPod serverless endpoint ID
	Model          string
	EmbeddingModel string
	BaseURL        string // Override base URL (default: https://api.runpod.ai/v2)
	HTTPClient     *http.Client
	Timeout        time.Duration
	// AllowPrivateIPs allows requests to private/loopback IPs and plain HTTP.
	// Off by default for SSRF safety; see WithAllowPrivateIPs.
	AllowPrivateIPs bool
	// AllowHTTP allows plain-HTTP (non-HTTPS) requests.
	AllowHTTP bool
}

// defaultOptions returns the default options for RunPod.
func defaultOptions() *options {
	return &options{
		BaseURL: "https://api.runpod.ai/v2",
	}
}

// WithAPIKey sets the API key.
func WithAPIKey(key string) Option {
	return func(o *options) {
		o.APIKey = key
	}
}

// WithEndpointID sets the RunPod serverless endpoint ID.
// This is required for making API calls.
func WithEndpointID(endpointID string) Option {
	return func(o *options) {
		o.EndpointID = endpointID
	}
}

// WithModel sets the model to use.
// For vLLM endpoints, this should match the MODEL_NAME environment variable.
func WithModel(model string) Option {
	return func(o *options) {
		o.Model = model
	}
}

// WithBaseURL sets a custom base URL.
// Default is https://api.runpod.ai/v2
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

// WithHTTPClient sets a custom HTTP client for advanced configuration
// (timeouts, retries, custom transport, etc.).
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
