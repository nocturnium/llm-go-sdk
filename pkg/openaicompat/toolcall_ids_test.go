package openaicompat_test

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/testutil"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

var nineChar = regexp.MustCompile(`^[a-zA-Z0-9]{9}$`)

func TestNineCharToolCallID(t *testing.T) {
	if got := openaicompat.NineCharToolCallID("abcDEF123"); got != "abcDEF123" {
		t.Errorf("valid ID changed to %q", got)
	}
	for _, id := range []string{"call_abc123", "toolu_01ABC", "", "short"} {
		got := openaicompat.NineCharToolCallID(id)
		if !nineChar.MatchString(got) {
			t.Errorf("NineCharToolCallID(%q) = %q, not nine alphanumerics", id, got)
		}
		if again := openaicompat.NineCharToolCallID(id); again != got {
			t.Errorf("NineCharToolCallID(%q) not deterministic: %q then %q", id, got, again)
		}
	}
	if openaicompat.NineCharToolCallID("call_a") == openaicompat.NineCharToolCallID("call_b") {
		t.Error("distinct IDs collided")
	}
}

func TestBaseProvider_ToolCallIDFormat(t *testing.T) {
	server := testutil.NewMockOpenAICompatibleServer(testutil.WithChatCompletionResponse(openaicompat.ChatCompletionResponse{
		ID: "x", Model: "m",
		Choices: []openaicompat.Choice{{Message: &openaicompat.ChatMessage{Role: "assistant", ContentValue: "ok"}, FinishReason: "stop"}},
	}))
	defer server.Close()
	client := openaicompat.NewClient(openaicompat.ClientConfig{BaseURL: server.URL(), APIKey: "k", AllowPrivateIPs: true, AllowHTTP: true})
	p := openaicompat.NewBaseProvider(client, openaicompat.ProviderConfig{
		Provider: llms.ProviderMistral, DefaultModel: "m", ToolCallIDFormat: openaicompat.ToolCallIDNineChar,
	})
	history := func(id string) []llms.Message {
		return []llms.Message{
			{Role: llms.RoleUser, Content: "q"},
			{Role: llms.RoleAssistant, ToolCalls: []llms.ToolCall{{ID: id, Type: llms.ToolTypeFunction, Function: &llms.FunctionCall{Name: "f", Arguments: "{}"}}}},
			{Role: llms.RoleTool, ToolCallID: id, Content: "r"},
		}
	}

	t.Run("foreign IDs are rewritten on call and result alike", func(t *testing.T) {
		resp, err := p.GenerateContent(context.Background(), history("toolu_01XYZ"))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(server.LastRequest().Body)
		var req struct {
			Messages []struct {
				ToolCalls []struct {
					ID string `json:"id"`
				} `json:"tool_calls"`
				ToolCallID string `json:"tool_call_id"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatal(err)
		}
		callID, resultID := req.Messages[1].ToolCalls[0].ID, req.Messages[2].ToolCallID
		if !nineChar.MatchString(callID) || callID != resultID {
			t.Errorf("call ID %q, result ID %q: want the same nine-char ID", callID, resultID)
		}
		if len(resp.Adjustments) != 1 || resp.Adjustments[0] != "mistral.tool_ids_rewritten" {
			t.Errorf("Adjustments = %v", resp.Adjustments)
		}
	})

	t.Run("valid IDs pass through without an adjustment", func(t *testing.T) {
		resp, err := p.GenerateContent(context.Background(), history("abcDEF123"))
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Adjustments) != 0 {
			t.Errorf("Adjustments = %v, want none", resp.Adjustments)
		}
	})
}

func TestBaseProvider_ToolCallIDFormatStream(t *testing.T) {
	server := testutil.NewMockOpenAICompatibleServer(testutil.WithStreamResponse(
		streamContentChunk("ok"),
		streamFinishChunk("stop", nil),
	))
	defer server.Close()
	client := openaicompat.NewClient(openaicompat.ClientConfig{BaseURL: server.URL(), APIKey: "k", AllowPrivateIPs: true, AllowHTTP: true})
	p := openaicompat.NewBaseProvider(client, openaicompat.ProviderConfig{
		Provider: llms.ProviderMistral, DefaultModel: "m", ToolCallIDFormat: openaicompat.ToolCallIDNineChar,
	})
	stream, err := p.Stream(context.Background(), []llms.Message{
		{Role: llms.RoleUser, Content: "q"},
		{Role: llms.RoleAssistant, ToolCalls: []llms.ToolCall{{ID: "toolu_01XYZ", Type: llms.ToolTypeFunction, Function: &llms.FunctionCall{Name: "f", Arguments: "{}"}}}},
		{Role: llms.RoleTool, ToolCallID: "toolu_01XYZ", Content: "r"},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := llms.CollectStream(stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Adjustments) != 1 || res.Adjustments[0] != "mistral.tool_ids_rewritten" {
		t.Errorf("stream Adjustments = %v", res.Adjustments)
	}
}
