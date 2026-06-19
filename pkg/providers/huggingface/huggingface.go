// Package huggingface provides a client for HuggingFace Inference Endpoints. A
// single per-deployment endpoint serves one model, which HuggingFace exposes
// behind an OpenAI-compatible route depending on the container:
//
//   - Text Generation Inference (TGI) — chat/text-generation at
//     POST <endpoint>/v1/chat/completions. The client implements llms.LLM
//     (GenerateContent / Stream).
//   - Text Embeddings Inference (TEI) — embeddings at POST <endpoint>/v1/embeddings.
//     The client implements llms.Embedder.
//
// Both reuse the SDK's OpenAI-compatible HTTP client (SSRF protection, retries,
// error mapping). Because Inference Endpoints are per-deployment URLs, an endpoint
// is required (WithEndpoint); the token is the Bearer credential, taken from
// WithAPIKey or the HF_TOKEN / HUGGINGFACE_API_KEY environment variables. The
// provider is not in the by-name registry (it needs an endpoint) — construct it
// directly with New.
package huggingface

import (
	"context"
	"fmt"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v3"
	"github.com/nocturnium/llm-go-sdk/v3/pkg/openaicompat"
)

// providerConfig is the HuggingFace Inference Endpoints provider configuration.
// Capabilities here is the full superset (the OpenAI-compatible chat surface a TGI
// endpoint exposes plus embeddings); whether a given endpoint actually answers
// chat or embeddings depends on the model it has deployed, so (*Client).Capabilities
// narrows this superset to the endpoint's deployment mode at runtime.
var providerConfig = openaicompat.ProviderConfig{
	Provider:     llms.ProviderHuggingFace,
	ProviderName: "huggingface",
	Capabilities: llms.Capabilities{
		Streaming:  true,
		Tools:      true,
		JSONMode:   true,
		Embeddings: true,
		Batch:      true,
	},
}

// Client is a HuggingFace Inference Endpoints client. It speaks both the
// chat/completions (TGI) and embeddings (TEI) OpenAI-compatible routes; use the
// one your deployed endpoint serves.
//
// Thread-safety: all methods are safe for concurrent use.
type Client struct {
	openaicompat.BaseProvider
	options        *options
	embeddingModel string
}

// New creates a HuggingFace Inference Endpoints client. WithEndpoint is required;
// the token is taken from WithAPIKey or, failing that, HF_TOKEN /
// HUGGINGFACE_API_KEY. Use WithModel for a chat (TGI) endpoint and
// WithEmbeddingModel for an embeddings (TEI) endpoint.
func New(opts ...Option) (*Client, error) {
	o := apply(opts...)

	if strings.TrimSpace(o.Endpoint) == "" {
		return nil, fmt.Errorf("huggingface: endpoint is required (use WithEndpoint): %w", llms.ErrInvalidParameters)
	}

	apiKey, err := llms.RequireAPIKey("huggingface", o.APIKey, llms.EnvHuggingFaceToken, llms.EnvHuggingFaceAPIKey)
	if err != nil {
		return nil, err
	}

	client := openaicompat.NewClient(openaicompat.ClientConfig{
		BaseURL:         normalizeEndpoint(o.Endpoint),
		APIKey:          apiKey,
		HTTPClient:      o.HTTPClient,
		Timeout:         o.Timeout,
		AllowPrivateIPs: o.AllowPrivateIPs,
		AllowHTTP:       o.AllowHTTP,
	})

	// The embedding model defaults to the chat model when unset, so a single
	// WithModel still labels embedding requests as it did before chat support.
	embeddingModel := o.EmbeddingModel
	if embeddingModel == "" {
		embeddingModel = o.Model
	}

	cfg := providerConfig
	cfg.DefaultModel = o.Model
	cfg.DefaultEmbeddingModel = embeddingModel

	return &Client{
		BaseProvider:   openaicompat.NewBaseProvider(client, cfg),
		options:        o,
		embeddingModel: embeddingModel,
	}, nil
}

// Capabilities reports the feature surface for THIS endpoint's deployment mode.
//
// An HF Inference Endpoint serves a single deployment: a TGI chat endpoint will
// not answer embeddings and a TEI embeddings endpoint will not answer chat, so a
// flat superset would route capability-gated callers to unsupported routes. The
// mode is derived from the construction options:
//
//   - only WithModel (chat endpoint): chat caps (Streaming/Tools/JSONMode),
//     Embeddings/Batch off.
//   - only WithEmbeddingModel (embeddings endpoint): Embeddings/Batch, chat caps
//     off.
//   - both or neither set (mode ambiguous): the full superset, matching the
//     embedded BaseProvider.
//
// It shadows the embedded BaseProvider.Capabilities, masking the booleans for the
// resolved mode while preserving the merged registry/static integer fields (e.g.
// MaxContextTokens).
func (c *Client) Capabilities() llms.Capabilities {
	caps := c.BaseProvider.Capabilities()

	hasChat := c.options.Model != ""
	hasEmbeddings := c.options.EmbeddingModel != ""

	switch {
	case hasChat && !hasEmbeddings:
		// Chat-only (TGI): keep chat caps, drop embeddings.
		caps.Embeddings = false
		caps.Batch = false
	case hasEmbeddings && !hasChat:
		// Embeddings-only (TEI): keep embeddings/batch, drop chat caps.
		caps.Streaming = false
		caps.Tools = false
		caps.JSONMode = false
	default:
		// Both or neither set: mode is ambiguous, advertise the superset as-is.
	}

	return caps
}

// Embed generates embeddings for one or more texts via the endpoint's TEI route.
// It overrides the embedded BaseProvider's Embed to tolerate an unset model — TEI
// endpoints serve a fixed model and ignore the field — and to wrap errors with the
// huggingface provider.
func (c *Client) Embed(ctx context.Context, texts []string, options ...llms.EmbedOption) (*llms.EmbeddingResponse, error) {
	if err := llms.ValidateEmbedInput(texts); err != nil {
		return nil, err
	}

	opts := llms.ApplyEmbedOptions(options...)
	model := c.embeddingModel
	if opts.Model != "" {
		model = opts.Model
	}

	req := &openaicompat.EmbeddingRequest{
		Model:          model,
		Input:          texts,
		EncodingFormat: opts.EncodingFormat,
		Dimensions:     opts.Dimensions,
		User:           opts.User,
	}

	resp, err := c.Client().CreateEmbedding(ctx, req)
	if err != nil {
		return nil, llms.WrapProviderError(llms.ProviderHuggingFace, "embed", err)
	}
	return openaicompat.ConvertEmbeddingResponse(resp), nil
}

// EmbedQuery embeds a single query text.
func (c *Client) EmbedQuery(ctx context.Context, text string, options ...llms.EmbedOption) ([]float32, error) {
	return llms.EmbedQuery(ctx, c, text, options...)
}

// EmbedDocuments embeds multiple documents.
func (c *Client) EmbedDocuments(ctx context.Context, texts []string, options ...llms.EmbedOption) ([][]float32, error) {
	return llms.EmbedDocuments(ctx, c, texts, options...)
}

// normalizeEndpoint returns the OpenAI-compatible base URL ("<endpoint>/v1"),
// tolerating a trailing slash or an endpoint that already includes "/v1".
func normalizeEndpoint(endpoint string) string {
	base := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	base = strings.TrimSuffix(base, "/v1")
	return base + "/v1"
}

// Ensure Client implements the chat and embeddings interfaces.
var (
	_ llms.LLM             = (*Client)(nil)
	_ llms.CapableProvider = (*Client)(nil)
	_ llms.Embedder        = (*Client)(nil)
)
