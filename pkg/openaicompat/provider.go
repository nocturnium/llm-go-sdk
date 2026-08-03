package openaicompat

import (
	"context"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// ProviderConfig defines the configuration for an OpenAI-compatible provider.
// This is used to customize the base provider behavior.
type ProviderConfig struct {
	// Provider is the provider type (e.g., llms.ProviderOpenAI). Its string form
	// is also used as the label in error messages.
	Provider llms.Provider

	// DefaultModel is the default model for chat/completion requests.
	DefaultModel string

	// DefaultEmbeddingModel is the default model for embeddings (optional)
	DefaultEmbeddingModel string

	// Capabilities defines what features this provider supports
	Capabilities llms.Capabilities

	// UseResponsesAPI routes GenerateContent and Stream through the OpenAI
	// Responses API (POST /responses) instead of /chat/completions. Only
	// meaningful for providers whose endpoint implements the Responses API (OpenAI).
	UseResponsesAPI bool
}

// BaseProvider provides common functionality for OpenAI-compatible providers.
// Embed this in your provider's Client struct to get shared implementations.
//
// Example usage:
//
//	type Client struct {
//	    openaicompat.BaseProvider
//	    options *Options
//	}
//
//	func New(opts ...Option) (*Client, error) {
//	    // ... setup ...
//	    return &Client{
//	        BaseProvider: openaicompat.NewBaseProvider(client, config),
//	        options: options,
//	    }, nil
//	}
type BaseProvider struct {
	client         *Client
	config         ProviderConfig
	model          string
	embeddingModel string
}

// NewBaseProvider creates a new base provider with the given configuration.
// It is a provider-author extension point for implementing a new
// OpenAI-compatible provider on top of Client, not an end-user client
// constructor. End users normally construct clients through provider packages
// such as openai.New or through the llms.New registry path.
func NewBaseProvider(client *Client, config ProviderConfig) BaseProvider {
	return BaseProvider{
		client:         client,
		config:         config,
		model:          config.DefaultModel,
		embeddingModel: config.DefaultEmbeddingModel,
	}
}

// Call sends a prompt to the LLM and returns the response.
func (p *BaseProvider) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: prompt},
	}

	resp, err := p.GenerateContent(ctx, messages, options...)
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

// GenerateContent generates content with more control over messages.
func (p *BaseProvider) GenerateContent(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.Response, error) {
	opts := llms.ApplyOptions(options...)

	if err := opts.Validate(); err != nil {
		return nil, err
	}

	// Prepare messages (merge consecutive same-role messages unless disabled, then validate)
	prepared, err := llms.PrepareMessages(messages, opts)
	if err != nil {
		return nil, err
	}
	if err := llms.ValidateInlineSystem(prepared); err != nil {
		return nil, err
	}
	if err := llms.ValidateToolCallIDs(prepared); err != nil {
		return nil, err
	}

	model := effectiveModel(p.model, opts.Model)

	var result *llms.Response
	if p.config.UseResponsesAPI {
		req := BuildResponsesRequest(model, prepared, opts, false)
		resp, err := p.client.CreateResponse(ctx, req)
		if err != nil {
			return nil, WrapError(p.config.Provider, "generate content", err)
		}
		if ferr := responsesResponseError(resp); ferr != nil {
			return nil, WrapError(p.config.Provider, "generate content", ferr)
		}
		result = ConvertResponsesResponse(resp)
	} else {
		req := BuildChatRequest(model, prepared, opts, false)
		resp, err := p.client.CreateChatCompletion(ctx, req)
		if err != nil {
			return nil, WrapError(p.config.Provider, "generate content", err)
		}
		result = ConvertResponse(resp)
	}

	// Apply token estimation if enabled and usage is missing
	if opts.EstimateTokens && result.Usage.TotalTokens == 0 {
		result.Usage = llms.EstimateUsageFromMessages(prepared, result.Content)
	}

	return result, nil
}

