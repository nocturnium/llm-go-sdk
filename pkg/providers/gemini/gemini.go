// Package gemini provides a Google Gemini LLM implementation using native HTTP
package gemini

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v3"
	"github.com/nocturnium/llm-go-sdk/v3/internal/geminiapi"
)

// Client is a Google Gemini LLM client.
//
// Thread-safety: All methods are safe for concurrent use. The same client
// can be shared across multiple goroutines without additional synchronization.
type Client struct {
	client  *geminiapi.Client
	options *options
}

var readStreamChunk = func(stream *geminiapi.StreamReader) (*geminiapi.StreamChunk, error) {
	return stream.Read()
}

// New creates a new Gemini client with the given options
func New(opts ...Option) (*Client, error) {
	options := apply(opts...)

	// Resolve API key from explicit value or environment variables
	// Checks GEMINI_API_KEY first, then GOOGLE_API_KEY as alternative
	apiKey, err := llms.RequireAPIKey("gemini", options.APIKey, llms.EnvGeminiAPIKey, llms.EnvGoogleAPIKey)
	if err != nil {
		return nil, err
	}
	options.APIKey = apiKey

	clientConfig := geminiapi.ClientConfig{
		APIKey:  options.APIKey,
		Model:   options.Model,
		BaseURL: options.BaseURL,
	}

	if options.HTTPClient != nil {
		clientConfig.HTTPClient = options.HTTPClient
	}
	clientConfig.Timeout = options.Timeout
	clientConfig.AllowPrivateIPs = options.AllowPrivateIPs
	clientConfig.AllowHTTP = options.AllowHTTP

	client := geminiapi.NewClient(clientConfig)

	return &Client{
		client:  client,
		options: options,
	}, nil
}

// Call sends a prompt to Gemini and returns the response
func (c *Client) Call(ctx context.Context, prompt string, options ...llms.CallOption) (string, error) {
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: prompt},
	}

	resp, err := c.GenerateContent(ctx, messages, options...)
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}

// GenerateContent generates content with more control over messages
func (c *Client) GenerateContent(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.Response, error) {
	opts := llms.ApplyOptions(options...)

	// Validate options
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	// Prepare messages (merge consecutive same-role messages unless disabled, then validate)
	prepared, err := llms.PrepareMessages(messages, opts)
	if err != nil {
		return nil, err
	}

	req := c.buildRequest(prepared, opts)

	model := c.options.Model
	if opts.Model != "" {
		model = opts.Model
	}

	resp, err := c.client.GenerateContent(ctx, model, req)
	if err != nil {
		return nil, geminiapi.WrapError("generate content", err)
	}

	if err := validateNonEmptyFinish(resp); err != nil {
		return nil, err
	}

	result := convertResponse(resp)

	// apply token estimation if enabled and usage is missing
	if opts.EstimateTokens && result.Usage.TotalTokens == 0 {
		result.Usage = llms.EstimateUsageFromMessages(prepared, result.Content)
	}

	return result, nil
}

// Provider returns the provider type
func (c *Client) Provider() llms.Provider {
	return llms.ProviderGemini
}

// Model returns the model name
func (c *Client) Model() string {
	return c.options.Model
}

// Capabilities returns the provider's capabilities
func (c *Client) Capabilities() llms.Capabilities {
	caps := llms.GetModelCapabilities(llms.ProviderGemini, c.options.Model)
	result := caps.ToCapabilities()
	// Gemini supports embeddings but not batch
	result.Embeddings = true
	result.Batch = false
	return result
}

