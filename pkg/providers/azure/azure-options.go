package azure

import (
	"net/http"
	"time"
)

// Option is a function that configures an Azure OpenAI client.
type Option func(*options)

// options contains configuration options for the Azure OpenAI client.
type options struct {
	APIKey              string
	Endpoint            string // Full Azure endpoint (e.g., https://myresource.openai.azure.com)
	DeploymentName      string // Deployment name (required)
	EmbeddingDeployment string // Embedding deployment name
	APIVersion          string // API version (default: 2024-02-15-preview)
	HTTPClient          *http.Client
	Timeout             time.Duration
	// AllowPrivateIPs allows requests to private/loopback IPs.
	// Off by default for SSRF safety; see WithAllowPrivateIPs.
	AllowPrivateIPs bool
	// AllowHTTP allows plain-HTTP (non-HTTPS) requests.
	AllowHTTP bool
}

// defaultOptions returns the default options for Azure OpenAI.
func defaultOptions() *options {
	return &options{
		APIVersion: "2024-02-15-preview",
	}
}

// WithAPIKey sets the API key.
func WithAPIKey(key string) Option {
	return func(o *options) {
		o.APIKey = key
	}
}

// WithEndpoint sets the Azure OpenAI endpoint.
func WithEndpoint(endpoint string) Option {
	return func(o *options) {
		o.Endpoint = endpoint
	}
}

// WithDeployment sets the deployment name.
func WithDeployment(deployment string) Option {
	return func(o *options) {
		o.DeploymentName = deployment
	}
}

// WithEmbeddingDeployment sets the embedding deployment name.
func WithEmbeddingDeployment(deployment string) Option {
	return func(o *options) {
		o.EmbeddingDeployment = deployment
	}
}

// WithAPIVersion sets the API version.
func WithAPIVersion(version string) Option {
	return func(o *options) {
		o.APIVersion = version
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
