// Package infinity provides a client for Infinity embeddings server.
//
// Infinity is a high-throughput, low-latency REST API for serving text-embeddings
// and reranking models. It uses an OpenAI-compatible API for embeddings.
package infinity

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	llms "github.com/nocturnium/llm-go-sdk/v2"
	"github.com/nocturnium/llm-go-sdk/v2/internal/httpclient"
	"github.com/nocturnium/llm-go-sdk/v2/pkg/openaicompat"
)

// Client is an Infinity embeddings client.
//
// Thread-safety: All methods are safe for concurrent use.
type Client struct {
	client       *openaicompat.Client
	httpClient   *httpclient.Client
	baseURL      string
	options      *options
	capabilities llms.Capabilities
}

// New creates a new Infinity client with the given options.
func New(opts ...Option) (*Client, error) {
	options := apply(opts...)

	// Resolve API key from environment if not provided (optional for local deployments)
	options.APIKey = llms.ResolveAPIKey(options.APIKey, llms.EnvInfinityAPIKey)

	var hcOpts []httpclient.ClientOption
	if options.HTTPClient != nil {
		hcOpts = append(hcOpts, httpclient.WithHTTPClient(options.HTTPClient))
	}
	if options.Timeout > 0 {
		hcOpts = append(hcOpts, httpclient.WithTimeout(options.Timeout))
	}
	hcOpts = append(hcOpts,
		httpclient.WithAllowPrivateIPs(options.AllowPrivateIPs),
		httpclient.WithAllowHTTP(options.AllowHTTP),
	)
	httpClient := httpclient.NewClient(hcOpts...)

	clientConfig := openaicompat.ClientConfig{
		BaseURL:         options.BaseURL,
		APIKey:          options.APIKey,
		HTTPClient:      options.HTTPClient,
		Timeout:         options.Timeout,
		AllowPrivateIPs: options.AllowPrivateIPs,
		AllowHTTP:       options.AllowHTTP,
	}

	client := openaicompat.NewClient(clientConfig)

	return &Client{
		client:     client,
		httpClient: httpClient,
		baseURL:    options.BaseURL,
		options:    options,
		capabilities: llms.Capabilities{
			Streaming:  false, // Infinity doesn't support chat streaming
			Tools:      false,
			Vision:     false,
			Embeddings: true, // Primary feature
			Batch:      true, // Efficient batch processing
			JSONMode:   false,
		},
	}, nil
}

// Provider returns the provider type.
func (c *Client) Provider() llms.Provider {
	return llms.ProviderInfinity
}

// Capabilities returns the provider's capabilities.
func (c *Client) Capabilities() llms.Capabilities {
	return c.capabilities
}

// Embed generates embeddings for one or more texts.
func (c *Client) Embed(ctx context.Context, texts []string, options ...llms.EmbedOption) (*llms.EmbeddingResponse, error) {
	if err := llms.ValidateEmbedInput(texts); err != nil {
		return nil, err
	}

	opts := llms.ApplyEmbedOptions(options...)

	model := c.options.EmbeddingModel
	if opts.Model != "" {
		model = opts.Model
	}

	// Use RunPod format if enabled
	if c.options.RunPodMode {
		return c.embedRunPod(ctx, texts, model, opts)
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
		return nil, llms.WrapProviderError(llms.ProviderInfinity, "embed", err)
	}

	return openaicompat.ConvertEmbeddingResponse(resp), nil
}

