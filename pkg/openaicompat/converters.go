package openaicompat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v5"
	"github.com/nocturnium/llm-go-sdk/v5/internal/httpclient"
)

// isOpenAIReasoningModel reports whether model is an OpenAI reasoning model that
// requires max_completion_tokens and rejects temperature/top_p/penalties. Matches
// the o-series (o1/o3/o4/o5…) and the gpt-5 family by name. Other providers'
// reasoning models (e.g. deepseek-reasoner, qwen) do not use this OpenAI-specific
// wire shape, so name-matching here does not affect them.
func isOpenAIReasoningModel(model string) bool {
	m := strings.ToLower(model)
	if i := strings.LastIndexByte(m, '/'); i >= 0 {
		m = m[i+1:] // strip a vendor prefix like "openai/"
	}
	switch {
	case strings.HasPrefix(m, "o1"), strings.HasPrefix(m, "o3"),
		strings.HasPrefix(m, "o4"), strings.HasPrefix(m, "o5"):
		return true
	case strings.HasPrefix(m, "gpt-5"):
		return true
	default:
		return false
	}
}

// ConvertMessages converts llms.Message slice to openaicompat.ChatMessage slice.
// This is used by all OpenAI-compatible providers (OpenAI, TogetherAI, Featherless, Synthetic).
func ConvertMessages(messages []llms.Message) []ChatMessage {
	result := make([]ChatMessage, len(messages))
	for i, msg := range messages {
		result[i] = ChatMessage{
			Role:       string(msg.Role),
			ToolCallID: msg.ToolCallID,
			Name:       msg.Name,
		}

		// Check if message has multi-part content (for vision)
		if len(msg.Parts) > 0 {
			result[i].ContentValue = convertContentParts(msg.Parts)
		} else {
			result[i].ContentValue = msg.Content
		}

		if len(msg.ToolCalls) > 0 {
			result[i].ToolCalls = make([]ToolCall, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				result[i].ToolCalls[j] = ToolCall{
					ID:   tc.ID,
					Type: string(tc.Type),
				}
				if tc.Function != nil {
					result[i].ToolCalls[j].Function = &FunctionCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					}
				}
			}
		}
	}
	return result
}

// convertContentParts converts llms.ContentPart slice to openaicompat.ContentPart slice.
//
// Parts are appended, not index-filled: a part that produces nothing (a nil
// image, or an unknown/unsupported part type such as audio) is SKIPPED rather
// than left as a zero-value {"type":""} entry, which OpenAI rejects with a 400
// on the whole request. This mirrors the Responses converter.
func convertContentParts(parts []llms.ContentPart) []ContentPart {
	result := make([]ContentPart, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case llms.PartTypeText:
			result = append(result, ContentPart{
				Type: "text",
				Text: part.Text,
			})
		case llms.PartTypeImage:
			if part.Image != nil {
				result = append(result, ContentPart{
					Type: "image_url",
					ImageURL: &ImageURL{
						URL: formatImageURL(part.Image),
					},
				})
			}
		}
	}
	return result
}

// formatImageURL formats an ImageContent as a data URL or returns the URL directly.
func formatImageURL(img *llms.ImageContent) string {
	if img.Source == llms.ImageSourceURL {
		return img.Data
	}
	// Use fmt.Sprintf to avoid multiple string allocations
	return fmt.Sprintf("data:%s;base64,%s", img.MediaType, img.Data)
}

// convertTools converts llms.Tool slice to openaicompat.Tool slice.
func convertTools(tools []llms.Tool) []Tool {
	result := make([]Tool, len(tools))
	for i, t := range tools {
		result[i] = Tool{
			Type: string(t.Type),
		}
		if t.Function != nil {
			result[i].Function = &FunctionDefinition{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
				Strict:      t.Function.Strict,
			}
		}
	}
	return result
}