// Stream generates content with streaming, returning chunks via channel.
func (p *BaseProvider) Stream(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	opts := llms.ApplyOptions(options...)

	if err := opts.Validate(); err != nil {
		return nil, err
	}

	// Prepare messages (merge consecutive same-role messages unless disabled, then validate)
	prepared, err := llms.PrepareMessages(messages, opts)
	if err != nil {
		return nil, err
	}
	if err := llms.ValidateInlineSystem(prepared); err != nil {
		return nil, err
	}
	if err := llms.ValidateToolCallIDs(prepared); err != nil {
		return nil, err
	}

	model := effectiveModel(p.model, opts.Model)

	bufferSize := llms.GetBufferSize(opts)
	chunks := make(chan llms.StreamChunk, bufferSize)
	sender := llms.NewStreamSender(ctx, chunks, opts.StreamSendTimeout)

	// Configure stream processing with token estimation if enabled
	var config *StreamConfig
	if opts.EstimateTokens {
		config = &StreamConfig{
			Messages:       prepared,
			EstimateTokens: true,
		}
	}

	if p.config.UseResponsesAPI {
		req := BuildResponsesRequest(model, prepared, opts, true)
		stream, err := p.client.CreateResponseStream(ctx, req)
		if err != nil {
			return nil, WrapError(p.config.Provider, "stream", err)
		}
		go ProcessResponsesStream(ctx, stream, chunks, sender, string(p.config.Provider), config)
		return chunks, nil
	}

	req := BuildChatRequest(model, prepared, opts, true)
	stream, err := p.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, WrapError(p.config.Provider, "stream", err)
	}

	go ProcessStream(ctx, stream, chunks, sender, string(p.config.Provider), config)

	return chunks, nil
}

// Provider returns the provider type.
func (p *BaseProvider) Provider() llms.Provider {
	return p.config.Provider
}

// Model returns the model name.
func (p *BaseProvider) Model() string {
	return p.model
}

// Capabilities returns the provider's capabilities.
//
// The result MERGES the per-model registry data with the provider's explicit
// static ProviderConfig.Capabilities. The provider's explicitly declared,
// non-zero capability fields always win; the registry only fills fields the
// static config left at their zero value. This prevents a registry default from
// silently downgrading a capability the provider declared (e.g. a provider
// statically declaring Vision:true must never report Vision:false because a
// conservative registry default says so).
func (p *BaseProvider) Capabilities() llms.Capabilities {
	static := p.config.Capabilities

	// Start from the per-model registry data (zero value if unknown), then
	// overlay the static config so its explicit, non-zero fields win.
	caps := llms.GetModelCapabilities(p.config.Provider, p.model).ToCapabilities()

	// Boolean capabilities: a static true is an explicit declaration that must
	// win; a static false is the zero value and never downgrades a registry true.
	caps.Streaming = caps.Streaming || static.Streaming
	caps.Tools = caps.Tools || static.Tools
	caps.Vision = caps.Vision || static.Vision
	caps.JSONMode = caps.JSONMode || static.JSONMode

	// Integer capabilities: a non-zero static value wins; otherwise keep the
	// registry value (which itself may be zero/unknown).
	if static.MaxContextTokens != 0 {
		caps.MaxContextTokens = static.MaxContextTokens
	}
	if static.MaxOutputTokens != 0 {
		caps.MaxOutputTokens = static.MaxOutputTokens
	}

	// Embeddings/Batch are not represented in the registry's ModelCapabilities, so
	// they come straight from the static config (an explicit declaration the
	// registry must never override). Some providers support embeddings in a
	// deployment-/model-dependent way without a fixed DefaultEmbeddingModel
	// (e.g. Azure, llama.cpp), so the declaration is honored as-is.
	caps.Embeddings = static.Embeddings
	caps.Batch = static.Batch

	return caps
}

// Embed generates embeddings for one or more texts.
func (p *BaseProvider) Embed(ctx context.Context, texts []string, options ...llms.EmbedOption) (*llms.EmbeddingResponse, error) {
	if err := llms.ValidateEmbedInput(texts); err != nil {
		return nil, err
	}

	opts := llms.ApplyEmbedOptions(options...)

	model := p.embeddingModel
	if opts.Model != "" {
		model = opts.Model
	}
	if model == "" {
		model = p.config.DefaultEmbeddingModel
	}
	if model == "" {
		return nil, llms.ErrEmbeddingModelRequired
	}

	req := &EmbeddingRequest{
		Model:          model,
		Input:          texts,
		EncodingFormat: opts.EncodingFormat,
		Dimensions:     opts.Dimensions,
		User:           opts.User,
	}

	resp, err := p.client.CreateEmbedding(ctx, req)
	if err != nil {
		return nil, WrapError(p.config.Provider, "embed", err)
	}

	return ConvertEmbeddingResponse(resp), nil
}

// EmbedQuery embeds a single query text.
func (p *BaseProvider) EmbedQuery(ctx context.Context, text string, options ...llms.EmbedOption) ([]float32, error) {
	return llms.EmbedQuery(ctx, p, text, options...)
}

// EmbedDocuments embeds multiple documents.
func (p *BaseProvider) EmbedDocuments(ctx context.Context, texts []string, options ...llms.EmbedOption) ([][]float32, error) {
	return llms.EmbedDocuments(ctx, p, texts, options...)
}

// Client returns the underlying HTTP client (for providers that need direct access).
func (p *BaseProvider) Client() *Client {
	return p.client
}
