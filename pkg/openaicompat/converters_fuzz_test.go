package openaicompat

import (
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v5"
)

// The openaicompat converter is the widest-blast-radius converter in the SDK
// (~17 providers route through it) and was previously the only major converter
// without fuzz coverage. These targets assert the convert functions never panic
// on arbitrary input, and that content-part conversion never emits a malformed
// empty-type part (the WS-4a bug class).

// FuzzConvertMessages fuzzes the shared chat message converter.
func FuzzConvertMessages(f *testing.F) {
	f.Add("hello", "be terse", "sure")
	f.Add("", "", "")
	f.Add(`{"k":"v"}`, "\x00\x01\x02", "🎉 日本語 العربية")
	f.Add("<script>alert(1)</script>", string(make([]byte, 5000)), "\n\t\r")
	f.Fuzz(func(t *testing.T, user, system, assistant string) {
		msgs := []llms.Message{
			{Role: llms.RoleSystem, Content: system},
			{Role: llms.RoleUser, Content: user},
			{Role: llms.RoleAssistant, Content: assistant},
		}
		_ = ConvertMessages(msgs) // must not panic
	})
}

// FuzzConvertContentParts fuzzes vision content conversion and asserts no
// malformed empty-type part is emitted (regression guard for WS-4a).
func FuzzConvertContentParts(f *testing.F) {
	f.Add("text", "sample", "")
	f.Add("image", "b64data", "image/png")
	f.Add("image", "", "")
	f.Add("audio", "data", "")
	f.Add("", "", "")
	f.Fuzz(func(t *testing.T, partType, data, mediaType string) {
		var parts []llms.ContentPart
		switch partType {
		case "text":
			parts = []llms.ContentPart{{Type: llms.PartTypeText, Text: data}}
		case "image":
			parts = []llms.ContentPart{{Type: llms.PartTypeImage, Image: &llms.ImageContent{
				Source: llms.ImageSourceBase64, Data: data, MediaType: mediaType,
			}}}
		default:
			parts = []llms.ContentPart{{Type: llms.PartType(partType), Text: data}}
		}
		for _, c := range convertContentParts(parts) {
			if c.Type == "" {
				t.Errorf("emitted a malformed empty-type content part for input %q", partType)
			}
		}
	})
}

// FuzzConvertResponsesResponse fuzzes the Responses converter.
func FuzzConvertResponsesResponse(f *testing.F) {
	f.Add("completed", "output_text", "hi", "sig")
	f.Add("failed", "refusal", "no", "")
	f.Add("incomplete", "", "", "")
	f.Add("", "unknown_type", "\x00", string(make([]byte, 2000)))
	f.Fuzz(func(t *testing.T, status, contentType, text, summary string) {
		resp := &ResponsesResponse{
			Status: status,
			Output: []ResponsesOutputItem{
				{Type: itemTypeMessage, Content: []ResponsesOutputContent{{Type: contentType, Text: text, Refusal: text}}},
				{Type: itemTypeReasoning, Summary: []ResponsesSummaryPart{{Type: "summary_text", Text: summary}}},
			},
		}
		_ = ConvertResponsesResponse(resp) // must not panic
	})
}