// convertToolChoice converts llms.ToolChoice to the appropriate OpenAI format.
func convertToolChoice(choice *llms.ToolChoice) any {
	if choice == nil {
		return nil
	}
	if choice.Mode == llms.ToolChoiceTool {
		return ToolChoiceFunction{
			Type: "function",
			Function: &FunctionSelector{
				Name: choice.Tool,
			},
		}
	}
	return string(choice.Mode)
}

// convertToolCallsToLLMs converts openaicompat.ToolCall slice to llms.ToolCall slice.
func convertToolCallsToLLMs(toolCalls []ToolCall) []llms.ToolCall {
	if len(toolCalls) == 0 {
		return nil
	}

	result := make([]llms.ToolCall, len(toolCalls))
	for i, tc := range toolCalls {
		result[i] = llms.ToolCall{
			ID:   tc.ID,
			Type: llms.ToolType(tc.Type),
		}
		if tc.Function != nil {
			result[i].Function = &llms.FunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			}
		}
	}
	return result
}

// appendOrMergeToolCall appends a new tool call or merges with an existing one.
// This is used during streaming to accumulate tool call arguments.
// OpenAI streaming sends tool call deltas with an index field to identify which
// tool call to update. The first delta has the id/name, subsequent deltas only
// have the index and arguments to append.
// mergeIndexedToolCall merges delta into the tool call at idx (0 <= idx < cap),
// extending the slice with empty entries if idx is beyond the current length.
func mergeIndexedToolCall(calls []llms.ToolCall, idx int, delta ToolCall) []llms.ToolCall {
	if idx >= len(calls) {
		// New tool call beyond the current slice; grow to fit.
		for len(calls) <= idx {
			calls = append(calls, llms.ToolCall{})
		}
		calls[idx] = llms.ToolCall{ID: delta.ID, Type: llms.ToolType(delta.Type)}
		if delta.Function != nil {
			calls[idx].Function = &llms.FunctionCall{
				Name:      delta.Function.Name,
				Arguments: delta.Function.Arguments,
			}
		}
		return calls
	}

	// Merge with the existing tool call at this index.
	if delta.ID != "" {
		calls[idx].ID = delta.ID
	}
	if delta.Type != "" {
		calls[idx].Type = llms.ToolType(delta.Type)
	}
	if delta.Function != nil {
		if calls[idx].Function == nil {
			calls[idx].Function = &llms.FunctionCall{}
		}
		if delta.Function.Name != "" {
			calls[idx].Function.Name = delta.Function.Name
		}
		calls[idx].Function.Arguments += delta.Function.Arguments
	}
	return calls
}

func appendOrMergeToolCall(calls []llms.ToolCall, delta ToolCall) []llms.ToolCall {
	// First try to match by index (OpenAI streaming format). A negative index is
	// malformed (a hostile/buggy server could send -1 to panic calls[-1]); fall
	// through to ID-based matching for it.
	if delta.Index != nil && *delta.Index >= 0 {
		// Cap the index so a malicious/buggy server can't force an unbounded
		// back-fill (OOM) via an absurd index; OpenAI only ever increments by one.
		const maxStreamedToolCalls = 1024
		if idx := *delta.Index; idx < maxStreamedToolCalls {
			return mergeIndexedToolCall(calls, idx, delta)
		}
		return calls
	}

	// Fall back to ID-based matching (for non-OpenAI providers or edge cases)
	for i := range calls {
		if calls[i].ID != "" && calls[i].ID == delta.ID {
			if delta.Function != nil && calls[i].Function != nil {
				calls[i].Function.Arguments += delta.Function.Arguments
			}
			return calls
		}
	}

	newCall := llms.ToolCall{
		ID:   delta.ID,
		Type: llms.ToolType(delta.Type),
	}
	if delta.Function != nil {
		newCall.Function = &llms.FunctionCall{
			Name:      delta.Function.Name,
			Arguments: delta.Function.Arguments,
		}
	}
	return append(calls, newCall)
}

