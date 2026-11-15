package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/decisionwire"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

const apiBase = "https://api.cloudflare.com/client/v4"

const (
	// DefaultModel is the default chat model, live-verified on /ai/v1 on 2026-11-14.
	DefaultModel = "@cf/openai/gpt-oss-120b"
	// DefaultEmbeddingModel is the default embedding model (1024 dimensions),
	// live-verified on 2026-11-14.
	DefaultEmbeddingModel = "@cf/baai/bge-m3"
	// DefaultDecisionModel is the fast 9B Clef decision model, live-verified on
	// /ai/run on 2026-11-14.
	DefaultDecisionModel = "@cf/cloudflare/clef-flash"
)

// providerConfig advertises routing support across the Workers AI catalog;
// individual models support fewer features. Context and output limits are
// model dependent and come from the catalog (ListModels), not from here.
var providerConfig = openaicompat.ProviderConfig{
	Provider: llms.ProviderCloudflare,
	Capabilities: llms.Capabilities{
		Streaming:       true,
		Tools:           true,
		Vision:          true,
		Embeddings:      true,
		JSONMode:        true,
		Decision:        true,
		ImageGeneration: true,
		Speech:          true,
		Transcription:   true,
	},
}

// Client is a Cloudflare Workers AI client, safe for concurrent use.
type Client struct {
	openaicompat.BaseProvider
	options   *options
	transport *httpclient.Client
	aiBase    *url.URL
	headers   map[string]string
}

// New constructs a client. It returns ErrMissingAPIKey without a token, and
// ErrInvalidParameters without an account ID (unless WithBaseURL is set), for a
// malformed account ID, for an invalid base URL or timeout, or for an http://
// base URL without WithAllowHTTP.
func New(opts ...Option) (*Client, error) {
	o := apply(opts...)
	key, err := llms.RequireAPIKey("cloudflare", o.APIKey, llms.EnvCloudflareAPIToken)
	if err != nil {
		return nil, err
	}
	o.APIKey = key
	base, err := resolveBase(o)
	if err != nil {
		return nil, err
	}
	compat := openaicompat.NewClient(openaicompat.ClientConfig{
		BaseURL:         base.JoinPath("v1").String(),
		APIKey:          key,
		HTTPClient:      o.HTTPClient,
		Timeout:         o.Timeout,
		AllowPrivateIPs: o.AllowPrivateIPs,
		AllowHTTP:       o.AllowHTTP,
		ErrorCodeMapper: mapErrorCode,
	})
	cfg := providerConfig
	cfg.DefaultModel = o.Model
	cfg.DefaultEmbeddingModel = o.EmbeddingModel
	transportOptions := []httpclient.ClientOption{
		httpclient.WithAllowPrivateIPs(o.AllowPrivateIPs),
		httpclient.WithAllowHTTP(o.AllowHTTP),
		httpclient.WithErrorCodeMapper(mapErrorCode),
	}
	if o.HTTPClient != nil {
		transportOptions = append(transportOptions, httpclient.WithHTTPClient(o.HTTPClient))
	}
	if o.Timeout > 0 {
		transportOptions = append(transportOptions, httpclient.WithTimeout(o.Timeout))
	}
	return &Client{
		BaseProvider: openaicompat.NewBaseProvider(compat, cfg),
		options:      o,
		transport:    httpclient.NewClient(transportOptions...),
		aiBase:       base,
		headers:      map[string]string{"Authorization": "Bearer " + key},
	}, nil
}

