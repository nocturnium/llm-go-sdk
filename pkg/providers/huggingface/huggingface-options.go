package huggingface

import (
	"net/http"
	"time"
)

// Option configures a HuggingFace embeddings client.
type Option func(*options)

type options struct {
	APIKey         string
	Endpoint       string
	EmbeddingModel string
	HTTPClient     *http.Client
	Timeout        time.Duration
	// AllowPrivateIPs / AllowHTTP relax SSRF protection for self-hosted TEI servers
	// reached over a private network or plain HTTP. Off by default: managed HF
	// Inference Endpoints are public HTTPS.
	AllowPrivateIPs bool
	AllowHTTP       bool
}

func defaultOptions() *options {
	return &options{}
}

// WithAPIKey sets the HuggingFace token (the Bearer credential). If unset, it is
// resolved from HF_TOKEN or HUGGINGFACE_API_KEY.
func WithAPIKey(key string) Option {
	return func(o *options) { o.APIKey = key }
}

// WithEndpoint sets the Inference Endpoint base URL (e.g.
// "https://xxxx.endpoints.huggingface.cloud"). The "/v1" suffix is added
// automatically if absent. Required.
func WithEndpoint(url string) Option {
	return func(o *options) { o.Endpoint = url }
}

// WithBaseURL is an alias for WithEndpoint.
func WithBaseURL(url string) Option {
	return func(o *options) { o.Endpoint = url }
}

// WithEmbeddingModel sets the model name sent in the request. TEI-backed
// endpoints serve a fixed model and ignore this value, but setting it to the real
// model id makes usage/metrics labeling accurate.
func WithEmbeddingModel(model string) Option {
	return func(o *options) { o.EmbeddingModel = model }
}

// WithModel is an alias for WithEmbeddingModel.
func WithModel(model string) Option {
	return func(o *options) { o.EmbeddingModel = model }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(o *options) { o.HTTPClient = client }
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(o *options) { o.Timeout = timeout }
}

// WithAllowPrivateIPs permits requests to private/loopback addresses (for
// self-hosted TEI servers). Off by default for SSRF safety.
func WithAllowPrivateIPs(allow bool) Option {
	return func(o *options) { o.AllowPrivateIPs = allow }
}

// WithAllowHTTP permits plain-HTTP (non-TLS) requests. Off by default.
func WithAllowHTTP(allow bool) Option {
	return func(o *options) { o.AllowHTTP = allow }
}

func apply(opts ...Option) *options {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}
	return o
}
