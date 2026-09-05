package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/anthropicapi"
)

// cacheControlTTL renders a cache TTL as the Anthropic cache_control "ttl" value
// ("1h" for one hour or longer), or "" to use the default 5-minute ephemeral
// cache.
func cacheControlTTL(ttl time.Duration) string {
	if ttl >= time.Hour {
		return "1h"
	}
	return ""
}

const (
	contentTypeToolUse          = "tool_use"
	contentTypeRedactedThinking = "redacted_thinking"

	// redactedThinkingMetadataKey is the ReasoningContent.Metadata key under which
	// encrypted redacted_thinking blocks ([]string) are carried so they can be
	// replayed verbatim on the next turn.
	redactedThinkingMetadataKey = "anthropic_redacted_thinking"
)

// ErrUnsupportedImageSource is returned when an image source type is not supported.
// Anthropic accepts base64 and URL image sources.
var ErrUnsupportedImageSource = errors.New("anthropic: unsupported image source type (base64 and url are supported)")

// convertUsage maps Anthropic usage onto the neutral llms.Usage. Anthropic's
// input_tokens already excludes cached tokens (matching the Usage contract), so
// TotalTokens is rebuilt to include the cache-read and cache-creation tokens the
// way OpenAI's and Gemini's provider totals do.
func convertUsage(u anthropicapi.Usage) llms.Usage {
	usage := llms.Usage{
		PromptTokens:        u.InputTokens,
		CompletionTokens:    u.OutputTokens,
		CacheReadTokens:     u.CacheReadInputTokens,
		CacheCreationTokens: u.CacheCreationInputTokens,
	}
	usage.TotalTokens = totalTokens(usage)
	return usage
}

// totalTokens is the cross-provider total: every token processed for the
// request, cached or not.
func totalTokens(u llms.Usage) int {
	return u.PromptTokens + u.CacheReadTokens + u.CacheCreationTokens + u.CompletionTokens
}

// convertResponse converts an Anthropic API response to an llms.Response.
func convertResponse(resp *anthropicapi.MessagesResponse, structuredToolName ...string) *llms.Response {
	structuredOutputTool := ""
	if len(structuredToolName) > 0 {
		structuredOutputTool = structuredToolName[0]
	}

	response := &llms.Response{
		ID:           resp.ID,
		Content:      anthropicapi.ExtractTextContent(resp.Content),
		FinishReason: convertStopReason(resp.StopReason),
		Usage:        convertUsage(resp.Usage),
	}

	// Extract tool calls and extended-thinking blocks.
	var reasoningText, reasoningSignature string
	var redacted []string
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
		case contentTypeRedactedThinking:
			if part.Data != "" {
				redacted = append(redacted, part.Data)
			}
		}
	}
	if reasoningText != "" || reasoningSignature != "" || len(redacted) > 0 {
		rc := &llms.ReasoningContent{
			Content:   reasoningText,
			Signature: reasoningSignature,
		}
		if len(redacted) > 0 {
			rc.Metadata = map[string]any{redactedThinkingMetadataKey: redacted}
		}
		response.SetReasoning(rc)
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
		if msg.Role == llms.RoleAssistant && msg.Reasoning != nil {
			var thinking []anthropicapi.ContentPart
			if msg.Reasoning.Signature != "" {
				thinking = append(thinking, anthropicapi.ContentPart{
					Type:      "thinking",
					Thinking:  msg.Reasoning.Content,
					Signature: msg.Reasoning.Signature,
				})
			}
			// Encrypted redacted_thinking blocks are opaque and must be echoed
			// back verbatim so the turn stays valid.
			for _, data := range redactedThinkingBlocks(msg.Reasoning) {
				thinking = append(thinking, anthropicapi.ContentPart{
					Type: contentTypeRedactedThinking,
					Data: data,
				})
			}
			if len(thinking) > 0 {
				content = append(thinking, content...)
			}
		}

		// Apply a per-message cache breakpoint to the last content block so the
		// prompt prefix up to this message is cached.
		if msg.CacheControl != nil && len(content) > 0 {
			content[len(content)-1].CacheControl = &anthropicapi.CacheControl{
				Type: "ephemeral",
				TTL:  cacheControlTTL(msg.CacheControl.TTL),
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
				switch part.Image.Source {
				case llms.ImageSourceBase64:
					result = append(result, anthropicapi.ContentPart{
						Type: "image",
						Source: &anthropicapi.ImageSource{
							Type:      "base64",
							MediaType: part.Image.MediaType,
							Data:      part.Image.Data,
						},
					})
				case llms.ImageSourceURL:
					// Anthropic fetches the image server-side from a public URL.
					result = append(result, anthropicapi.ContentPart{
						Type:   "image",
						Source: &anthropicapi.ImageSource{Type: "url", URL: part.Image.Data},
					})
				default:
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

// redactedThinkingBlocks returns the encrypted redacted_thinking payloads stored
// on a ReasoningContent by convertResponse / Stream, tolerating both the
// []string form and a JSON-roundtripped []any form.
func redactedThinkingBlocks(rc *llms.ReasoningContent) []string {
	if rc == nil || rc.Metadata == nil {
		return nil
	}
	switch v := rc.Metadata[redactedThinkingMetadataKey].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