// ConvertResponse converts an OpenAI-compatible response to llms.Response.
func ConvertResponse(resp *ChatCompletionResponse) *llms.Response {
	if len(resp.Choices) == 0 {
		return &llms.Response{ID: resp.ID}
	}

	choice := resp.Choices[0]
	response := &llms.Response{
		ID:           resp.ID,
		FinishReason: llms.FinishReason(choice.FinishReason),
	}

	if choice.Message != nil {
		response.Content = choice.Message.RawContent()
		response.ToolCalls = convertToolCallsToLLMs(choice.Message.ToolCalls)

		// Surface reasoning content if present. Providers use different field names:
		// "reasoning_content" (Z.AI GLM) and "reasoning" (Synthetic/Qwen Thinking).
		if reasoning := choice.Message.ReasoningText(); reasoning != "" {
			response.SetReasoning(&llms.ReasoningContent{Content: reasoning})
		}
	}

	if resp.Usage != nil {
		response.Usage = convertUsage(resp.Usage)
		if response.Reasoning != nil && response.Usage.ReasoningTokens > 0 {
			response.Reasoning.Tokens = response.Usage.ReasoningTokens
		}
	}

	return response
}

// convertUsage maps an OpenAI-compatible usage payload to the neutral llms.Usage,
// normalizing so PromptTokens EXCLUDES cache-read tokens (OpenAI/DeepSeek report
// prompt_tokens inclusive of the cached subset).
func convertUsage(u *Usage) llms.Usage {
	cacheRead := u.cacheReadTokens()
	prompt := u.PromptTokens - cacheRead
	if prompt < 0 {
		prompt = 0
	}
	return llms.Usage{
		PromptTokens:     prompt,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
		CacheReadTokens:  cacheRead,
		ReasoningTokens:  u.reasoningTokens(),
	}
}

