package anthropic

import (
	"net/http"
	"time"
)

// Option is a function that configures an Anthropic client.
type Option func(*options)

// options contains configuration options for the Anthropic client.
type options struct {
	APIKey     string
	Model      string
	BaseURL    string
	HTTPClient *http.Client
	Timeout    time.Duration
	// AllowPrivateIPs allows requests to private/loopback IPs and plain HTTP.
	// Off by default for SSRF safety; see WithAllowPrivateIPs.
	AllowPrivateIPs bool
	// AllowHTTP allows plain-HTTP (non-HTTPS) requests.
	AllowHTTP bool
}

// defaultOptions returns the default options for Anthropic.
func defaultOptions() *options {
	return &options{
		Model: "claude-sonnet-4-20250514",
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
