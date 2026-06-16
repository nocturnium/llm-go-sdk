package gemini

import (
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v2"
	"github.com/nocturnium/llm-go-sdk/v2/internal/geminiapi"
)

// FuzzConvertMessages tests that convertMessages handles arbitrary content
// without panicking.
func FuzzConvertMessages(f *testing.F) {
	// Seed with typical cases
	f.Add("Hello, how are you?")
	f.Add("")
	f.Add("{\"key\": \"value\"}")
	f.Add("<script>alert('xss')</script>")
	f.Add("\x00\x01\x02\x03")
	f.Add("🎉 Unicode emoji 日本語 العربية")
	f.Add("\n\t\r")
	f.Add("{\"nested\": {\"deep\": {\"data\":")

	f.Fuzz(func(t *testing.T, content string) {
		messages := []llms.Message{
			{Role: llms.RoleUser, Content: content},
		}

		// Should not panic
		result := convertMessages(messages)

		if len(result) == 0 && content != "" {
			t.Error("expected non-empty result for non-empty content")
		}
	})
}

// FuzzConvertMessages_ToolResponse tests tool response handling.
func FuzzConvertMessages_ToolResponse(f *testing.F) {
	f.Add("get_weather", `{"temperature": 72}`)
	f.Add("", "")
	f.Add("func", "not json")
	f.Add("search", `[1, 2, 3]`)

	f.Fuzz(func(t *testing.T, toolName, response string) {
		messages := []llms.Message{
			{
				Role:       llms.RoleTool,
				ToolCallID: toolName,
				Name:       toolName,
				Content:    response,
			},
		}

		// Should not panic
		_ = convertMessages(messages)
	})
}

// FuzzConvertRole tests role conversion.
func FuzzConvertRole(f *testing.F) {
	f.Add("user")
	f.Add("assistant")
	f.Add("system")
	f.Add("tool")
	f.Add("")
	f.Add("unknown")
	f.Add("admin")

	f.Fuzz(func(t *testing.T, roleStr string) {
		role := llms.Role(roleStr)
		result := convertRole(role)

		// Should always return a valid role
		if result == "" {
			t.Error("expected non-empty role")
		}
	})
}

// FuzzConvertContentParts tests content part conversion.
func FuzzConvertContentParts(f *testing.F) {
	f.Add("text", "sample text", "", "")
	f.Add("image", "", "image/png", "base64data")
	f.Add("unknown", "data", "", "")

	f.Fuzz(func(t *testing.T, partType, text, mediaType, imageData string) {
		var parts []llms.ContentPart

		switch partType {
		case "text":
			parts = []llms.ContentPart{
				{Type: llms.PartTypeText, Text: text},
			}
		case "image":
			parts = []llms.ContentPart{
				{
					Type: llms.PartTypeImage,
					Image: &llms.ImageContent{
						Source:    llms.ImageSourceBase64,
						MediaType: mediaType,
						Data:      imageData,
					},
				},
			}
		default:
			parts = []llms.ContentPart{
				{Type: llms.PartType(partType), Text: text},
			}
		}

		// Should not panic
		_ = convertContentParts(parts)
	})
}

// FuzzConvertTools tests tool conversion.
func FuzzConvertTools(f *testing.F) {
	f.Add("get_weather", "Get weather for a location")
	f.Add("", "")
	f.Add("search", "Search the web")

	f.Fuzz(func(t *testing.T, name, description string) {
		tools := []llms.Tool{
			{
				Type: "function",
				Function: &llms.FunctionDefinition{
					Name:        name,
					Description: description,
				},
			},
		}

		// Should not panic
		result := convertTools(tools)

		// Empty tools should return nil
		if len(tools) == 0 && result != nil {
			t.Error("expected nil for empty tools")
		}
	})
}

// FuzzConvertResponse tests response conversion.
func FuzzConvertResponse(f *testing.F) {
	f.Add("Hello world", "STOP", 10, 5, 15)
	f.Add("", "", 0, 0, 0)
	f.Add("{\"json\": true}", "MAX_TOKENS", 100, 50, 150)

	f.Fuzz(func(t *testing.T, text, finishReason string, prompt, completion, total int) {
		resp := &geminiapi.GenerateContentResponse{
			Candidates: []geminiapi.Candidate{
				{
					Content: &geminiapi.Content{
						Parts: []geminiapi.Part{
							{Text: text},
						},
					},
					FinishReason: finishReason,
				},
			},
			UsageMetadata: &geminiapi.UsageMetadata{
				PromptTokenCount:     prompt,
				CandidatesTokenCount: completion,
				TotalTokenCount:      total,
			},
		}

		// Should not panic
		result := convertResponse(resp)

		if result == nil {
			t.Error("expected non-nil result")
		}
	})
}

// FuzzConvertResponse_Empty tests empty response handling.
func FuzzConvertResponse_Empty(f *testing.F) {
	f.Add(true)
	f.Add(false)

	f.Fuzz(func(t *testing.T, hasCandidate bool) {
		var resp *geminiapi.GenerateContentResponse

		if hasCandidate {
			resp = &geminiapi.GenerateContentResponse{
				Candidates: []geminiapi.Candidate{},
			}
		} else {
			resp = &geminiapi.GenerateContentResponse{}
		}

		// Should not panic
		result := convertResponse(resp)

		if result == nil {
			t.Error("expected non-nil result even for empty response")
		}
	})
}
