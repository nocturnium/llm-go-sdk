package anthropic

import (
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/internal/anthropicapi"
)

func TestConvertMessages_DeduplicatesToolResultsRequestWide(t *testing.T) {
	const toolUseID = "toolu_123"

	messages := []llms.Message{
		{
			Role: llms.RoleAssistant,
			ToolCalls: []llms.ToolCall{
				{
					ID:   toolUseID,
					Type: "function",
					Function: &llms.FunctionCall{
						Name:      "lookup",
						Arguments: `{"query":"whiskey"}`,
					},
				},
			},
		},
		{Role: llms.RoleTool, ToolCallID: toolUseID, Content: "first result"},
		{Role: llms.RoleTool, ToolCallID: toolUseID, Content: "duplicate result"},
	}

	result, err := convertMessages(messages)
	if err != nil {
		t.Fatalf("convertMessages returned error: %v", err)
	}

	toolResults := collectToolResults(result, toolUseID)
	if len(toolResults) != 1 {
		t.Fatalf("expected exactly one tool_result for %q, got %d", toolUseID, len(toolResults))
	}
	if toolResults[0].Content != "first result" {
		t.Fatalf("expected first tool result to be kept, got %q", toolResults[0].Content)
	}
}

func TestConvertMessages_SingleToolResultUnaffected(t *testing.T) {
	const toolUseID = "toolu_456"

	messages := []llms.Message{
		{
			Role: llms.RoleAssistant,
			ToolCalls: []llms.ToolCall{
				{
					ID:   toolUseID,
					Type: "function",
					Function: &llms.FunctionCall{
						Name:      "lookup",
						Arguments: `{"query":"bourbon"}`,
					},
				},
			},
		},
		{Role: llms.RoleTool, ToolCallID: toolUseID, Content: "single result"},
	}

	result, err := convertMessages(messages)
	if err != nil {
		t.Fatalf("convertMessages returned error: %v", err)
	}

	toolResults := collectToolResults(result, toolUseID)
	if len(toolResults) != 1 {
		t.Fatalf("expected one tool_result for %q, got %d", toolUseID, len(toolResults))
	}
	if toolResults[0].Content != "single result" {
		t.Fatalf("expected tool result content to be preserved, got %q", toolResults[0].Content)
	}
}

func collectToolResults(messages []anthropicapi.Message, toolUseID string) []anthropicapi.ContentPart {
	var results []anthropicapi.ContentPart
	for _, msg := range messages {
		for _, part := range msg.Content {
			if part.Type == "tool_result" && part.ToolUseID == toolUseID {
				results = append(results, part)
			}
		}
	}
	return results
}