// Stream generates content with streaming, returning chunks via channel
func (c *Client) Stream(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	opts := llms.ApplyOptions(options...)

	// Validate options
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	// Prepare messages (merge consecutive same-role messages unless disabled, then validate)
	prepared, err := llms.PrepareMessages(messages, opts)
	if err != nil {
		return nil, err
	}

	req := c.buildRequest(prepared, opts)

	model := c.options.Model
	if opts.Model != "" {
		model = opts.Model
	}

	stream, err := c.client.GenerateContentStream(ctx, model, req)
	if err != nil {
		return nil, geminiapi.WrapError("stream", err)
	}

	bufferSize := llms.GetBufferSize(opts)
	chunks := make(chan llms.StreamChunk, bufferSize)

	// Capture variables for goroutine
	estimateTokens := opts.EstimateTokens

	go func() {
		sender := llms.NewStreamSender(ctx, chunks, opts.StreamSendTimeout)

		defer close(chunks)
		// A malformed/hostile provider response must never crash the host process.
		// Convert any panic in stream processing into a terminal error chunk.
		defer func() {
			if r := recover(); r != nil {
				sender.DeliverTerminal(llms.StreamChunk{
					Error: fmt.Errorf("gemini: panic during stream processing: %v", r),
				})
			}
		}()
		defer func() { _ = stream.Close() }()

		readDone := make(chan struct{})
		defer close(readDone)
		go func() {
			select {
			case <-ctx.Done():
				_ = stream.Close()
			case <-readDone:
			}
		}()

		var accumulatedToolCalls []llms.ToolCall
		var accumulatedContent string // Full content for token estimation
		var accumulatedReasoning string
		var reasoningDeltaEmitted bool
		var finishReason llms.FinishReason
		var rawFinishReason string
		var usage *llms.Usage
		var bytesRead int64
		var chunksRead int
		var lastContent string

		for {
			select {
			case <-ctx.Done():
				// Context canceled or deadline exceeded. Deliver a terminal chunk
				// carrying the context error so a truncated stream is never mistaken
				// for a successful completion.
				sender.ForwardTerminalOnEarlyExit(llms.SendContextCanceled)
				return
			default:
			}

			chunk, err := readStreamChunk(stream)
			if errors.Is(err, io.EOF) {
				if ctx.Err() != nil {
					sender.DeliverTerminal(llms.StreamChunk{Error: ctx.Err(), Done: true})
					return
				}
				// apply token estimation if enabled and usage is missing
				finalUsage := usage
				if estimateTokens && (usage == nil || usage.TotalTokens == 0) {
					estimated := llms.EstimateUsageFromMessages(prepared, accumulatedContent)
					finalUsage = &estimated
				}

				var finalReasoning *llms.ReasoningContent
				if !reasoningDeltaEmitted && accumulatedReasoning != "" {
					finalReasoning = &llms.ReasoningContent{Content: accumulatedReasoning}
					if finalUsage != nil {
						finalReasoning.Tokens = finalUsage.ReasoningTokens
					}
				}

				// Gemini reports STOP even with function calls; normalize.
				if len(accumulatedToolCalls) > 0 {
					finishReason = llms.FinishReasonToolCalls
				}

				reason := strings.ToUpper(rawFinishReason)
				hasOutput := accumulatedContent != "" || len(accumulatedToolCalls) > 0 || accumulatedReasoning != ""
				if !hasOutput && reason != "" && reason != "STOP" {
					sender.SendFinal(llms.StreamChunk{
						Error:        fmt.Errorf("gemini: candidate finished with %s and no content", rawFinishReason),
						FinishReason: finishReason,
						Usage:        finalUsage,
					})
					return
				}

				sender.SendFinal(llms.StreamChunk{
					Reasoning:    finalReasoning,
					Thinking:     finalReasoning,
					ToolCalls:    accumulatedToolCalls,
					FinishReason: finishReason,
					Usage:        finalUsage,
				})
				return
			}
			if err != nil {
				if ctx.Err() != nil {
					sender.DeliverTerminal(llms.StreamChunk{Error: ctx.Err(), Done: true})
					return
				}
				streamErr := &llms.StreamError{
					Cause:       geminiapi.WrapError("stream read", err),
					BytesRead:   bytesRead,
					ChunksRead:  chunksRead,
					LastContent: lastContent,
				}
				sender.SendFinal(llms.StreamChunk{Error: streamErr})
				return
			}
			chunksRead++

			if len(chunk.Candidates) > 0 {
				candidate := chunk.Candidates[0]

				if candidate.Content != nil {
					// Emit reasoning ("thought") content as it streams.
					if thought := geminiapi.ExtractThoughtContent(candidate.Content.Parts); thought != "" {
						accumulatedReasoning += thought
						reasoningDeltaEmitted = true
						rc := &llms.ReasoningContent{Content: thought}
						if sender.ForwardTerminalOnEarlyExit(sender.Send(llms.StreamChunk{Reasoning: rc, Thinking: rc})) {
							return
						}
					}

					// Extract text content
					text := geminiapi.ExtractTextContent(candidate.Content.Parts)
					if text != "" {
						lastContent = text
						accumulatedContent += text // Accumulate for estimation
						bytesRead += int64(len(text))
						if sender.ForwardTerminalOnEarlyExit(sender.Send(llms.StreamChunk{Content: text})) {
							// Consumer stopped reading or context canceled; a terminal
							// chunk has been delivered, abort.
							return
						}
					}

					// Extract function calls
					for _, part := range candidate.Content.Parts {
						if part.FunctionCall != nil {
							tc := llms.ToolCall{
								ID:   part.FunctionCall.Name, // Gemini doesn't have IDs
								Type: llms.ToolTypeFunction,
								Function: &llms.FunctionCall{
									Name:      part.FunctionCall.Name,
									Arguments: geminiapi.FunctionCallToJSON(part.FunctionCall),
								},
							}
							accumulatedToolCalls = append(accumulatedToolCalls, tc)
						}
					}
				}

				if candidate.FinishReason != "" {
					rawFinishReason = candidate.FinishReason
					finishReason = llms.FinishReason(geminiapi.GetFinishReason(candidate.FinishReason))
				}
			}

			if chunk.UsageMetadata != nil {
				u := convertUsageMetadata(chunk.UsageMetadata)
				usage = &u
			}
		}
	}()

	return chunks, nil
}