// ConvertEmbeddingResponse converts an OpenAI-compatible embedding response to llms.EmbeddingResponse.
func ConvertEmbeddingResponse(resp *EmbeddingResponse) *llms.EmbeddingResponse {
	embeddings := make([]llms.Embedding, len(resp.Data))
	for i, data := range resp.Data {
		embeddings[i] = llms.Embedding{
			Index:  data.Index,
			Vector: data.Embedding,
			Object: data.Object,
		}
	}

	return &llms.EmbeddingResponse{
		Embeddings: embeddings,
		Model:      resp.Model,
		Usage: llms.EmbeddingUsage{
			PromptTokens: resp.Usage.PromptTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
	}
}

// BuildChatRequest builds a ChatCompletionRequest from llms types.
func BuildChatRequest(model string, messages []llms.Message, opts *llms.CallOptions, stream bool) *ChatCompletionRequest {
	req := &ChatCompletionRequest{
		Model:            model,
		Messages:         ConvertMessages(messages),
		Temperature:      opts.Temperature,
		MaxTokens:        opts.MaxTokens,
		TopP:             opts.TopP,
		FrequencyPenalty: opts.FrequencyPenalty,
		PresencePenalty:  opts.PresencePenalty,
		Stop:             opts.StopWords,
		Stream:           stream,
	}
	if stream {
		req.StreamOptions = &StreamOptions{IncludeUsage: true}
	}

	// OpenAI reasoning models (o-series, gpt-5) reject `max_tokens` (they require
	// `max_completion_tokens`) and reject `temperature`/`top_p`/penalties with an
	// HTTP 400. Normalize the request for them. reasoning_effort (set via
	// applyReasoning below) remains valid and is preserved.
	if isOpenAIReasoningModel(model) {
		req.MaxCompletionTokens = opts.MaxTokens
		req.MaxTokens = nil
		req.Temperature = nil
		req.TopP = nil
		req.FrequencyPenalty = nil
		req.PresencePenalty = nil
	}

	if len(opts.Tools) > 0 {
		req.Tools = convertTools(opts.Tools)
	}

	if opts.ToolChoice != nil {
		req.ToolChoice = convertToolChoice(opts.ToolChoice)
	}

	if opts.ResponseFormat != nil {
		req.ResponseFormat = convertResponseFormat(opts.ResponseFormat)
	}

	// Pass through ExtraBody for provider-specific extensions (e.g., LoRAX adapter_id).
	// Copy so reasoning translation below never mutates the caller's option map.
	if len(opts.ExtraBody) > 0 {
		eb := make(map[string]any, len(opts.ExtraBody)+1)
		for k, v := range opts.ExtraBody {
			eb[k] = v
		}
		req.ExtraBody = eb
	}

	applyReasoning(req, opts.Reasoning)

	return req
}

// applyReasoning translates a neutral ReasoningConfig onto an OpenAI-compatible
// request. OpenAI reasoning models read the top-level reasoning_effort field;
// providers exposing a boolean "thinking" toggle (Z.AI GLM, Qwen) read a
// {"type":"enabled"|"disabled"} object, which we inject via ExtraBody so it is
// flattened at the top level. A caller-supplied "thinking" key is left untouched.
func applyReasoning(req *ChatCompletionRequest, rc *llms.ReasoningConfig) {
	if rc == nil {
		return
	}
	if rc.Effort != "" {
		req.ReasoningEffort = string(rc.Effort)
	}
	// Emit the boolean thinking toggle only when the caller set Enabled explicitly.
	// A bare BudgetTokens is an Anthropic/Gemini concept; injecting a "thinking"
	// field from it could make a strict OpenAI-compatible server reject the request.
	var mode string
	switch {
	case rc.Enabled != nil && *rc.Enabled:
		mode = "enabled"
	case rc.Enabled != nil && !*rc.Enabled:
		mode = "disabled"
	}
	if mode != "" {
		if req.ExtraBody == nil {
			req.ExtraBody = make(map[string]any, 1)
		}
		if _, exists := req.ExtraBody["thinking"]; !exists {
			req.ExtraBody["thinking"] = map[string]any{"type": mode}
		}
	}
}

// convertResponseFormat converts an llms response format to the OpenAI-compatible wire format.
func convertResponseFormat(format *llms.ResponseFormat) *ResponseFormat {
	if format == nil {
		return nil
	}

	result := &ResponseFormat{Type: string(format.Type)}
	if format.Type == llms.ResponseFormatJSONSchema && format.JSONSchema != nil {
		result.JSONSchema = &ResponseJSONSchema{
			Name:        format.JSONSchema.Name,
			Description: format.JSONSchema.Description,
			Schema:      append([]byte(nil), format.JSONSchema.Schema...),
			Strict:      format.JSONSchema.Strict,
		}
	}
	return result
}

// effectiveModel returns the model to use, preferring the option override if set.
func effectiveModel(clientModel, optionModel string) string {
	if optionModel != "" {
		return optionModel
	}
	return clientModel
}

// StreamConfig holds optional configuration for stream processing.
type StreamConfig struct {
	// Messages are the input messages (used for token estimation)
	Messages []llms.Message
	// EstimateTokens enables token estimation when provider doesn't return usage
	EstimateTokens bool
}

// ProcessStream handles streaming with optional configuration.
func ProcessStream(
	ctx context.Context,
	stream *StreamReader,
	chunks chan<- llms.StreamChunk,
	sender *llms.StreamSender,
	provider string,
	config *StreamConfig,
) {
	defer close(chunks)

	// Validate required parameters to prevent nil pointer panics.
	// These checks catch programming errors early with clear error messages.
	if stream == nil {
		if sender != nil {
			sender.SendFinal(llms.StreamChunk{
				Error: fmt.Errorf("%s: stream reader is nil", provider),
			})
		}
		return
	}
	if sender == nil {
		// Can't report error without sender, just close stream and return
		_ = stream.Close()
		return
	}

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

	// A malformed/hostile provider response must never crash the host process.
	// Convert any panic in stream processing into a terminal error chunk.
	defer func() {
		if r := recover(); r != nil {
			sender.DeliverTerminal(llms.StreamChunk{
				Error: fmt.Errorf("%s: panic during stream processing: %v", provider, r),
			})
		}
	}()

	var accumulatedToolCalls []llms.ToolCall
	var accumulatedContent string // Full content for token estimation
	var finishReason llms.FinishReason
	var usage *llms.Usage
	var bytesRead int64
	var chunksRead int
	var lastContent string

	for {
		select {
		case <-ctx.Done():
			// The context was canceled or its deadline expired. Deliver a terminal
			// chunk carrying the context error so a truncated stream is never
			// mistaken for a successful completion.
			sender.DeliverTerminal(llms.StreamChunk{Error: ctx.Err(), Done: true})
			return
		default:
		}

		chunk, err := stream.Read()
		if errors.Is(err, io.EOF) {
			if ctx.Err() != nil {
				sender.DeliverTerminal(llms.StreamChunk{Error: ctx.Err(), Done: true})
				return
			}
			// Apply token estimation if enabled and usage is missing
			finalUsage := usage
			if config != nil && config.EstimateTokens && (usage == nil || usage.TotalTokens == 0) {
				estimated := llms.EstimateUsageFromMessages(config.Messages, accumulatedContent)
				finalUsage = &estimated
			}

			sender.SendFinal(llms.StreamChunk{
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
				Cause:       fmt.Errorf("%s: %w", provider, err),
				BytesRead:   bytesRead,
				ChunksRead:  chunksRead,
				LastContent: lastContent,
			}
			sender.SendFinal(llms.StreamChunk{Error: streamErr})
			return
		}
		chunksRead++

		if len(chunk.Choices) > 0 {
			choice := chunk.Choices[0]

			if choice.Delta != nil {
				content := choice.Delta.RawContent()
				if content != "" {
					lastContent = content
					accumulatedContent += content // Accumulate for estimation
					bytesRead += int64(len(content))
					if sender.ForwardTerminalOnEarlyExit(sender.Send(llms.StreamChunk{Content: content})) {
						return
					}
				}

				// Accumulate reasoning content separately
				// Handles both "reasoning_content" (Z.AI GLM) and "reasoning" (Synthetic/Qwen Thinking)
				if reasoning := choice.Delta.ReasoningText(); reasoning != "" {
					// Send reasoning in chunks for real-time display
					rc := &llms.ReasoningContent{Content: reasoning}
					if sender.ForwardTerminalOnEarlyExit(sender.Send(llms.StreamChunk{
						Reasoning: rc,
					})) {
						return
					}
				}
			}

			if choice.Delta != nil && len(choice.Delta.ToolCalls) > 0 {
				for _, tc := range choice.Delta.ToolCalls {
					accumulatedToolCalls = appendOrMergeToolCall(accumulatedToolCalls, tc)
				}
			}

			if choice.FinishReason != "" {
				finishReason = llms.FinishReason(choice.FinishReason)
			}
		}

		if chunk.Usage != nil {
			u := convertUsage(chunk.Usage)
			usage = &u
		}
	}
}

// WrapError converts an httpclient.APIError to an llms.APIError with provider context.
// For other errors, it wraps them in an llms.ProviderError.
// Returns nil if err is nil.
func WrapError(provider llms.Provider, operation string, err error) error {
	if err == nil {
		return nil
	}

	// Check if it's an httpclient.APIError and convert to llms.APIError
	var httpErr *httpclient.APIError
	if errors.As(err, &httpErr) {
		return &llms.APIError{
			StatusCode:    httpErr.StatusCode,
			Message:       httpErr.Message,
			Type:          httpErr.Type,
			Code:          httpErr.Code,
			Param:         httpErr.Param,
			RequestID:     httpErr.RequestID,
			RetryAfter:    httpErr.RetryAfter,
			Provider:      provider,
			RequestURL:    httpErr.RequestURL,
			RequestMethod: httpErr.RequestMethod,
		}
	}

	// Check if it's already an llms.APIError
	var apiErr *llms.APIError
	if errors.As(err, &apiErr) {
		if apiErr.Provider == "" {
			apiErr.Provider = provider
		}
		return apiErr
	}

	// For other errors, wrap with provider context
	return llms.WrapProviderError(provider, operation, err)
}
