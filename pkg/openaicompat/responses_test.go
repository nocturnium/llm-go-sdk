package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
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

// TestConvertResponsesResponse_Refusal pins that a safety refusal is surfaced as
// content plus a content_filter finish reason, not a silent empty "stop".
func TestConvertResponsesResponse_Refusal(t *testing.T) {
	resp := &ResponsesResponse{
		ID:     "r",
		Status: "completed",
		Output: []ResponsesOutputItem{
			{Type: itemTypeMessage, Content: []ResponsesOutputContent{
				{Type: "refusal", Refusal: "I can't help with that."},
			}},
		},
	}
	out := ConvertResponsesResponse(resp)
	if out.Content != "I can't help with that." {
		t.Errorf("refusal text not surfaced: Content = %q", out.Content)
	}
	if out.FinishReason != llms.FinishReasonContentFilter {
		t.Errorf("FinishReason = %q, want %q", out.FinishReason, llms.FinishReasonContentFilter)
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

// TestBaseProvider_ResponsesFailedStatusReturnsError is the golden test for the
// non-streaming Responses failure path: a 200 body with status:"failed" must
// surface as an error, not a silent empty completion.
func TestBaseProvider_ResponsesFailedStatusReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"r","status":"failed","error":{"message":"safety system triggered","code":"content_policy"},"output":[]}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL, APIKey: "k", AllowPrivateIPs: true, AllowHTTP: true})
	p := NewBaseProvider(client, ProviderConfig{
		Provider:        llms.ProviderOpenAI,
		DefaultModel:    "gpt-4o",
		UseResponsesAPI: true,
	})

	resp, err := p.GenerateContent(context.Background(), []llms.Message{{Role: llms.RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatalf("expected error for status:failed, got resp=%+v", resp)
	}
	if !strings.Contains(err.Error(), "safety system triggered") {
		t.Errorf("error does not surface the API message: %v", err)
	}
	var apiErr *llms.APIError
	if !errors.As(err, &apiErr) {
		t.Errorf("expected *llms.APIError, got %T (%v)", err, err)
	} else if apiErr.Code != "content_policy" {
		t.Errorf("APIError.Code = %q, want content_policy", apiErr.Code)
	}
}

func TestResponsesResponseError(t *testing.T) {
	mustUnmarshal := func(body string) *ResponsesResponse {
		var r ResponsesResponse
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return &r
	}

	if err := responsesResponseError(mustUnmarshal(`{"status":"completed"}`)); err != nil {
		t.Errorf("completed: unexpected error %v", err)
	}
	if err := responsesResponseError(mustUnmarshal(`{"status":"incomplete"}`)); err != nil {
		t.Errorf("incomplete: unexpected error %v", err)
	}
	if responsesResponseError(nil) != nil {
		t.Error("nil response should not error")
	}

	err := responsesResponseError(mustUnmarshal(`{"status":"failed","error":{"message":"boom","code":"server_error"}}`))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("failed status: got %v", err)
	}
	var apiErr *llms.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "server_error" {
		t.Errorf("want *llms.APIError code=server_error, got %v", err)
	}

	// A populated error object surfaces even when status is not "failed".
	if responsesResponseError(mustUnmarshal(`{"status":"completed","error":{"message":"partial"}}`)) == nil {
		t.Error("populated error object should surface as an error")
	}
}

// TestBuildResponsesRequest_MergesExtraBody pins that CallOptions.ExtraBody
// escape-hatch keys (service_tier, metadata, ...) are merged into the Responses
// request body (matching the chat path), while keys already mapped to typed
// fields or colliding with a typed field are left to the typed value.
func TestBuildResponsesRequest_MergesExtraBody(t *testing.T) {
	opts := llms.ApplyOptions(llms.WithExtraBody(map[string]any{
		"service_tier": "flex",
		"metadata":     map[string]any{"user_id": "u1"},
		"store":        true,                  // consumed -> typed Store field
		"model":        "SHOULD_NOT_OVERRIDE", // collides with typed Model field
	}))
	req := BuildResponsesRequest("gpt-4o", nil, opts, false)
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["service_tier"] != "flex" {
		t.Errorf("service_tier escape-hatch key not merged: %s", data)
	}
	if _, ok := m["metadata"]; !ok {
		t.Errorf("metadata not merged: %s", data)
	}
	if m["model"] != "gpt-4o" {
		t.Errorf("ExtraBody must not override the typed model field: got %v", m["model"])
	}
	if m["store"] != true {
		t.Errorf("store should be the typed field value, got %v", m["store"])
	}
}
