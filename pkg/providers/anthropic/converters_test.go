package anthropic

import (
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/anthropicapi"
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

// TestConvertMessages_ReEmitsThinkingBlock pins that an assistant turn carrying a
// signed Reasoning block is re-emitted with the thinking block FIRST, so an
// agentic extended-thinking + tools loop does not 400 on turn 2 ("must start
// with a thinking block").
func TestConvertMessages_ReEmitsThinkingBlock(t *testing.T) {
	msgs := []llms.Message{
		{Role: llms.RoleUser, Content: "weather?"},
		{
			Role:      llms.RoleAssistant,
			Content:   "let me check",
			Reasoning: &llms.ReasoningContent{Content: "thinking...", Signature: "sig123"},
			ToolCalls: []llms.ToolCall{{ID: "t1", Type: llms.ToolTypeFunction, Function: &llms.FunctionCall{Name: "get_weather", Arguments: "{}"}}},
		},
	}
	out, err := convertMessages(msgs)
	if err != nil {
		t.Fatalf("convertMessages: %v", err)
	}
	var asst *anthropicapi.Message
	for i := range out {
		if out[i].Role == "assistant" {
			asst = &out[i]
			break
		}
	}
	if asst == nil || len(asst.Content) == 0 {
		t.Fatalf("no assistant message: %+v", out)
	}
	first := asst.Content[0]
	if first.Type != "thinking" || first.Signature != "sig123" || first.Thinking != "thinking..." {
		t.Errorf("assistant turn must START with the signed thinking block, got %+v", asst.Content)
	}
}

// TestConvertMessages_NoThinkingWithoutSignature pins that an unsigned reasoning
// block (e.g. a summary) is NOT replayed as a thinking block, which Anthropic
// would reject.
func TestConvertMessages_NoThinkingWithoutSignature(t *testing.T) {
	msgs := []llms.Message{{
		Role:      llms.RoleAssistant,
		Content:   "hi",
		Reasoning: &llms.ReasoningContent{Content: "summary only", Signature: ""},
	}}
	out, err := convertMessages(msgs)
	if err != nil {
		t.Fatalf("convertMessages: %v", err)
	}
	for _, m := range out {
		for _, c := range m.Content {
			if c.Type == "thinking" {
				t.Errorf("must not emit an unsigned thinking block: %+v", c)
			}
		}
	}
}
