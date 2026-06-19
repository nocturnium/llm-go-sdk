// Package huggingface provides an embeddings client for HuggingFace Inference
// Endpoints. It targets the OpenAI-compatible embeddings route (POST
// /v1/embeddings) exposed by Text Embeddings Inference (TEI) — the container HF
// runs for embedding/feature-extraction models — so it reuses the SDK's
// OpenAI-compatible client.
//
// HuggingFace Inference Endpoints are per-deployment URLs, so an endpoint is
// required; the token is the Bearer credential (HF_TOKEN).
//
// Example:
//
//	emb, err := huggingface.New(
//	    huggingface.WithEndpoint("https://xxxx.endpoints.huggingface.cloud"),
//	    huggingface.WithAPIKey(os.Getenv("HF_TOKEN")),
//	)
//	if err != nil { return err }
//	vecs, err := emb.EmbedDocuments(ctx, []string{"hello", "world"})
package huggingface

import (
	"context"
	"fmt"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v3"
	"github.com/nocturnium/llm-go-sdk/v3/pkg/openaicompat"
)

// Client is a HuggingFace Inference Endpoints embeddings client.
//
// Thread-safety: all methods are safe for concurrent use.
type Client struct {
	client         *openaicompat.Client
	embeddingModel string
}

// New creates a HuggingFace embeddings client. WithEndpoint is required; the token
// is taken from WithAPIKey or, failing that, HF_TOKEN / HUGGINGFACE_API_KEY.
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

	return &Client{client: client, embeddingModel: o.EmbeddingModel}, nil
}

// normalizeEndpoint returns the OpenAI-compatible base URL ("<endpoint>/v1"),
// tolerating a trailing slash or an endpoint that already includes "/v1".
func normalizeEndpoint(endpoint string) string {
	base := strings.TrimRight(strings.TrimSpace(endpoint), "/")
	base = strings.TrimSuffix(base, "/v1")
	return base + "/v1"
}

// Provider returns the provider type.
func (c *Client) Provider() llms.Provider { return llms.ProviderHuggingFace }

// Capabilities reports that this client supports embeddings only.
func (c *Client) Capabilities() llms.Capabilities {
	return llms.Capabilities{Embeddings: true, Batch: true}
}

// Embed generates embeddings for one or more texts.
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

	resp, err := c.client.CreateEmbedding(ctx, req)
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

// Ensure Client implements the embeddings interfaces.
var _ llms.Embedder = (*Client)(nil)
