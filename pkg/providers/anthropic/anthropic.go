// Package anthropic provides an Anthropic/Claude LLM implementation using native HTTP
package anthropic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/anthropicapi"
)

const structuredOutputToolName = "structured_output"

// modelGeneration classifies a Claude model by the request surface it accepts.
// The Messages API changed shape several times across the 4.x/5.x families, so
// the request builder keys a handful of decisions off this.
type modelGeneration int

const (
	// genLegacy covers Claude 3.x, 4, 4.1 and 4.5 (Opus/Sonnet/Haiku): thinking
	// is {type:"enabled", budget_tokens}; temperature/top_p are accepted.
	genLegacy modelGeneration = iota
	// gen46 covers Opus 4.6 and Sonnet 4.6: adaptive thinking is the on-mode
	// (budget_tokens deprecated); temperature/top_p still accepted.
	gen46
	// gen47 covers Opus 4.7, Opus 4.8, Opus 5 and Sonnet 5: adaptive thinking is
	// the only on-mode; budget_tokens, temperature and top_p are rejected (400).
	gen47
	// genAlwaysOn covers Fable and Mythos: thinking is always on and the
	// thinking parameter must be omitted; sampling params are rejected.
	genAlwaysOn
)

func classifyModel(model string) modelGeneration {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "fable"), strings.Contains(m, "mythos"):
		return genAlwaysOn
	case strings.Contains(m, "opus-4-7"), strings.Contains(m, "opus-4-8"),
		strings.Contains(m, "opus-5"), strings.Contains(m, "sonnet-5"):
		return gen47
	case strings.Contains(m, "opus-4-6"), strings.Contains(m, "sonnet-4-6"):
		return gen46
	default:
		return genLegacy
	}
}

// anthropicModelDeprecatesTemperature reports whether an Anthropic model rejects
// the `temperature`/`top_p` request parameters with HTTP 400. Opus 4.7/4.8, the
// 5-family and Fable/Mythos removed sampling parameters; Opus 4.6 / Sonnet 4.6
// and earlier still accept them.
func anthropicModelDeprecatesTemperature(model string) bool {
	switch classifyModel(model) {
	case gen47, genAlwaysOn:
		return true
	default:
		return false
	}
}

// rejectsForcedToolChoice reports whether the model returns HTTP 400 for
// tool_choice type "any"/"tool" (Fable 5.1 and the Mythos line).
func rejectsForcedToolChoice(model string) bool {
	m := strings.ToLower(model)
	return strings.Contains(m, "fable-5-1") || strings.Contains(m, "mythos")
}

// effortForModel maps the neutral effort onto Anthropic's output_config.effort.
// "minimal" has no Anthropic equivalent and maps to "low"; an unknown value is
// forwarded as-is so newer levels (xhigh, max) pass through.
func effortForModel(effort llms.ReasoningEffort) string {
	if effort == llms.ReasoningEffortMinimal {
		return "low"
	}
	return string(effort)
}

// Client is an Anthropic LLM client.
//
// Thread-safety: All methods are safe for concurrent use. The same client
// can be shared across multiple goroutines without additional synchronization.
type Client struct {
	client  *anthropicapi.Client
	options *options
}

