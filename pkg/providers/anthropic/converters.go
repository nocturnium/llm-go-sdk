package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/internal/anthropicapi"
)

const (
	contentTypeToolUse = "tool_use"
)

// ErrUnsupportedImageSource is returned when an image source type is not supported.
// Anthropic only supports base64-encoded images, not URL-based images.
var ErrUnsupportedImageSource = errors.New("anthropic: unsupported image source type (only base64 is supported, not URL)")

// convertResponse converts an Anthropic API response to an llms.Response.
func convertResponse(resp *anthropicapi.MessagesResponse, structuredToolName ...string) *llms.Response {
	structuredOutputTool := ""
	if len(structuredToolName) > 0 {
		structuredOutputTool = structuredToolName[0]
	}

	response := &llms.Response{
		Content:      anthropicapi.ExtractTextContent(resp.Content),
		FinishReason: convertStopReason(resp.StopReason),
		Usage: llms.Usage{
			PromptTokens:        resp.Usage.InputTokens,
			CompletionTokens:    resp.Usage.OutputTokens,
			TotalTokens:         resp.Usage.InputTokens + resp.Usage.OutputTokens,
			CacheReadTokens:     resp.Usage.CacheReadInputTokens,
			CacheCreationTokens: resp.Usage.CacheCreationInputTokens,
		},
	}

	// Extract tool calls and extended-thinking blocks.
	var reasoningText, reasoningSignature string
	for _, part := range resp.Content {
		switch part.Type {
		case contentTypeToolUse:
			if structuredOutputTool != "" && part.Name == structuredOutputTool && response.Content == "" {
				response.Content = string(part.Input)
			}
			tc := llms.ToolCall{
				ID:   part.ID,
				Type: llms.ToolTypeFunction,
				Function: &llms.FunctionCall{
					Name:      part.Name,
					Arguments: string(part.Input),
				},
			}
			response.ToolCalls = append(response.ToolCalls, tc)
		case "thinking":
			reasoningText += part.Thinking
			if part.Signature != "" {
				reasoningSignature = part.Signature
			}
		}
	}
	if reasoningText != "" || reasoningSignature != "" {
		response.SetReasoning(&llms.ReasoningContent{
			Content:   reasoningText,
			Signature: reasoningSignature,
		})
	}

	return response
}

// convertMessages converts llms.Message slice to anthropicapi.Message slice.
// Returns an error if any message contains unsupported content types.
func convertMessages(messages []llms.Message) ([]anthropicapi.Message, error) {
	var result []anthropicapi.Message
	seenToolResults := make(map[string]bool)

	for _, msg := range messages {
		// Skip system messages - they're handled separately
		if msg.Role == llms.RoleSystem {
			continue
		}

		role := string(msg.Role)
		if role == string(llms.RoleTool) {
			role = "user" // Anthropic uses "user" role for tool results
		}

		var content []anthropicapi.ContentPart

		// Handle tool result messages
		switch {
		case msg.Role == llms.RoleTool && msg.ToolCallID != "":
			if seenToolResults[msg.ToolCallID] {
				// Duplicate tool results indicate malformed upstream history; Anthropic rejects them request-wide.
				continue
			}
			seenToolResults[msg.ToolCallID] = true
			content = append(content, anthropicapi.ContentPart{
				Type:      "tool_result",
				ToolUseID: msg.ToolCallID,
				Content:   msg.Content,
			})
		case len(msg.Parts) > 0:
			// Handle multi-part content (including images)
			parts, err := convertContentParts(msg.Parts)
			if err != nil {
				return nil, err
			}
			content = append(content, parts...)
		case msg.Content != "":
			// Regular text content
			content = append(content, anthropicapi.ContentPart{
				Type: "text",
				Text: msg.Content,
			})
		}

		// Handle tool calls in assistant messages
		for _, tc := range msg.ToolCalls {
			if tc.Function != nil {
				content = append(content, anthropicapi.ContentPart{
					Type:  contentTypeToolUse,
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: json.RawMessage(tc.Function.Arguments),
				})
			}
		}

		// Anthropic requires an assistant turn that used extended thinking to BEGIN
		// with its thinking block; re-emit it from Message.Reasoning so the second
		// and later turns of an agentic thinking+tools loop do not fail with
		// "messages.N: must start with a thinking block". Only a signed block can
		// be replayed (an unsigned summary would itself be rejected).
		if msg.Role == llms.RoleAssistant && msg.Reasoning != nil && msg.Reasoning.Signature != "" {
			thinking := anthropicapi.ContentPart{
				Type:      "thinking",
				Thinking:  msg.Reasoning.Content,
				Signature: msg.Reasoning.Signature,
			}
			content = append([]anthropicapi.ContentPart{thinking}, content...)
		}

		// Apply a per-message cache breakpoint to the last content block so the
		// prompt prefix up to this message is cached.
		if msg.CacheControl != nil && len(content) > 0 {
			content[len(content)-1].CacheControl = &anthropicapi.CacheControl{
				Type: "ephemeral",
				TTL:  llms.AnthropicTTL(msg.CacheControl.TTL),
			}
		}

		if len(content) > 0 {
			result = append(result, anthropicapi.Message{
				Role:    role,
				Content: content,
			})
		}
	}

	return result, nil
}

