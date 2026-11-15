package openrouter

import (
	"net/http"
	"time"
)

// Option is a function that configures a OpenRouter client.
type Option func(*options)

// options contains configuration options for the OpenRouter client.
type options struct {
	SiteURL        string
	AppName        string
	UsageLookup    bool
	APIKey         string
	Model          string
	EmbeddingModel string
	DecisionModel  string
	BaseURL        string
	HTTPClient     *http.Client
	Timeout        time.Duration
	// AllowPrivateIPs allows requests to private/loopback IPs.
	// Off by default for SSRF safety; see WithAllowPrivateIPs.
	AllowPrivateIPs bool
	// AllowHTTP allows plain-HTTP (non-HTTPS) requests.
	AllowHTTP bool
	// ReplayReasoning sends assistant reasoning back; see WithReplayReasoning.
	ReplayReasoning bool
}

// defaultOptions returns the default options for OpenRouter.
func defaultOptions() *options {
	return &options{
		Model:         DefaultModel,
		DecisionModel: DefaultDecisionModel,
		Timeout:       120 * time.Second,
		BaseURL:       defaultBaseURL,
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

// WithDecisionModel sets the default decision model for Decide. A per-call
// llms.WithDecisionModel overrides it.
func WithDecisionModel(model string) Option {
	return func(o *options) {
		o.DecisionModel = model
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
		if opt == nil {
			continue
		}
		opt(options)
	}
	return options
}

// WithSiteURL sets the optional HTTP-Referer attribution header.
func WithSiteURL(value string) Option { return func(o *options) { o.SiteURL = value } }

// WithAppName sets the optional X-Title attribution header.
func WithAppName(value string) Option { return func(o *options) { o.AppName = value } }

// WithReplayReasoning sends each assistant message's reasoning back in its
// "reasoning" field, which OpenRouter passes to the upstream model, so a model
// that thinks between tool calls can continue its earlier thinking instead of
// re-deriving it from the tool results. It is off by default because it is not
// a gain for every model: a multi-turn tool loop over DeepSeek V4.1 Flash scored
// worse with it than without (F1 0.378 against 0.415 on 20 pull requests, three
// passes each). Reasoning another provider stamped is dropped first, as for any
// replay.
func WithReplayReasoning() Option { return func(o *options) { o.ReplayReasoning = true } }

// WithUsageLookup enables a post-speech generation lookup for reported cost.
// Lookup failures leave cost unknown and preserve successful audio.
func WithUsageLookup() Option { return func(o *options) { o.UsageLookup = true } }