var readStreamEvent = func(stream *anthropicapi.StreamReader) (*anthropicapi.StreamEvent, error) {
	return stream.Read()
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

// prepare runs the option and message validation shared by GenerateContent and
// Stream, mirroring the openaicompat base provider so all providers reject the
// same malformed inputs (mid-conversation system messages, tool messages with
// no ToolCallID).
func prepare(messages []llms.Message, opts *llms.CallOptions) ([]llms.Message, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	// Merge consecutive same-role messages unless disabled, then validate.
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
	return prepared, nil
}

// GenerateContent generates content with more control over messages.
func (c *Client) GenerateContent(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.Response, error) {
	opts := llms.ApplyOptions(options...)

	prepared, err := prepare(messages, opts)
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
	// Anthropic doesn't support embeddings. The Message Batches API is not
	// wired through this client, so Batch is reported false too.
	result.Embeddings = false
	result.Batch = false
	return result
}

// Stream generates content with streaming, returning chunks via channel.
func (c *Client) Stream(ctx context.Context, messages []llms.Message, options ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	opts := llms.ApplyOptions(options...)

	prepared, err := prepare(messages, opts)
	if err != nil {
		return nil, err
	}

	structuredToolName := structuredOutputToolNameFor(opts)
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
		sender := llms.NewStreamSender(ctx, chunks, opts.StreamSendTimeout)

		defer close(chunks)
		// A malformed/hostile provider response must never crash the host process.
		// Convert any panic in stream processing into a terminal error chunk.
		defer func() {
			if r := recover(); r != nil {
				sender.DeliverTerminal(llms.StreamChunk{
					Error: fmt.Errorf("anthropic: panic during stream processing: %v", r),
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
		var reasoningSignature string
		var redactedThinking []string
		var reasoningDeltaEmitted bool
		var currentToolCall *llms.ToolCall
		var currentToolArgs string
		var finishReason llms.FinishReason
		var usage *llms.Usage
		var bytesRead int64
		var chunksRead int
		var lastContent string

		// finish delivers the terminal chunk. It runs on message_stop and, like
		// the other providers, on a clean EOF so a proxy that closes the
		// connection after the last event is not reported as an error.
		finish := func() {
			// A tool_use block that never received content_block_stop (EOF
			// mid-block) is still surfaced rather than dropped.
			if currentToolCall != nil {
				currentToolCall.Function.Arguments = normalizeToolArgs(currentToolArgs)
				accumulatedToolCalls = append(accumulatedToolCalls, *currentToolCall)
				currentToolCall = nil
			}

			// Structured output: the forced tool's input JSON is the content.
			// Mirrors convertResponse so Stream and GenerateContent agree.
			finalContent := ""
			if structuredToolName != "" && accumulatedContent == "" {
				for _, tc := range accumulatedToolCalls {
					if tc.Function != nil && tc.Function.Name == structuredToolName {
						finalContent = tc.Function.Arguments
						accumulatedContent = finalContent
						break
					}
				}
			}

			// apply token estimation if enabled and usage is missing
			finalUsage := usage
			if estimateTokens && (usage == nil || usage.TotalTokens == 0) {
				estimated := llms.EstimateUsageFromMessages(prepared, accumulatedContent)
				finalUsage = &estimated
			}

			var finalReasoning *llms.ReasoningContent
			switch {
			case !reasoningDeltaEmitted && (accumulatedReasoning != "" || reasoningSignature != ""):
				// No thinking deltas were streamed: deliver the full reasoning
				// text plus its signature in one terminal chunk.
				finalReasoning = &llms.ReasoningContent{
					Content:   accumulatedReasoning,
					Signature: reasoningSignature,
				}
			case reasoningSignature != "":
				// Thinking deltas already streamed the text, but the signature
				// arrives at the end; deliver it on the terminal chunk (content
				// empty to avoid duplicating the streamed text) so the
				// extended-thinking block can be replayed on the next turn.
				finalReasoning = &llms.ReasoningContent{Signature: reasoningSignature}
			}
			if len(redactedThinking) > 0 {
				if finalReasoning == nil {
					finalReasoning = &llms.ReasoningContent{}
				}
				finalReasoning.Metadata = map[string]any{redactedThinkingMetadataKey: redactedThinking}
			}

			sender.SendFinal(llms.StreamChunk{
				Content:      finalContent,
				Reasoning:    finalReasoning,
				ToolCalls:    accumulatedToolCalls,
				FinishReason: finishReason,
				Usage:        finalUsage,
			})
		}

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

			event, err := readStreamEvent(stream)
			if err != nil {
				if ctx.Err() != nil {
					sender.ForwardTerminalOnEarlyExit(llms.SendContextCanceled)
					return
				}
				if errors.Is(err, io.EOF) {
					finish()
					return
				}
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
					u := convertUsage(event.Message.Usage)
					u.CompletionTokens = 0
					u.TotalTokens = 0
					usage = &u
				}

			case "content_block_start":
				if event.ContentBlock != nil {
					switch event.ContentBlock.Type {
					case contentTypeToolUse:
						currentToolCall = &llms.ToolCall{
							ID:   event.ContentBlock.ID,
							Type: llms.ToolTypeFunction,
							Function: &llms.FunctionCall{
								Name: event.ContentBlock.Name,
							},
						}
						currentToolArgs = ""
					case contentTypeRedactedThinking:
						if event.ContentBlock.Data != "" {
							redactedThinking = append(redactedThinking, event.ContentBlock.Data)
						}
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
							reasoningDeltaEmitted = true
							if sender.ForwardTerminalOnEarlyExit(sender.Send(llms.StreamChunk{Reasoning: rc})) {
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
					// A tool with no arguments streams no input_json_delta at all;
					// normalize to "{}" so the arguments always parse, matching
					// the non-streaming path and the other providers.
					currentToolCall.Function.Arguments = normalizeToolArgs(currentToolArgs)
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
					usage.TotalTokens = totalTokens(*usage)
				}

			case "message_stop":
				finish()
				return
			}
		}
	}()

	return chunks, nil
}

// normalizeToolArgs returns "{}" for an empty streamed tool input so
// FunctionCall.Arguments is always valid JSON.
func normalizeToolArgs(args string) string {
	if strings.TrimSpace(args) == "" {
		return "{}"
	}
	return args
}

func (c *Client) buildRequest(messages []llms.Message, opts *llms.CallOptions, stream bool) (*anthropicapi.MessagesRequest, error) {
	model := c.options.Model
	if opts.Model != "" {
		model = opts.Model
	}
	gen := classifyModel(model)

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
	// Opus 4.7/4.8, the 5-family and Fable/Mythos REJECT the sampling parameters
	// `temperature` AND `top_p` with HTTP 400. Only send them for models that
	// still accept them; a nil pointer is omitted from the wire request so a
	// caller that never set the value lets the API apply its own default, while
	// an explicit value (including 0) is forwarded. FrequencyPenalty and
	// PresencePenalty have no Anthropic equivalent and are never sent.
	if !anthropicModelDeprecatesTemperature(model) {
		if opts.Temperature != nil {
			req.Temperature = opts.Temperature
		}
		if opts.TopP != nil {
			req.TopP = opts.TopP
		}
	}

	// Pass through ExtraBody for Anthropic-specific extensions (metadata, beta
	// fields, service_tier, ...). Copied so the request never aliases the
	// caller's map; standard fields win over a colliding key.
	if len(opts.ExtraBody) > 0 {
		eb := make(map[string]any, len(opts.ExtraBody))
		for k, v := range opts.ExtraBody {
			eb[k] = v
		}
		req.ExtraBody = eb
	}

	structuredName := structuredOutputToolNameFor(opts)
	applyThinking(req, gen, opts.Reasoning, structuredName != "")

	// Prompt caching. Caching the system prompt is on by default (preserving prior
	// behavior); WithCache/WithCacheTTL additionally cache the tool definitions,
	// and WithoutCache disables automatic caching. Per-message CacheControl marks
	// are applied in convertMessages regardless of this call-level setting.
	cacheEnabled := opts.Cache == nil || !opts.Cache.Disabled
	explicitCache := opts.Cache != nil && !opts.Cache.Disabled
	var cacheTTL string
	if opts.Cache != nil {
		cacheTTL = cacheControlTTL(opts.Cache.TTL)
	}

	// Extract system message if present. Use Text so a system message
	// built from Parts (rather than the simple Content field) is not dropped.
	// ValidateInlineSystem guarantees at most one system message, at index 0.
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

	// Anthropic has no response_format field. For JSON output (schema or
	// json_object mode), force a single tool and expose its input JSON as
	// Response.Content.
	if structuredName != "" {
		tool := anthropicapi.Tool{
			Name:        structuredName,
			InputSchema: structuredOutputSchema(opts),
		}
		if opts.ResponseFormat.JSONSchema != nil {
			tool.Description = opts.ResponseFormat.JSONSchema.Description
		}
		if tool.Description == "" {
			tool.Description = "Record the response as a JSON object."
		}
		req.Tools = []anthropicapi.Tool{tool}
		if rejectsForcedToolChoice(model) {
			// Fable 5.1 / Mythos return 400 for a forcing tool_choice; fall back
			// to auto plus an explicit instruction so the model still emits the
			// tool call.
			req.ToolChoice = anthropicapi.ToolChoiceAuto{Type: "auto"}
			req.System = append(req.System, anthropicapi.SystemBlock{
				Type: "text",
				Text: "You must respond by calling the " + structuredName + " tool exactly once with the complete answer; do not reply with prose.",
			})
		} else {
			req.ToolChoice = anthropicapi.ToolChoiceTool{Type: "tool", Name: structuredName}
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
	// allowed — and Fable 5.1 / Mythos reject forcing choices outright, so
	// soften a forcing choice to "auto" in those cases, keeping the request
	// valid rather than failing with HTTP 400.
	if opts.ToolChoice != nil {
		forcing := opts.ToolChoice.Mode == llms.ToolChoiceRequired || opts.ToolChoice.Mode == llms.ToolChoiceTool
		thinkingOn := req.Thinking != nil && req.Thinking.Type != "disabled" || gen == genAlwaysOn
		if forcing && (thinkingOn || rejectsForcedToolChoice(model)) {
			req.ToolChoice = anthropicapi.ToolChoiceAuto{Type: "auto"}
		} else {
			req.ToolChoice = convertToolChoice(opts.ToolChoice)
		}
	}

	enforceCacheLimit(req)
	return req, nil
}

// applyThinking translates the neutral ReasoningConfig onto the request using
// the shape the model generation accepts:
//
//   - legacy (pre-4.6): {type:"enabled", budget_tokens} with budget >= 1024 and
//     max_tokens > budget; temperature/top_p must be omitted.
//   - 4.6 / 4.7+: {type:"adaptive"} plus output_config.effort; an explicit
//     Enabled=false sends {type:"disabled"}. BudgetTokens is ignored (the API
//     rejects it on 4.7+).
//   - Fable/Mythos: thinking is always on and the parameter is omitted; only
//     output_config.effort is sent. Enabled=false is ignored (400 otherwise).
//
// A forced structured-output tool cannot be combined with thinking (Anthropic
// rejects a forcing tool_choice alongside thinking), so thinking is skipped in
// that case on generations where it can be turned off.
func applyThinking(req *anthropicapi.MessagesRequest, gen modelGeneration, rc *llms.ReasoningConfig, structured bool) {
	if rc == nil {
		return
	}
	enabled := rc.IsEnabled()
	explicitOff := rc.Enabled != nil && !*rc.Enabled

	switch gen {
	case genAlwaysOn:
		if enabled && rc.Effort != "" {
			req.OutputConfig = &anthropicapi.OutputConfig{Effort: effortForModel(rc.Effort)}
		}
		return

	case gen46, gen47:
		if explicitOff {
			req.Thinking = &anthropicapi.ThinkingConfig{Type: "disabled"}
			return
		}
		if !enabled || structured {
			return
		}
		req.Thinking = &anthropicapi.ThinkingConfig{Type: "adaptive"}
		if rc.Effort != "" {
			req.OutputConfig = &anthropicapi.OutputConfig{Effort: effortForModel(rc.Effort)}
		}
		req.Temperature = nil
		req.TopP = nil
		return

	default: // genLegacy
		if !enabled || structured {
			return
		}
		budget := rc.BudgetTokens
		if budget <= 0 {
			budget = llms.ReasoningBudgetForEffort(rc.Effort)
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

// structuredOutputToolNameFor returns the name of the tool used to force JSON
// output, or "" when no JSON response format was requested. Both json_schema
// and json_object formats are served through a forced tool since Anthropic has
// no response_format field.
func structuredOutputToolNameFor(opts *llms.CallOptions) string {
	if opts.ResponseFormat == nil {
		return ""
	}
	switch opts.ResponseFormat.Type {
	case llms.ResponseFormatJSONSchema:
		if opts.ResponseFormat.JSONSchema == nil {
			return ""
		}
		if opts.ResponseFormat.JSONSchema.Name != "" {
			return opts.ResponseFormat.JSONSchema.Name
		}
		return structuredOutputToolName
	case llms.ResponseFormatJSONObject:
		return structuredOutputToolName
	default:
		return ""
	}
}

// structuredOutputSchema returns the input_schema for the forced JSON tool: the
// caller's schema, or a permissive object schema for json_object mode.
func structuredOutputSchema(opts *llms.CallOptions) any {
	if opts.ResponseFormat.JSONSchema != nil && len(opts.ResponseFormat.JSONSchema.Schema) > 0 {
		return opts.ResponseFormat.JSONSchema.Schema
	}
	return map[string]any{"type": "object", "additionalProperties": true}
}

// Ensure Client implements the LLM interface.
var _ llms.LLM = (*Client)(nil)