// resolveBase returns the account Workers AI base URL.
func resolveBase(o *options) (*url.URL, error) {
	raw := o.BaseURL
	if raw == "" {
		account := o.AccountID
		if account == "" {
			account = os.Getenv(llms.EnvCloudflareAccountID)
		}
		if account == "" {
			return nil, fmt.Errorf("cloudflare: account ID is required (WithAccountID or %s): %w", llms.EnvCloudflareAccountID, llms.ErrInvalidParameters)
		}
		if !isAccountID(account) {
			return nil, fmt.Errorf("cloudflare: account ID must be letters and digits only: %w", llms.ErrInvalidParameters)
		}
		raw = apiBase + "/accounts/" + url.PathEscape(account) + "/ai"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || o.Timeout < 0 {
		return nil, fmt.Errorf("cloudflare: invalid base URL or timeout: %w", llms.ErrInvalidParameters)
	}
	if u.Scheme == "http" && !o.AllowHTTP {
		return nil, fmt.Errorf("cloudflare: plain-HTTP base URL needs WithAllowHTTP: %w", llms.ErrInvalidParameters)
	}
	return u, nil
}

// isAccountID reports whether s is a plausible account ID. Cloudflare account
// IDs are 32 hex digits; letters and digits only keeps the path segment safe.
func isAccountID(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

// mapErrorCode maps Workers AI error codes, observed live on 2026-11-14, onto
// the canonical codes llms.APIError classifies. Chat, embeddings and /ai/run
// share it, so a billing failure reads the same on every route.
//
//	2021  "Insufficient balance; add money to your gateway or use BYOK" (402)
//	5007  "No such model" (400, chat route)
//	7000  "No route for that URI" (400, unknown /ai/run model). 7000 also means
//	      "Invalid request body", so only the No-route message maps to a model miss.
func mapErrorCode(status int, code, message string) string {
	switch code {
	case "2021":
		return decisionwire.CodeInsufficientQuota
	case "5007":
		return decisionwire.CodeModelNotFound
	case "7000":
		if strings.Contains(message, "No route") {
			return decisionwire.CodeModelNotFound
		}
	}
	if status == http.StatusPaymentRequired && isDigits(code) {
		return decisionwire.CodeInsufficientQuota
	}
	return code
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// envelope is the Cloudflare v4 response wrapper around every /ai/run and
// catalog result.
type envelope struct {
	Result     json.RawMessage `json:"result"`
	Success    bool            `json:"success"`
	Errors     []envelopeError `json:"errors"`
	ResultInfo struct {
		PerPage int `json:"per_page"`
	} `json:"result_info"`
}

type envelopeError struct {
	Code    json.Number `json:"code"`
	Message string      `json:"message"`
}

// send posts or gets route under the account AI base and returns the envelope.
// Transport errors arrive as *llms.APIError with the provider set and Workers AI
// codes already mapped by mapErrorCode. A response that is HTTP 200 but
// success:false (the HTTP status is then 200, so only the code classifies it)
// is returned the same way. Sensitive requests keep their body out of the
// debug log.
func (c *Client) send(ctx context.Context, method string, route []string, query url.Values, body any, sensitive bool) (*envelope, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	u := c.aiBase.JoinPath(route...)
	u.RawQuery = query.Encode()
	var env envelope
	err := c.transport.DoJSON(ctx, httpclient.Request{Method: method, URL: u.String(), Headers: c.headers, Body: body, Sensitive: sensitive}, &env)
	if err != nil {
		return nil, openaicompat.WrapError(c.Provider(), strings.Join(route, "/"), err)
	}
	if !env.Success {
		code, message := "", "request was not successful"
		if len(env.Errors) > 0 {
			code, message = env.Errors[0].Code.String(), env.Errors[0].Message
		}
		apiErr := &llms.APIError{StatusCode: http.StatusOK, Code: mapErrorCode(http.StatusOK, code, message), Message: message, Provider: c.Provider(), RequestURL: u.String(), RequestMethod: method}
		if apiErr.Code != decisionwire.CodeInsufficientQuota && apiErr.Code != decisionwire.CodeModelNotFound {
			// No canonical code applies and a 200 status classifies nothing, so an
			// unmapped failure reads as an invalid request rather than nothing.
			apiErr.Type = "invalid_request_error"
		}
		return nil, apiErr
	}
	return &env, nil
}

// post sends body to route and decodes the envelope's result into out.
func (c *Client) post(ctx context.Context, route []string, body any, sensitive bool, out any) error {
	env, err := c.send(ctx, http.MethodPost, route, nil, body, sensitive)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return fmt.Errorf("cloudflare: decode %s result: %w: %w", strings.Join(route, "/"), llms.ErrIncompleteResponse, err)
	}
	return nil
}

// runRoute is the /ai/run route for model. Each slash-separated part of the
// model ID is its own path segment, which is how Workers AI routes it:
// /ai/run/@cf/cloudflare/clef-flash was live-verified on 2026-11-14. Callers
// pass only allowlisted model IDs, so no segment can be "." or "..".
func runRoute(model string) []string { return append([]string{"run"}, strings.Split(model, "/")...) }

// Ensure Client implements the expected interfaces.
var (
	_ llms.LLM             = (*Client)(nil)
	_ llms.CapableProvider = (*Client)(nil)
	_ llms.ModelLister     = (*Client)(nil)
	_ llms.Embedder        = (*Client)(nil)
	_ llms.Decider         = (*Client)(nil)
)
