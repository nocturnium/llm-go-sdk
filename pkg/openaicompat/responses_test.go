package openaicompat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v3"
)

func TestBuildResponsesRequest_MessagesAndInstructions(t *testing.T) {
	msgs := []llms.Message{
		{Role: llms.RoleSystem, Content: "be terse"},
		{Role: llms.RoleSystem, Content: "and kind"},
		{Role: llms.RoleUser, Content: "hi"},
	}
	req := BuildResponsesRequest("gpt-4o", msgs, llms.ApplyOptions(), false)

	if req.Instructions != "be terse\n\nand kind" {
		t.Errorf("instructions = %q", req.Instructions)
	}
	if len(req.Input) != 1 || req.Input[0].Type != "message" || req.Input[0].Role != "user" {
		t.Fatalf("input = %+v", req.Input)
	}
	if len(req.Input[0].Content) != 1 || req.Input[0].Content[0].Type != "input_text" || req.Input[0].Content[0].Text != "hi" {
		t.Errorf("user content = %+v", req.Input[0].Content)
	}
}

func TestBuildResponsesRequest_ToolCallRoundTrip(t *testing.T) {
	msgs := []llms.Message{
		{Role: llms.RoleUser, Content: "weather?"},
		{Role: llms.RoleAssistant, Content: "checking", ToolCalls: []llms.ToolCall{
			{ID: "call_1", Type: llms.ToolTypeFunction, Function: &llms.FunctionCall{Name: "get_weather", Arguments: `{"city":"Paris"}`}},
		}},
		{Role: llms.RoleTool, ToolCallID: "call_1", Content: "sunny"},
	}
	req := BuildResponsesRequest("gpt-4o", msgs, llms.ApplyOptions(), false)

	// Expect: user message, assistant message (text), function_call, function_call_output
	if len(req.Input) != 4 {
		t.Fatalf("got %d input items, want 4: %+v", len(req.Input), req.Input)
	}
	if req.Input[1].Type != "message" || req.Input[1].Role != "assistant" ||
		req.Input[1].Content[0].Type != "output_text" {
		t.Errorf("assistant message item = %+v", req.Input[1])
	}
	fc := req.Input[2]
	if fc.Type != "function_call" || fc.CallID != "call_1" || fc.Name != "get_weather" || fc.Arguments != `{"city":"Paris"}` {
		t.Errorf("function_call item = %+v", fc)
	}
	out := req.Input[3]
	if out.Type != "function_call_output" || out.CallID != "call_1" || out.Output != "sunny" {
		t.Errorf("function_call_output item = %+v", out)
	}
}

func TestBuildResponsesRequest_ToolsFlattenedAndReasoningModel(t *testing.T) {
	tool := llms.NewFunctionTool("f", "desc", map[string]any{"type": "object"})
	temp := 0.7
	opts := llms.ApplyOptions(
		llms.WithTools([]llms.Tool{tool}),
		llms.WithTemperature(temp),
		llms.WithReasoningEffort(llms.ReasoningEffortHigh),
	)
	// Reasoning model must drop temperature; tools are flattened (no nested function).
	req := BuildResponsesRequest("o3-mini", nil, opts, false)

	if req.Temperature != nil {
		t.Error("temperature should be dropped for reasoning model")
	}
	if req.Reasoning == nil || req.Reasoning.Effort != "high" {
		t.Errorf("reasoning = %+v", req.Reasoning)
	}
	if len(req.Tools) != 1 || req.Tools[0].Type != "function" || req.Tools[0].Name != "f" {
		t.Fatalf("tools = %+v", req.Tools)
	}
	// The flattened tool must marshal with name at top level, not nested under "function".
	raw, _ := json.Marshal(req.Tools[0])
	if got := string(raw); !strings.Contains(got, `"name":"f"`) || strings.Contains(got, `"function":`) {
		t.Errorf("tool JSON not flattened: %s", got)
	}
}

func TestBuildResponsesRequest_StateFromExtraBody(t *testing.T) {
	opts := llms.ApplyOptions(
		llms.WithExtraBodyParam("previous_response_id", "resp_123"),
		llms.WithExtraBodyParam("store", true),
	)
	req := BuildResponsesRequest("gpt-4o", []llms.Message{{Role: llms.RoleUser, Content: "x"}}, opts, false)
	if req.PreviousResponseID != "resp_123" {
		t.Errorf("previous_response_id = %q", req.PreviousResponseID)
	}
	if req.Store == nil || !*req.Store {
		t.Errorf("store = %v", req.Store)
	}
}