func validateNonEmptyFinish(resp *geminiapi.GenerateContentResponse) error {
	if resp == nil || len(resp.Candidates) == 0 {
		return nil
	}
	candidate := resp.Candidates[0]
	reason := strings.ToUpper(candidate.FinishReason)
	if reason == "" || reason == "STOP" || candidateHasOutput(candidate) {
		return nil
	}
	return fmt.Errorf("gemini: candidate finished with %s and no content", candidate.FinishReason)
}

func candidateHasOutput(candidate geminiapi.Candidate) bool {
	if candidate.Content == nil {
		return false
	}
	if geminiapi.ExtractTextContent(candidate.Content.Parts) != "" {
		return true
	}
	if geminiapi.ExtractThoughtContent(candidate.Content.Parts) != "" {
		return true
	}
	return geminiapi.HasFunctionCalls(candidate.Content)
}

func splitSystemInstruction(messages []llms.Message) (*geminiapi.Content, []llms.Message) {
	contents := make([]llms.Message, 0, len(messages))
	var systemParts []string
	for _, msg := range messages {
		if msg.Role != llms.RoleSystem {
			contents = append(contents, msg)
			continue
		}
		if msg.Content != "" {
			systemParts = append(systemParts, msg.Content)
			continue
		}
		for _, part := range msg.Parts {
			if part.Type == llms.PartTypeText && part.Text != "" {
				systemParts = append(systemParts, part.Text)
			}
		}
	}
	if len(systemParts) == 0 {
		return nil, contents
	}
	return &geminiapi.Content{
		Parts: []geminiapi.Part{{Text: strings.Join(systemParts, "\n\n")}},
	}, contents
}

func (c *Client) buildRequest(messages []llms.Message, opts *llms.CallOptions) *geminiapi.GenerateContentRequest {
	systemInstruction, contents := splitSystemInstruction(messages)
	req := &geminiapi.GenerateContentRequest{
		SystemInstruction: systemInstruction,
		Contents:          convertMessages(contents),
	}

	// Add generation config. Temperature/TopP are forwarded as pointers so an
	// explicit value (including 0) is sent while an unset value is omitted.
	config := &geminiapi.GenerationConfig{
		Temperature:   opts.Temperature,
		TopP:          opts.TopP,
		StopSequences: opts.StopWords,
	}
	if opts.MaxTokens != nil {
		config.MaxOutputTokens = *opts.MaxTokens
	}

	// Add JSON response formats.
	if opts.ResponseFormat != nil {
		switch opts.ResponseFormat.Type {
		case llms.ResponseFormatJSONSchema:
			config.ResponseMimeType = "application/json"
			if opts.ResponseFormat.JSONSchema != nil {
				config.ResponseSchema = opts.ResponseFormat.JSONSchema.Schema
			}
		case llms.ResponseFormatJSONObject:
			config.ResponseMimeType = "application/json"
		case llms.ResponseFormatText:
		}
	}

	// Thinking config: enable thought summaries and apply a budget when reasoning
	// is requested, or explicitly disable thinking (budget 0) when asked.
	if rc := opts.Reasoning; rc != nil {
		switch {
		case rc.IsEnabled():
			tcfg := &geminiapi.ThinkingConfig{IncludeThoughts: true}
			budget := rc.BudgetTokens
			if budget <= 0 {
				budget = llms.ReasoningBudgetForEffort(rc.Effort)
			}
			if budget > 0 {
				b := budget
				tcfg.ThinkingBudget = &b
			}
			config.ThinkingConfig = tcfg
		case rc.Enabled != nil && !*rc.Enabled:
			zero := 0
			config.ThinkingConfig = &geminiapi.ThinkingConfig{ThinkingBudget: &zero}
		}
	}

	req.GenerationConfig = config

	// Add tools if specified
	if len(opts.Tools) > 0 {
		req.Tools = convertTools(opts.Tools)
	}

	return req
}

