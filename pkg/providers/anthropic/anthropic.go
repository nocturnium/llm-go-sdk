// Package anthropic provides an Anthropic/Claude LLM implementation using native HTTP
package anthropic

import (
	"context"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/internal/anthropicapi"
)

const structuredOutputToolName = "structured_output"

// anthropicModelDeprecatesTemperature reports whether an Anthropic model rejects
// the `temperature` request parameter. The Claude Opus/Sonnet 4.x reasoning
// models (e.g. claude-opus-4-8) return HTTP 400 "temperature is deprecated for
// this model" when it is sent, so callers must omit it for those.
func anthropicModelDeprecatesTemperature(model string) bool {
	m := strings.ToLower(model)
	if !strings.Contains(m, "claude") {
		return false
	}
	return strings.Contains(m, "opus-4") ||
		strings.Contains(m, "-4-6") ||
		strings.Contains(m, "-4-7") ||
		strings.Contains(m, "-4-8")
}

// Client is an Anthropic LLM client.
//
// Thread-safety: All methods are safe for concurrent use. The same client
// can be shared across multiple goroutines without additional synchronization.
type Client struct {
	client  *anthropicapi.Client
	options *options
}

// New creates a new Anthropic client with the given options.
func New(opts ...Option) (*Client, error) {
	options := apply(opts...)

	// Resolve API key from explicit value or environment variables
	apiKey, err := llms.RequireAPIKey("anthropic", options.APIKey, llms.EnvAnthropicAPIKey)
	if err != nil {
		return nil, err
	}
	options.APIKey = apiKey

	clientConfig := anthropicapi.ClientConfig{
		BaseURL: options.BaseURL,
		APIKey:  options.APIKey,
	}

	if options.HTTPClient != nil {
		clientConfig.HTTPClient = options.HTTPClient
	}
	clientConfig.Timeout = options.Timeout
	clientConfig.AllowPrivateIPs = options.AllowPrivateIPs
	clientConfig.AllowHTTP = options.AllowHTTP

	client := anthropicapi.NewClient(clientConfig)

	return &Client{
		client:  client,
		options: options,
	}, nil
}

// Call sends a prompt to Anthropic and returns the response.
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

// GenerateContent generates content with more control over messages.
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

	structuredToolName := structuredOutputToolNameFor(opts)
	req, err := c.buildRequest(prepared, opts, false)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.CreateMessage(ctx, req)
	if err != nil {
		return nil, anthropicapi.WrapError("generate content", err)
	}

	result := convertResponse(resp, structuredToolName)

	// apply token estimation if enabled and usage is missing
	if opts.EstimateTokens && result.Usage.TotalTokens == 0 {
		result.Usage = llms.EstimateUsageFromMessages(prepared, result.Content)
	}

	return result, nil
}

// Provider returns the provider type.
func (c *Client) Provider() llms.Provider {
	return llms.ProviderAnthropic
}

// Model returns the model name.
func (c *Client) Model() string {
	return c.options.Model
}

// Capabilities returns the provider's capabilities.
func (c *Client) Capabilities() llms.Capabilities {
	caps := llms.GetModelCapabilities(llms.ProviderAnthropic, c.options.Model)
	result := caps.ToCapabilities()
	// Anthropic doesn't support embeddings or batch
	result.Embeddings = false
	result.Batch = false
	return result
}

