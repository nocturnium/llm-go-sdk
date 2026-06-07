package openaicompat

import (
	"context"
	"errors"
	"fmt"
	"io"

	llms "github.com/nocturnium/llm-go-sdk"
	"github.com/nocturnium/llm-go-sdk/internal/httpclient"
)

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
func convertContentParts(parts []llms.ContentPart) []ContentPart {
	result := make([]ContentPart, len(parts))
	for i, part := range parts {
		switch part.Type {
		case llms.PartTypeText:
			result[i] = ContentPart{
				Type: "text",
				Text: part.Text,
			}
		case llms.PartTypeImage:
			if part.Image != nil {
				result[i] = ContentPart{
					Type: "image_url",
					ImageURL: &ImageURL{
						URL: formatImageURL(part.Image),
					},
				}
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
	if choice.Function != nil {
		return ToolChoiceFunction{
			Type: "function",
			Function: &FunctionSelector{
				Name: choice.Function.Name,
			},
		}
	}
	return string(choice.Type)
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
func appendOrMergeToolCall(calls []llms.ToolCall, delta ToolCall) []llms.ToolCall {
	// First try to match by index (OpenAI streaming format)
	if delta.Index != nil {
		idx := *delta.Index
		if idx < len(calls) {
			// Merge with existing tool call at this index
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
		// Index is beyond current slice - this is a new tool call
		// Extend the slice to accommodate the new index
		for len(calls) <= idx {
			calls = append(calls, llms.ToolCall{})
		}
		calls[idx] = llms.ToolCall{
			ID:   delta.ID,
			Type: llms.ToolType(delta.Type),
		}
		if delta.Function != nil {
			calls[idx].Function = &llms.FunctionCall{
				Name:      delta.Function.Name,
				Arguments: delta.Function.Arguments,
			}
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
		return &llms.Response{}
	}

	choice := resp.Choices[0]
	response := &llms.Response{
		FinishReason: llms.FinishReason(choice.FinishReason),
	}

	if choice.Message != nil {
		response.Content = choice.Message.Content()
		response.ToolCalls = convertToolCallsToLLMs(choice.Message.ToolCalls)

		// Populate Thinking if reasoning content is present
		// Handles both "reasoning_content" (Z.AI GLM) and "reasoning" (Synthetic/Qwen Thinking)
		if reasoning := choice.Message.ReasoningText(); reasoning != "" {
			response.Thinking = &llms.ThinkingContent{
				Content: reasoning,
			}
		}
	}

	if resp.Usage != nil {
		response.Usage = llms.Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
		if resp.Usage.PromptTokensDetails != nil {
			response.Usage.CacheReadTokens = resp.Usage.PromptTokensDetails.CachedTokens
		}
	}

	return response
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

	if len(opts.Tools) > 0 {
		req.Tools = convertTools(opts.Tools)
	}

	if opts.ToolChoice != nil {
		req.ToolChoice = convertToolChoice(opts.ToolChoice)
	}

	if opts.ResponseFormat != nil {
		req.ResponseFormat = convertResponseFormat(opts.ResponseFormat)
	}

	// Pass through ExtraBody for provider-specific extensions (e.g., LoRAX adapter_id)
	if len(opts.ExtraBody) > 0 {
		req.ExtraBody = opts.ExtraBody
	}

	return req
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

	var accumulatedToolCalls []llms.ToolCall
	var accumulatedContent string   // Full content for token estimation
	var accumulatedReasoning string // For reasoning/thinking content
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
			sender.ForwardTerminalOnEarlyExit(llms.SendContextCanceled)
			return
		default:
		}

		chunk, err := stream.Read()
		if errors.Is(err, io.EOF) {
			// Apply token estimation if enabled and usage is missing
			finalUsage := usage
			if config != nil && config.EstimateTokens && (usage == nil || usage.TotalTokens == 0) {
				estimated := llms.EstimateUsageFromMessages(config.Messages, accumulatedContent)
				finalUsage = &estimated
			}

			// Build final thinking if we accumulated any
			var finalThinking *llms.ThinkingContent
			if accumulatedReasoning != "" {
				finalThinking = &llms.ThinkingContent{
					Content: accumulatedReasoning,
				}
			}

			sender.SendFinal(llms.StreamChunk{
				Thinking:     finalThinking,
				ToolCalls:    accumulatedToolCalls,
				FinishReason: finishReason,
				Usage:        finalUsage,
			})
			return
		}
		if err != nil {
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
				content := choice.Delta.Content()
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
					accumulatedReasoning += reasoning
					// Send reasoning in chunks for real-time display
					if sender.ForwardTerminalOnEarlyExit(sender.Send(llms.StreamChunk{
						Thinking: &llms.ThinkingContent{
							Content: reasoning,
						},
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
			usage = &llms.Usage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
			if chunk.Usage.PromptTokensDetails != nil {
				usage.CacheReadTokens = chunk.Usage.PromptTokensDetails.CachedTokens
			}
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