func TestConvertResponsesResponse_TextReasoningUsage(t *testing.T) {
	resp := &ResponsesResponse{
		Status: "completed",
		Output: []ResponsesOutputItem{
			{Type: "reasoning", Summary: []ResponsesSummaryPart{{Type: "summary_text", Text: "thinking..."}}},
			{Type: "message", Role: "assistant", Content: []ResponsesOutputContent{{Type: "output_text", Text: "Hello!"}}},
		},
		Usage: &ResponsesUsage{
			InputTokens:  100,
			OutputTokens: 20,
			TotalTokens:  120,
			InputTokensDetails: &struct {
				CachedTokens int `json:"cached_tokens"`
			}{CachedTokens: 30},
			OutputTokensDetails: &struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			}{ReasoningTokens: 8},
		},
	}
	out := ConvertResponsesResponse(resp)

	if out.Content != "Hello!" {
		t.Errorf("content = %q", out.Content)
	}
	if out.ReasoningText() != "thinking..." {
		t.Errorf("reasoning = %q", out.ReasoningText())
	}
	if out.FinishReason != llms.FinishReasonStop {
		t.Errorf("finish = %q", out.FinishReason)
	}
	// PromptTokens excludes the cached subset (100 - 30 = 70).
	if out.Usage.PromptTokens != 70 || out.Usage.CacheReadTokens != 30 {
		t.Errorf("usage prompt/cache = %d/%d, want 70/30", out.Usage.PromptTokens, out.Usage.CacheReadTokens)
	}
	if out.Usage.ReasoningTokens != 8 || out.Reasoning.Tokens != 8 {
		t.Errorf("reasoning tokens = %d / %d", out.Usage.ReasoningTokens, out.Reasoning.Tokens)
	}
}

func TestConvertResponsesResponse_ToolCallsAndLength(t *testing.T) {
	toolResp := &ResponsesResponse{
		Status: "completed",
		Output: []ResponsesOutputItem{
			{Type: "function_call", CallID: "call_9", Name: "do_it", Arguments: `{"a":1}`},
		},
	}
	out := ConvertResponsesResponse(toolResp)
	if len(out.ToolCalls) != 1 || out.ToolCalls[0].ID != "call_9" || out.ToolCalls[0].Function.Name != "do_it" {
		t.Fatalf("tool calls = %+v", out.ToolCalls)
	}
	if out.FinishReason != llms.FinishReasonToolCalls {
		t.Errorf("finish = %q, want tool_calls", out.FinishReason)
	}

	incomplete := &ResponsesResponse{
		Status: "incomplete",
		IncompleteDetails: &struct {
			Reason string `json:"reason"`
		}{Reason: "max_output_tokens"},
		Output: []ResponsesOutputItem{{Type: "message", Content: []ResponsesOutputContent{{Type: "output_text", Text: "partial"}}}},
	}
	if fr := ConvertResponsesResponse(incomplete).FinishReason; fr != llms.FinishReasonLength {
		t.Errorf("incomplete finish = %q, want length", fr)
	}
}

// Both the chat-completions and Responses converters must surface the provider
// response id on llms.Response so callers can chain turns.
func TestConverters_SurfaceResponseID(t *testing.T) {
	chat := ConvertResponse(&ChatCompletionResponse{
		ID:      "chatcmpl-1",
		Choices: []Choice{{Message: &ChatMessage{Role: "assistant", ContentValue: "hi"}}},
	})
	if chat.ID != "chatcmpl-1" {
		t.Errorf("chat path ID = %q, want chatcmpl-1", chat.ID)
	}

	responses := ConvertResponsesResponse(&ResponsesResponse{
		ID:     "resp-1",
		Status: "completed",
		Output: []ResponsesOutputItem{{Type: "message", Content: []ResponsesOutputContent{{Type: "output_text", Text: "hi"}}}},
	})
	if responses.ID != "resp-1" {
		t.Errorf("responses path ID = %q, want resp-1", responses.ID)
	}
}

func TestClient_CreateResponse_RoundTrip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("path = %s, want /responses", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"input"`) {
			t.Errorf("request body missing input: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi there"}]}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "k", AllowPrivateIPs: true, AllowHTTP: true})
	resp, err := client.CreateResponse(context.Background(), &ResponsesRequest{
		Model: "gpt-4o",
		Input: []ResponsesInputItem{{Type: "message", Role: "user", Content: []ResponsesContentPart{{Type: "input_text", Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("CreateResponse: %v", err)
	}
	out := ConvertResponsesResponse(resp)
	if out.Content != "hi there" {
		t.Errorf("content = %q", out.Content)
	}
}

func TestBaseProvider_RoutesToResponsesAPI(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"routed"}]}]}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "k", AllowPrivateIPs: true, AllowHTTP: true})
	p := NewBaseProvider(client, ProviderConfig{
		Provider:        llms.ProviderOpenAI,
		ProviderName:    "openai",
		DefaultModel:    "gpt-4o",
		UseResponsesAPI: true,
	})

	resp, err := p.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if gotPath != "/responses" {
		t.Errorf("routed to %s, want /responses", gotPath)
	}
	if resp.Content != "routed" {
		t.Errorf("content = %q", resp.Content)
	}
}