// embedRunPod sends an embedding request using RunPod's serverless API format.
func (c *Client) embedRunPod(ctx context.Context, texts []string, model string, opts *llms.EmbedOptions) (*llms.EmbeddingResponse, error) {
	// Build the OpenAI-compatible embedding request
	openAIInput := map[string]any{
		"model": model,
		"input": texts,
	}
	if opts.EncodingFormat != "" {
		openAIInput["encoding_format"] = opts.EncodingFormat
	}
	if opts.Dimensions > 0 {
		openAIInput["dimensions"] = opts.Dimensions
	}
	if opts.User != "" {
		openAIInput["user"] = opts.User
	}

	// Wrap in RunPod format
	runPodReq := &runPodRequest{
		Input: runPodInput{
			OpenAIRoute: "/v1/embeddings",
			OpenAIInput: openAIInput,
		},
	}

	var runPodResp runPodResponse
	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method:  "POST",
		URL:     c.baseURL + "/runsync",
		Headers: c.getHeaders(),
		Body:    runPodReq,
	}, &runPodResp)

	if err != nil {
		return nil, llms.WrapProviderError(llms.ProviderInfinity, "embed", err)
	}

	if runPodResp.Status != "COMPLETED" {
		return nil, llms.WrapProviderError(llms.ProviderInfinity, "embed",
			fmt.Errorf("runpod job failed with status: %s", runPodResp.Status))
	}

	if len(runPodResp.Output) == 0 {
		return nil, llms.WrapProviderError(llms.ProviderInfinity, "embed",
			fmt.Errorf("runpod returned empty output"))
	}

	// Parse the embedded output - it's the first element wrapped in the Output array
	outputBytes, err := json.Marshal(runPodResp.Output[0])
	if err != nil {
		return nil, llms.WrapProviderError(llms.ProviderInfinity, "embed", err)
	}

	var embedOutput runPodEmbeddingOutput
	if err := json.Unmarshal(outputBytes, &embedOutput); err != nil {
		return nil, llms.WrapProviderError(llms.ProviderInfinity, "embed", err)
	}

	// Convert to standard response format
	embeddings := make([]llms.Embedding, len(embedOutput.Data))
	for i, d := range embedOutput.Data {
		embeddings[i] = llms.Embedding{
			Index:  d.Index,
			Vector: d.Embedding,
			Object: d.Object,
		}
	}

	return &llms.EmbeddingResponse{
		Embeddings: embeddings,
		Model:      embedOutput.Model,
		Usage: llms.EmbeddingUsage{
			PromptTokens: embedOutput.Usage.PromptTokens,
			TotalTokens:  embedOutput.Usage.TotalTokens,
		},
	}, nil
}

// EmbedQuery embeds a single query text.
func (c *Client) EmbedQuery(ctx context.Context, text string, options ...llms.EmbedOption) ([]float32, error) {
	return llms.EmbedQuery(ctx, c, text, options...)
}

// EmbedDocuments embeds multiple documents.
func (c *Client) EmbedDocuments(ctx context.Context, texts []string, options ...llms.EmbedOption) ([][]float32, error) {
	return llms.EmbedDocuments(ctx, c, texts, options...)
}

// Rerank scores and ranks documents by relevance to the query.
// Results are sorted by relevance score in descending order.
func (c *Client) Rerank(ctx context.Context, query string, documents []string, options ...llms.RerankOption) (*llms.RerankResponse, error) {
	if query == "" {
		return nil, llms.ErrEmptyInput
	}
	if len(documents) == 0 {
		return nil, llms.ErrEmptyInput
	}

	opts := llms.ApplyRerankOptions(options...)

	model := c.options.RerankModel
	if opts.Model != "" {
		model = opts.Model
	}

	// Use RunPod format if enabled
	if c.options.RunPodMode {
		return c.rerankRunPod(ctx, query, documents, model, opts)
	}

	req := &rerankRequest{
		Model:           model,
		Query:           query,
		Documents:       documents,
		TopN:            opts.TopN,
		ReturnDocuments: opts.ReturnDocuments,
	}

	var resp rerankAPIResponse
	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method:  "POST",
		URL:     c.baseURL + "/rerank",
		Headers: c.getHeaders(),
		Body:    req,
	}, &resp)

	if err != nil {
		return nil, llms.WrapProviderError(llms.ProviderInfinity, "rerank", err)
	}

	return c.convertRerankResponse(&resp), nil
}

