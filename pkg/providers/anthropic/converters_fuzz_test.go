package anthropic

import (
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v3"
	"github.com/nocturnium/llm-go-sdk/v3/internal/anthropicapi"
)

// FuzzConvertMessages tests that convertMessages handles arbitrary string content
// without panicking.
func FuzzConvertMessages(f *testing.F) {
	// Seed with some typical cases
	f.Add("Hello, how are you?")
	f.Add("")
	f.Add("{\"key\": \"value\"}")
	f.Add("<script>alert('xss')</script>")
	f.Add("\x00\x01\x02\x03") // Binary data
	f.Add("🎉 Unicode emoji 日本語 العربية")
	f.Add("\n\t\r")                             // Whitespace
	f.Add("a" + string(make([]byte, 10000)))    // Long string
	f.Add("{\"nested\": {\"deep\": {\"data\":") // Malformed JSON

	f.Fuzz(func(t *testing.T, content string) {
		messages := []llms.Message{
			{Role: llms.RoleUser, Content: content},
		}

		// Should not panic and should handle any input gracefully
		result, err := convertMessages(messages)
		if err != nil {
			// Errors are acceptable, panics are not
			return
		}

		// If successful, verify basic invariants
		if len(result) == 0 && content != "" {
			t.Error("expected non-empty result for non-empty content")
		}
	})
}

// FuzzConvertMessages_MultiRole tests conversion with multiple message roles.
func FuzzConvertMessages_MultiRole(f *testing.F) {
	f.Add("system prompt", "user message", "assistant response")
	f.Add("", "", "")
	f.Add("{}", "{}", "{}")

	f.Fuzz(func(t *testing.T, system, user, assistant string) {
		messages := []llms.Message{
			{Role: llms.RoleSystem, Content: system},
			{Role: llms.RoleUser, Content: user},
			{Role: llms.RoleAssistant, Content: assistant},
		}

		// Should not panic
		_, _ = convertMessages(messages)
	})
}

// FuzzConvertContentParts tests image content handling.
func FuzzConvertContentParts(f *testing.F) {
	f.Add("text", "sample text", "")
	f.Add("image", "base64data", "image/png")
	f.Add("image", "", "")
	f.Add("unknown", "data", "type")

	f.Fuzz(func(t *testing.T, partType, data, mediaType string) {
		var parts []llms.ContentPart

		switch partType {
		case "text":
			parts = []llms.ContentPart{
				{Type: llms.PartTypeText, Text: data},
			}
		case "image":
			parts = []llms.ContentPart{
				{
					Type: llms.PartTypeImage,
					Image: &llms.ImageContent{
						Source:    llms.ImageSourceBase64,
						Data:      data,
						MediaType: mediaType,
					},
				},
			}
		default:
			parts = []llms.ContentPart{
				{Type: llms.PartType(partType), Text: data},
			}
		}

		// Should not panic
		_, _ = convertContentParts(parts)
	})
}

// FuzzConvertToolChoice tests tool choice conversion with arbitrary input.
func FuzzConvertToolChoice(f *testing.F) {
	f.Add("auto")
	f.Add("required")
	f.Add("none")
	f.Add("")
	f.Add("random_string")
	f.Add("{}")

	f.Fuzz(func(t *testing.T, choice string) {
		// Should not panic with any string input
		_ = convertToolChoice(&llms.ToolChoice{Type: llms.ToolChoiceType(choice)})
	})
}

// FuzzConvertStopReason tests stop reason conversion.
func FuzzConvertStopReason(f *testing.F) {
	f.Add("end_turn")
	f.Add("tool_use")
	f.Add("max_tokens")
	f.Add("stop_sequence")
	f.Add("")
	f.Add("unknown_reason")
	f.Add("null")

	f.Fuzz(func(t *testing.T, reason string) {
		result := convertStopReason(reason)

		// Should always return a non-empty string (either mapped or original)
		if result == "" && reason != "" {
			t.Errorf("expected non-empty result for non-empty input %q", reason)
		}
	})
}

// FuzzConvertResponse tests response conversion with arbitrary content.
func FuzzConvertResponse(f *testing.F) {
	f.Add("Hello", "end_turn", 10, 5)
	f.Add("", "", 0, 0)
	f.Add("{\"json\": true}", "tool_use", 100, 50)

	f.Fuzz(func(t *testing.T, content, stopReason string, inputTokens, outputTokens int) {
		resp := &anthropicapi.MessagesResponse{
			Content: []anthropicapi.ContentPart{
				{Type: "text", Text: content},
			},
			StopReason: stopReason,
			Usage: anthropicapi.Usage{
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
			},
		}

		// Should not panic
		result := convertResponse(resp)

		// Basic invariants
		if result == nil {
			t.Error("expected non-nil result")
		}
	})
}

// FuzzConvertTools tests tool conversion with arbitrary function definitions.
func FuzzConvertTools(f *testing.F) {
	f.Add("function_name", "A description", "{}")
	f.Add("", "", "")
	f.Add("func", "desc", "{\"invalid json")
	f.Add("get_weather", "Get the weather for a location", `{"type": "object", "properties": {"location": {"type": "string"}}}`)

	f.Fuzz(func(t *testing.T, name, description, params string) {
		tools := []llms.Tool{
			{
				Type: "function",
				Function: &llms.FunctionDefinition{
					Name:        name,
					Description: description,
					// Parameters would need to be map[string]any, but we can't easily fuzz that
					// So we skip this for now and just test the function path
				},
			},
		}

		// Should not panic
		result := convertTools(tools)

		if len(tools) > 0 && len(result) != len(tools) {
			// Tools with Function defined should be converted
			t.Logf("input tools: %d, output tools: %d", len(tools), len(result))
		}
	})
}
