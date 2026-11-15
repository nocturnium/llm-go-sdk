package cloudflare

import (
	"net/http"
	"time"
)

// Option configures a Cloudflare client.
type Option func(*options)

// options is the unexported client configuration.
type options struct {
	APIKey         string
	AccountID      string
	Model          string
	EmbeddingModel string
	DecisionModel  string
	BaseURL        string
	HTTPClient     *http.Client
	Timeout        time.Duration
	// AllowPrivateIPs allows requests to private/loopback IPs. Off by default.
	AllowPrivateIPs bool
	// AllowHTTP allows plain-HTTP requests. Off by default.
	AllowHTTP bool
}

func defaultOptions() *options {
	return &options{
		Model:          DefaultModel,
		EmbeddingModel: DefaultEmbeddingModel,
		DecisionModel:  DefaultDecisionModel,
		Timeout:        120 * time.Second,
	}
}

func apply(opts ...Option) *options {
	o := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	return o
}

// WithAPIKey sets the Cloudflare API token. Without it, New reads
// CLOUDFLARE_API_TOKEN, then LLM_API_KEY.
func WithAPIKey(key string) Option { return func(o *options) { o.APIKey = key } }

// WithAccountID sets the Cloudflare account ID. Without it, New reads
// CLOUDFLARE_ACCOUNT_ID. Not needed when WithBaseURL is set.
func WithAccountID(id string) Option { return func(o *options) { o.AccountID = id } }

// WithModel sets the default chat model.
func WithModel(model string) Option { return func(o *options) { o.Model = model } }

// WithEmbeddingModel sets the default embedding model.
func WithEmbeddingModel(model string) Option { return func(o *options) { o.EmbeddingModel = model } }

// WithDecisionModel sets the default decision model for Decide. A per-call
// llms.WithDecisionModel overrides it.
func WithDecisionModel(model string) Option { return func(o *options) { o.DecisionModel = model } }

// WithBaseURL replaces the account Workers AI base,
// https://api.cloudflare.com/client/v4/accounts/{account_id}/ai, for a proxy or
// test server. The client appends /v1 for chat and embeddings, /run/{model} for
// native models, and /models/search for the catalog.
func WithBaseURL(url string) Option { return func(o *options) { o.BaseURL = url } }

// WithHTTPClient sets the underlying HTTP client.
func WithHTTPClient(c *http.Client) Option { return func(o *options) { o.HTTPClient = c } }

// WithTimeout sets the request timeout. The default is 120 seconds.
func WithTimeout(d time.Duration) Option { return func(o *options) { o.Timeout = d } }

// WithAllowPrivateIPs allows requests to private and loopback addresses. Off by
// default (SSRF protection); needed only for a private proxy or test server.
func WithAllowPrivateIPs() Option { return func(o *options) { o.AllowPrivateIPs = true } }

// WithAllowHTTP allows plain-HTTP requests. Off by default; pair with
// WithAllowPrivateIPs for a local plain-HTTP endpoint.
func WithAllowHTTP() Option { return func(o *options) { o.AllowHTTP = true } }
