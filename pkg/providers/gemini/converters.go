package gemini

import (
	"encoding/json"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/internal/geminiapi"
)

const (
	roleUser = "user"
)

// convertResponse converts a Gemini API response to an llms.Response.
func convertResponse(resp *geminiapi.GenerateContentResponse) *llms.Response {
	if len(resp.Candidates) == 0 {
		return &llms.Response{}
	}

	candidate := resp.Candidates[0]
	response := &llms.Response{}

	if candidate.Content != nil {
		response.Content = geminiapi.ExtractTextContent(candidate.Content.Parts)

		// Extract reasoning ("thought") content, preserving the thought
		// signature so callers can inspect it for multi-turn replay.
		if thought := geminiapi.ExtractThoughtContent(candidate.Content.Parts); thought != "" {
			response.SetReasoning(&llms.ReasoningContent{
				Content:   thought,
				Signature: geminiapi.ExtractThoughtSignature(candidate.Content.Parts),
			})
		}

		// Extract function calls. Gemini attaches a thoughtSignature to the
		// functionCall part when thinking is enabled; carry it on the tool call
		// so it can be echoed back on the next turn (see convertMessages).
		for _, part := range candidate.Content.Parts {
			if part.FunctionCall != nil {
				tc := llms.ToolCall{
					ID:   part.FunctionCall.Name, // Gemini doesn't have IDs, use name
					Type: llms.ToolTypeFunction,
					Function: &llms.FunctionCall{
						Name:      part.FunctionCall.Name,
						Arguments: geminiapi.FunctionCallToJSON(part.FunctionCall),
					},
					Signature: part.ThoughtSignature,
				}
				response.ToolCalls = append(response.ToolCalls, tc)
			}
		}
	}

	response.FinishReason = llms.FinishReason(geminiapi.GetFinishReason(candidate.FinishReason))
	// Gemini returns STOP even when the response contains function calls; normalize
	// to the cross-provider tool-calls finish reason so callers keying on it work.
	if len(response.ToolCalls) > 0 {
		response.FinishReason = llms.FinishReasonToolCalls
	}

	if resp.UsageMetadata != nil {
		response.Usage = convertUsageMetadata(resp.UsageMetadata)
		if response.Reasoning != nil {
			response.Reasoning.Tokens = response.Usage.ReasoningTokens
		}
	}

	return response
}

// convertUsageMetadata maps Gemini usage metadata to the neutral llms.Usage.
// PromptTokens excludes cached tokens; CompletionTokens must include reasoning
// tokens (Usage contract). Whether candidatesTokenCount already includes
// thoughtsTokenCount is backend- and model-dependent: Vertex reports them
// separately, and while the Gemini API is documented to fold thoughts into
// candidates it frequently does not in practice. So we detect the
// separate-accounting case via the reported total and fold thoughts in only
// then — counting reasoning tokens exactly once and keeping
// PromptTokens + CompletionTokens + CacheReadTokens == TotalTokenCount.
func convertUsageMetadata(um *geminiapi.UsageMetadata) llms.Usage {
	prompt := um.PromptTokenCount - um.CachedContentTokenCount
	if prompt < 0 {
		prompt = 0
	}
	completion := um.CandidatesTokenCount
	if um.ThoughtsTokenCount > 0 &&
		um.PromptTokenCount+um.CandidatesTokenCount+um.ThoughtsTokenCount == um.TotalTokenCount {
		completion += um.ThoughtsTokenCount
	}
	return llms.Usage{
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      um.TotalTokenCount,
		CacheReadTokens:  um.CachedContentTokenCount,
		ReasoningTokens:  um.ThoughtsTokenCount,
	}
}

// convertMessages converts llms.Message slice to geminiapi.Content slice.
func convertMessages(messages []llms.Message) []geminiapi.Content {
	var result []geminiapi.Content

	for _, msg := range messages {
		role := convertRole(msg.Role)

		var parts []geminiapi.Part

		// Handle tool result messages
		switch {
		case msg.Role == llms.RoleTool && msg.ToolCallID != "":
			// Parse the content as response
			var respData map[string]any
			if err := json.Unmarshal([]byte(msg.Content), &respData); err != nil {
				// If not JSON, wrap in result field
				respData = map[string]any{"result": msg.Content}
			}

			parts = append(parts, geminiapi.Part{
				FunctionResponse: &geminiapi.FunctionResponse{
					Name:     msg.Name,
					Response: respData,
				},
			})
		case len(msg.Parts) > 0:
			// Handle multi-part content (including images)
			parts = append(parts, convertContentParts(msg.Parts)...)
		case msg.Content != "":
			parts = append(parts, geminiapi.Part{
				Text: msg.Content,
			})
		}

		// Handle function calls in assistant messages. Echo back the Gemini
		// thoughtSignature captured on the tool call so 2.5+ thinking models
		// keep their reasoning context across the tool-calling round-trip.
		for _, tc := range msg.ToolCalls {
			if tc.Function != nil {
				var args map[string]any
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)

				parts = append(parts, geminiapi.Part{
					FunctionCall: &geminiapi.FunctionCall{
						Name: tc.Function.Name,
						Args: args,
					},
					ThoughtSignature: tc.Signature,
				})
			}
		}

		if len(parts) > 0 {
			result = append(result, geminiapi.Content{
				Role:  role,
				Parts: parts,
			})
		}
	}

	return result
}

// convertContentParts converts llms.ContentPart to geminiapi.Part.
func convertContentParts(parts []llms.ContentPart) []geminiapi.Part {
	result := make([]geminiapi.Part, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case llms.PartTypeText:
			result = append(result, geminiapi.Part{
				Text: part.Text,
			})
		case llms.PartTypeImage:
			if part.Image != nil {
				// Gemini uses inlineData for base64 images
				if part.Image.Source == llms.ImageSourceBase64 {
					result = append(result, geminiapi.Part{
						InlineData: &geminiapi.InlineData{
							MimeType: part.Image.MediaType,
							Data:     part.Image.Data,
						},
					})
				}
				// Note: For URL images, you'd need to fetch and convert to base64
			}
		}
	}
	return result
}

// convertRole converts llms.Role to Gemini role string.
func convertRole(role llms.Role) string {
	switch role {
	case llms.RoleUser:
		return roleUser
	case llms.RoleAssistant:
		return "model"
	case llms.RoleSystem:
		return roleUser // Gemini handles system prompts differently
	case llms.RoleTool:
		return roleUser // Tool results come from user
	default:
		return roleUser
	}
}

// convertTools converts llms.Tool slice to geminiapi.Tool slice.
func convertTools(tools []llms.Tool) []geminiapi.Tool {
	var declarations []geminiapi.FunctionDeclaration

	for _, t := range tools {
		if t.Function != nil {
			declarations = append(declarations, geminiapi.FunctionDeclaration{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			})
		}
	}

	if len(declarations) == 0 {
		return nil
	}

	return []geminiapi.Tool{
		{FunctionDeclarations: declarations},
	}
}