// Stream generates content with streaming, returning chunks via channel.
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

	req, err := c.buildRequest(prepared, opts, true)
	if err != nil {
		return nil, err
	}

	stream, err := c.client.CreateMessageStream(ctx, req)
	if err != nil {
		return nil, anthropicapi.WrapError("stream", err)
	}

	bufferSize := llms.GetBufferSize(opts)
	chunks := make(chan llms.StreamChunk, bufferSize)

	// Capture variables for goroutine
	estimateTokens := opts.EstimateTokens

	go func() {
		defer close(chunks)
		defer func() { _ = stream.Close() }()

		sender := llms.NewStreamSender(ctx, chunks, opts.StreamSendTimeout)

		var accumulatedToolCalls []llms.ToolCall
		var accumulatedContent string // Full content for token estimation
		var accumulatedReasoning string
		var reasoningSignature string
		var currentToolCall *llms.ToolCall
		var currentToolArgs string
		var finishReason llms.FinishReason
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

			event, err := stream.Read()
			if err != nil {
				streamErr := &llms.StreamError{
					Cause:       anthropicapi.WrapError("stream read", err),
					BytesRead:   bytesRead,
					ChunksRead:  chunksRead,
					LastContent: lastContent,
				}
				sender.SendFinal(llms.StreamChunk{Error: streamErr})
				return
			}
			chunksRead++

			switch event.Type {
			case "message_start":
				if event.Message != nil && event.Message.Usage.InputTokens > 0 {
					usage = &llms.Usage{
						PromptTokens:        event.Message.Usage.InputTokens,
						CacheReadTokens:     event.Message.Usage.CacheReadInputTokens,
						CacheCreationTokens: event.Message.Usage.CacheCreationInputTokens,
					}
				}

			case "content_block_start":
				if event.ContentBlock != nil {
					if event.ContentBlock.Type == "tool_use" {
						currentToolCall = &llms.ToolCall{
							ID:   event.ContentBlock.ID,
							Type: llms.ToolTypeFunction,
							Function: &llms.FunctionCall{
								Name: event.ContentBlock.Name,
							},
						}
						currentToolArgs = ""
					}
				}

			case "content_block_delta":
				if event.Delta != nil {
					switch event.Delta.Type {
					case "text_delta":
						if event.Delta.Text != "" {
							lastContent = event.Delta.Text
							accumulatedContent += event.Delta.Text // Accumulate for estimation
							bytesRead += int64(len(event.Delta.Text))
							if sender.ForwardTerminalOnEarlyExit(sender.Send(llms.StreamChunk{Content: event.Delta.Text})) {
								// Consumer stopped reading or context canceled; a terminal
								// chunk has been delivered, abort.
								return
							}
						}
					case "input_json_delta":
						if event.Delta.PartialJSON != "" && currentToolCall != nil {
							currentToolArgs += event.Delta.PartialJSON
						}
					case "thinking_delta":
						if event.Delta.Thinking != "" {
							accumulatedReasoning += event.Delta.Thinking
							rc := &llms.ReasoningContent{Content: event.Delta.Thinking}
							if sender.ForwardTerminalOnEarlyExit(sender.Send(llms.StreamChunk{Reasoning: rc, Thinking: rc})) {
								return
							}
						}
					case "signature_delta":
						if event.Delta.Signature != "" {
							reasoningSignature += event.Delta.Signature
						}
					}
				}

			case "content_block_stop":
				if currentToolCall != nil {
					currentToolCall.Function.Arguments = currentToolArgs
					accumulatedToolCalls = append(accumulatedToolCalls, *currentToolCall)
					currentToolCall = nil
					currentToolArgs = ""
				}

			case "message_delta":
				if event.MessageDelta != nil {
					finishReason = convertStopReason(event.MessageDelta.StopReason)
				}
				if event.Usage != nil {
					if usage == nil {
						usage = &llms.Usage{}
					}
					usage.CompletionTokens = event.Usage.OutputTokens
					// Cache counts arrive at message_start; message_delta usually omits
					// them, so only overwrite when present to avoid zeroing them out.
					if event.Usage.CacheReadInputTokens > 0 {
						usage.CacheReadTokens = event.Usage.CacheReadInputTokens
					}
					if event.Usage.CacheCreationInputTokens > 0 {
						usage.CacheCreationTokens = event.Usage.CacheCreationInputTokens
					}
					usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
				}

			case "message_stop":
				// apply token estimation if enabled and usage is missing
				finalUsage := usage
				if estimateTokens && (usage == nil || usage.TotalTokens == 0) {
					estimated := llms.EstimateUsageFromMessages(prepared, accumulatedContent)
					finalUsage = &estimated
				}

				var finalReasoning *llms.ReasoningContent
				if accumulatedReasoning != "" || reasoningSignature != "" {
					finalReasoning = &llms.ReasoningContent{
						Content:   accumulatedReasoning,
						Signature: reasoningSignature,
					}
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
		}
	}()

	return chunks, nil
}