// Embed generates embeddings for one or more texts
func (c *Client) Embed(ctx context.Context, texts []string, options ...llms.EmbedOption) (*llms.EmbeddingResponse, error) {
	if err := llms.ValidateEmbedInput(texts); err != nil {
		return nil, err
	}

	opts := llms.ApplyEmbedOptions(options...)

	model := c.options.EmbeddingModel
	if opts.Model != "" {
		model = opts.Model
	}
	if model == "" {
		model = "text-embedding-004" // Default Gemini embedding model
	}

	// Use batch API for multiple texts
	if len(texts) > 1 {
		return c.embedBatch(ctx, model, texts, opts)
	}

	// Validate we have at least one text
	if len(texts) == 0 {
		return nil, fmt.Errorf("gemini: no texts provided for embedding")
	}

	// Single text embedding
	req := &geminiapi.EmbedContentRequest{
		Content: geminiapi.EmbeddingContent{
			Parts: []geminiapi.EmbeddingPart{{Text: texts[0]}},
		},
		TaskType: opts.TaskType,
	}

	resp, err := c.client.EmbedContent(ctx, model, req)
	if err != nil {
		return nil, geminiapi.WrapError("embed", err)
	}

	return &llms.EmbeddingResponse{
		Embeddings: []llms.Embedding{
			{
				Index:  0,
				Vector: resp.Embedding.Values,
				Object: "embedding",
			},
		},
		Model: model,
	}, nil
}

func (c *Client) embedBatch(ctx context.Context, model string, texts []string, opts *llms.EmbedOptions) (*llms.EmbeddingResponse, error) {
	requests := make([]geminiapi.EmbedContentRequest, len(texts))
	for i, text := range texts {
		requests[i] = geminiapi.EmbedContentRequest{
			Model: "models/" + model,
			Content: geminiapi.EmbeddingContent{
				Parts: []geminiapi.EmbeddingPart{{Text: text}},
			},
			TaskType: opts.TaskType,
		}
	}

	resp, err := c.client.BatchEmbedContents(ctx, model, &geminiapi.BatchEmbedContentsRequest{
		Requests: requests,
	})
	if err != nil {
		return nil, geminiapi.WrapError("embed batch", err)
	}

	embeddings := make([]llms.Embedding, len(resp.Embeddings))
	for i, emb := range resp.Embeddings {
		embeddings[i] = llms.Embedding{
			Index:  i,
			Vector: emb.Values,
			Object: "embedding",
		}
	}

	return &llms.EmbeddingResponse{
		Embeddings: embeddings,
		Model:      model,
	}, nil
}

// EmbedQuery embeds a single query text
func (c *Client) EmbedQuery(ctx context.Context, text string, options ...llms.EmbedOption) ([]float32, error) {
	// Use RETRIEVAL_QUERY task type for queries by default
	opts := llms.ApplyEmbedOptions(options...)
	if opts.TaskType == "" {
		options = append(options, llms.WithTaskType(llms.TaskTypeRetrievalQuery))
	}

	return llms.EmbedQuery(ctx, c, text, options...)
}

// EmbedDocuments embeds multiple documents
func (c *Client) EmbedDocuments(ctx context.Context, texts []string, options ...llms.EmbedOption) ([][]float32, error) {
	// Use RETRIEVAL_DOCUMENT task type for documents by default
	opts := llms.ApplyEmbedOptions(options...)
	if opts.TaskType == "" {
		options = append(options, llms.WithTaskType(llms.TaskTypeRetrievalDocument))
	}

	return llms.EmbedDocuments(ctx, c, texts, options...)
}

// Ensure Client implements the LLM interface
var _ llms.LLM = (*Client)(nil)

// Ensure Client implements the Embedder interface
var _ llms.Embedder = (*Client)(nil)
