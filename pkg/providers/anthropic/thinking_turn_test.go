package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func call(id string) []llms.ToolCall {
	return []llms.ToolCall{{ID: id, Type: llms.ToolTypeFunction, Function: &llms.FunctionCall{Name: "f", Arguments: "{}"}}}
}

func signed(provider llms.Provider) *llms.ReasoningContent {
	return &llms.ReasoningContent{Content: "t", Signature: "sig-" + string(provider), Provider: provider}
}

func TestSuspendThinkingForUnsignedTurn(t *testing.T) {
	thinking := llms.WithReasoningBudget(2048)
	loop := func(first *llms.ReasoningContent) []llms.Message {
		return []llms.Message{
			{Role: llms.RoleUser, Content: "q"},
			{Role: llms.RoleAssistant, Reasoning: first, ToolCalls: call("toolu_1")},
			{Role: llms.RoleTool, ToolCallID: "toolu_1", Content: "r1"},
			{Role: llms.RoleAssistant, ToolCalls: call("toolu_2")},
			{Role: llms.RoleTool, ToolCallID: "toolu_2", Content: "r2"},
		}
	}
	tests := []struct {
		name  string
		model string
		msgs  []llms.Message
		want  bool
	}{
		{"own block on step 1 keeps thinking at step 3", "claude-opus-4-5", loop(signed(llms.ProviderAnthropic)), false},
		{"foreign-only turn suspends", "claude-opus-4-5", loop(signed(llms.ProviderGemini)), true},
		{"turn with no reasoning at all suspends", "claude-opus-4-5", loop(nil), true},
		{"adaptive thinking is exempt", "claude-opus-4-8", loop(signed(llms.ProviderGemini)), false},
		{"always-on model is left alone", "claude-fable-5-1", loop(signed(llms.ProviderGemini)), false},
		{"no turn in progress", "claude-opus-4-5", []llms.Message{{Role: llms.RoleUser, Content: "q"}}, false},
		{"earlier turn without thinking is ignored", "claude-opus-4-5", append(loop(nil),
			llms.Message{Role: llms.RoleAssistant, Content: "done"},
			llms.Message{Role: llms.RoleUser, Content: "next"}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClientFor(t, tt.model)
			opts := llms.ApplyOptions(thinking)
			prepared, err := prepare(tt.msgs, opts)
			if err != nil {
				t.Fatal(err)
			}
			req, err := c.buildRequest(prepared, opts, false)
			if err != nil {
				t.Fatal(err)
			}
			hadThinking := req.Thinking != nil
			got := suspendThinkingForUnsignedTurn(req, prepared)
			if got != tt.want {
				t.Fatalf("suspended = %v, want %v", got, tt.want)
			}
			if got && req.Thinking != nil {
				t.Error("reported suspension but thinking is still set")
			}
			if !got && hadThinking && req.Thinking == nil {
				t.Error("thinking removed without reporting it")
			}
		})
	}
}

func TestGenerateContent_ReportsThinkingSuspension(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m","type":"message","role":"assistant","model":"claude-opus-4-5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()
	c, err := New(WithAPIKey("k"), WithModel("claude-opus-4-5"), WithBaseURL(server.URL+"/v1"), WithAllowPrivateIPs(), WithAllowHTTP())
	if err != nil {
		t.Fatal(err)
	}
	msgs := []llms.Message{
		{Role: llms.RoleUser, Content: "q"},
		{Role: llms.RoleAssistant, Reasoning: signed(llms.ProviderOpenAI), ToolCalls: call("call_1")},
		{Role: llms.RoleTool, ToolCallID: "call_1", Content: "r"},
	}
	resp, err := c.GenerateContent(context.Background(), msgs, llms.WithReasoningBudget(2048))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, `"thinking"`) {
		t.Errorf("request still asks for thinking: %s", body)
	}
	if len(resp.Adjustments) != 1 || resp.Adjustments[0] != adjustmentThinkingSuspended {
		t.Errorf("Adjustments = %v, want [%s]", resp.Adjustments, adjustmentThinkingSuspended)
	}
}