// rerankRunPod sends a rerank request using RunPod's serverless API format.
func (c *Client) rerankRunPod(ctx context.Context, query string, documents []string, model string, opts *llms.RerankOptions) (*llms.RerankResponse, error) {
	// Build the rerank request (Infinity API format)
	openAIInput := map[string]any{
		"model":     model,
		"query":     query,
		"documents": documents,
	}
	if opts.TopN > 0 {
		openAIInput["top_n"] = opts.TopN
	}
	if opts.ReturnDocuments {
		openAIInput["return_documents"] = opts.ReturnDocuments
	}

	// Wrap in RunPod format
	runPodReq := &runPodRequest{
		Input: runPodInput{
			OpenAIRoute: "/v1/rerank",
			OpenAIInput: openAIInput,
		},
	}

	var runPodResp runPodResponse
	err := c.httpClient.DoJSON(ctx, httpclient.Request{
		Method:  "POST",
		URL:     c.baseURL + "/runsync",
		Headers: c.getHeaders(),
		Body:    runPodReq,
	}, &runPodResp)

	if err != nil {
		return nil, llms.WrapProviderError(llms.ProviderInfinity, "rerank", err)
	}

	if runPodResp.Status != "COMPLETED" {
		return nil, llms.WrapProviderError(llms.ProviderInfinity, "rerank",
			fmt.Errorf("runpod job failed with status: %s", runPodResp.Status))
	}

	if len(runPodResp.Output) == 0 {
		return nil, llms.WrapProviderError(llms.ProviderInfinity, "rerank",
			fmt.Errorf("runpod returned empty output"))
	}

	// Parse the rerank output
	outputBytes, err := json.Marshal(runPodResp.Output[0])
	if err != nil {
		return nil, llms.WrapProviderError(llms.ProviderInfinity, "rerank", err)
	}

	var rerankResp rerankAPIResponse
	if err := json.Unmarshal(outputBytes, &rerankResp); err != nil {
		return nil, llms.WrapProviderError(llms.ProviderInfinity, "rerank", err)
	}

	return c.convertRerankResponse(&rerankResp), nil
}

// convertRerankResponse converts an API response to the standard format.
func (c *Client) convertRerankResponse(resp *rerankAPIResponse) *llms.RerankResponse {
	// Sort results by score descending
	results := make([]llms.RerankResult, len(resp.Results))
	for i, r := range resp.Results {
		results[i] = llms.RerankResult{
			Index:    r.Index,
			Score:    r.RelevanceScore,
			Document: r.Document,
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return &llms.RerankResponse{
		Results: results,
		Model:   resp.Model,
		Usage: llms.RerankUsage{
			TotalTokens: resp.Usage.TotalTokens,
		},
	}
}

func (c *Client) getHeaders() map[string]string {
	headers := make(map[string]string)
	if c.options.APIKey != "" {
		headers["Authorization"] = "Bearer " + c.options.APIKey
	}
	return headers
}

// rerankRequest is the request body for the /rerank endpoint.
type rerankRequest struct {
	Model           string   `json:"model"`
	Query           string   `json:"query"`
	Documents       []string `json:"documents"`
	TopN            int      `json:"top_n,omitempty"`
	ReturnDocuments bool     `json:"return_documents,omitempty"`
}

// rerankAPIResponse is the response from the /rerank endpoint.
type rerankAPIResponse struct {
	Results []rerankAPIResult `json:"results"`
	Model   string            `json:"model"`
	Usage   struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// rerankAPIResult is a single result from the reranking API.
type rerankAPIResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
	Document       string  `json:"document,omitempty"`
}

// RunPod types for serverless API format.

// runPodRequest wraps a request for RunPod serverless endpoints.
type runPodRequest struct {
	Input runPodInput `json:"input"`
}

// runPodInput contains the OpenAI-compatible route and input.
type runPodInput struct {
	OpenAIRoute string `json:"openai_route"`
	OpenAIInput any    `json:"openai_input"`
}

// runPodResponse wraps a response from RunPod serverless endpoints.
type runPodResponse struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	DelayTime     int    `json:"delayTime"`
	ExecutionTime int    `json:"executionTime"`
	Output        []any  `json:"output"`
	WorkerID      string `json:"workerId"`
}

// runPodEmbeddingOutput is the embedding output from RunPod.
type runPodEmbeddingOutput struct {
	Object string                `json:"object"`
	Model  string                `json:"model"`
	Data   []runPodEmbeddingData `json:"data"`
	Usage  runPodEmbeddingUsage  `json:"usage"`
}

// runPodEmbeddingData is a single embedding from RunPod.
type runPodEmbeddingData struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

// runPodEmbeddingUsage is usage info from RunPod.
type runPodEmbeddingUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// Ensure Client implements the Embedder interface.
var _ llms.Embedder = (*Client)(nil)

// Ensure Client implements the Reranker interface.
var _ llms.Reranker = (*Client)(nil)

// Ensure Client implements the CapableProvider interface (partially).
// Note: Infinity doesn't implement the full LLM interface as it's an embedding service.