func (c *Client) buildRequest(messages []llms.Message, opts *llms.CallOptions, stream bool) (*anthropicapi.MessagesRequest, error) {
	model := c.options.Model
	if opts.Model != "" {
		model = opts.Model
	}

	maxTokens := 4096
	if opts.MaxTokens != nil {
		maxTokens = *opts.MaxTokens
	}
	if maxTokens == 0 {
		maxTokens = 4096 // Anthropic requires max_tokens
	}

	convertedMessages, err := convertMessages(messages)
	if err != nil {
		return nil, err
	}

	req := &anthropicapi.MessagesRequest{
		Model:         model,
		MaxTokens:     maxTokens,
		Messages:      convertedMessages,
		StopSequences: opts.StopWords,
		Stream:        stream,
	}
	// Newer Anthropic models (Claude Opus/Sonnet 4.x reasoning models) REJECT
	// the sampling parameters `temperature` AND `top_p` with HTTP 400
	// "<param> is deprecated for this model". Only send them for models that
	// still accept them; a nil pointer is omitted from the wire request so a
	// caller that never set the value lets the API apply its own default, while
	// an explicit value (including 0) is forwarded.
	if !anthropicModelDeprecatesTemperature(model) {
		if opts.Temperature != nil {
			req.Temperature = opts.Temperature
		}
		if opts.TopP != nil {
			req.TopP = opts.TopP
		}
	}

	// Extended thinking: when reasoning is requested (and we're not forcing a
	// single structured-output tool, which Anthropic rejects alongside thinking),
	// send a thinking budget. The model emits thinking content blocks before its
	// answer. Anthropic requires budget_tokens >= 1024, max_tokens > budget_tokens,
	// and that temperature/top_p be omitted, so we normalize those here.
	if opts.Reasoning.IsEnabled() && structuredOutputToolNameFor(opts) == "" {
		budget := opts.Reasoning.BudgetTokens
		if budget <= 0 {
			budget = llms.ReasoningBudgetForEffort(opts.Reasoning.Effort)
		}
		if budget <= 0 {
			budget = 4096 // Enabled with no effort/budget hint: a sensible default.
		}
		if budget < 1024 {
			budget = 1024 // Anthropic minimum.
		}
		if req.MaxTokens <= budget {
			req.MaxTokens = budget + 4096
		}
		req.Thinking = &anthropicapi.ThinkingConfig{Type: "enabled", BudgetTokens: budget}
		req.Temperature = nil
		req.TopP = nil
	}

	// Prompt caching. Caching the system prompt is on by default (preserving prior
	// behavior); WithCache/WithCacheTTL additionally cache the tool definitions,
	// and WithoutCache disables automatic caching. Per-message CacheControl marks
	// are applied in convertMessages regardless of this call-level setting.
	cacheEnabled := opts.Cache == nil || !opts.Cache.Disabled
	explicitCache := opts.Cache != nil && !opts.Cache.Disabled
	var cacheTTL string
	if opts.Cache != nil {
		cacheTTL = llms.AnthropicTTL(opts.Cache.TTL)
	}

	// Extract system message if present. Use Text so a system message
	// built from Parts (rather than the simple Content field) is not dropped.
	for _, msg := range messages {
		if msg.Role == llms.RoleSystem {
			sb := anthropicapi.SystemBlock{Type: "text", Text: msg.Text()}
			if cacheEnabled {
				sb.CacheControl = &anthropicapi.CacheControl{Type: "ephemeral", TTL: cacheTTL}
			}
			req.System = []anthropicapi.SystemBlock{sb}
			break
		}
	}

	// Anthropic has no response_format field. For JSON Schema structured output,
	// force a single tool and expose its input JSON as Response.Content.
	if name := structuredOutputToolNameFor(opts); name != "" {
		req.Tools = []anthropicapi.Tool{{
			Name:        name,
			Description: opts.ResponseFormat.JSONSchema.Description,
			InputSchema: opts.ResponseFormat.JSONSchema.Schema,
		}}
		req.ToolChoice = anthropicapi.ToolChoiceTool{
			Type: "tool",
			Name: name,
		}
		enforceCacheLimit(req)
		return req, nil
	}

	// Add tools if specified
	if len(opts.Tools) > 0 {
		req.Tools = convertTools(opts.Tools)
		// Cache the (stable) tool definitions by marking the last tool when the
		// caller explicitly enabled caching for this call.
		if explicitCache && len(req.Tools) > 0 {
			req.Tools[len(req.Tools)-1].CacheControl = &anthropicapi.CacheControl{Type: "ephemeral", TTL: cacheTTL}
		}
	}

	// Add tool choice if specified. Anthropic rejects extended thinking combined
	// with a forcing tool_choice (type "any" or "tool") — only "auto"/"none" are
	// allowed — so soften a forcing choice to "auto" when thinking is enabled,
	// keeping the request valid rather than failing with HTTP 400.
	if opts.ToolChoice != nil {
		if req.Thinking != nil && (opts.ToolChoice.Type == llms.ToolChoiceRequired || opts.ToolChoice.Function != nil) {
			req.ToolChoice = anthropicapi.ToolChoiceAuto{Type: "auto"}
		} else {
			req.ToolChoice = convertToolChoice(opts.ToolChoice)
		}
	}

	enforceCacheLimit(req)
	return req, nil
}

// enforceCacheLimit caps the number of cache_control breakpoints at Anthropic's
// maximum of four. Breakpoints are kept in priority order — system block, tool
// definitions, then message content top-to-bottom — and any beyond the limit are
// dropped so the request never fails with "too many cache breakpoints".
func enforceCacheLimit(req *anthropicapi.MessagesRequest) {
	const maxBreakpoints = 4
	count := 0
	keep := func(cc **anthropicapi.CacheControl) {
		if *cc == nil {
			return
		}
		if count >= maxBreakpoints {
			*cc = nil
			return
		}
		count++
	}
	for i := range req.System {
		keep(&req.System[i].CacheControl)
	}
	for i := range req.Tools {
		keep(&req.Tools[i].CacheControl)
	}
	for i := range req.Messages {
		for j := range req.Messages[i].Content {
			keep(&req.Messages[i].Content[j].CacheControl)
		}
	}
}

func structuredOutputToolNameFor(opts *llms.CallOptions) string {
	if opts.ResponseFormat == nil ||
		opts.ResponseFormat.Type != llms.ResponseFormatJSONSchema ||
		opts.ResponseFormat.JSONSchema == nil {
		return ""
	}
	if opts.ResponseFormat.JSONSchema.Name != "" {
		return opts.ResponseFormat.JSONSchema.Name
	}
	return structuredOutputToolName
}

// Ensure Client implements the LLM interface.
var _ llms.LLM = (*Client)(nil)