// convertContentParts converts llms.ContentPart to anthropicapi.ContentPart.
// Returns an error if any image has an unsupported source type.
func convertContentParts(parts []llms.ContentPart) ([]anthropicapi.ContentPart, error) {
	result := make([]anthropicapi.ContentPart, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case llms.PartTypeText:
			result = append(result, anthropicapi.ContentPart{
				Type: "text",
				Text: part.Text,
			})
		case llms.PartTypeImage:
			if part.Image != nil {
				// Anthropic requires base64 images with source object
				if part.Image.Source == llms.ImageSourceBase64 {
					result = append(result, anthropicapi.ContentPart{
						Type: "image",
						Source: &anthropicapi.ImageSource{
							Type:      "base64",
							MediaType: part.Image.MediaType,
							Data:      part.Image.Data,
						},
					})
				} else {
					// Return error for unsupported image sources (e.g., URL)
					return nil, fmt.Errorf("%w: got %q", ErrUnsupportedImageSource, part.Image.Source)
				}
			}
		}
	}
	return result, nil
}

// convertTools converts llms.Tool slice to anthropicapi.Tool slice.
func convertTools(tools []llms.Tool) []anthropicapi.Tool {
	result := make([]anthropicapi.Tool, 0, len(tools))
	for _, t := range tools {
		if t.Function != nil {
			result = append(result, anthropicapi.Tool{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				InputSchema: t.Function.Parameters,
			})
		}
	}
	return result
}

// convertToolChoice converts llms tool choice to Anthropic format.
func convertToolChoice(choice *llms.ToolChoice) any {
	if choice == nil {
		return nil
	}
	if choice.Mode == llms.ToolChoiceTool {
		return anthropicapi.ToolChoiceTool{
			Type: "tool",
			Name: choice.Tool,
		}
	}
	switch choice.Mode {
	case llms.ToolChoiceAuto:
		return anthropicapi.ToolChoiceAuto{Type: "auto"}
	case llms.ToolChoiceRequired:
		return anthropicapi.ToolChoiceAny{Type: "any"}
	case llms.ToolChoiceNone:
		return struct {
			Type string `json:"type"`
		}{Type: "none"}
	default:
		return anthropicapi.ToolChoiceAuto{Type: "auto"}
	}
}

// convertStopReason converts Anthropic stop reason to standard finish reason.
func convertStopReason(reason string) llms.FinishReason {
	switch reason {
	case "end_turn":
		return llms.FinishReasonStop
	case contentTypeToolUse:
		return llms.FinishReasonToolCalls
	case "max_tokens":
		return llms.FinishReasonLength
	case "stop_sequence":
		return llms.FinishReasonStop
	default:
		return llms.FinishReason(reason)
	}
}
