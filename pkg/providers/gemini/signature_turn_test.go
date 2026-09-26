package gemini

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func geminiCall(id, sig string, p llms.Provider) llms.ToolCall {
	return llms.ToolCall{ID: id, Type: llms.ToolTypeFunction, Function: &llms.FunctionCall{Name: "f", Arguments: "{}"},
		Signature: sig, SignatureProvider: p}
}

func TestSkipValidationForUnsignedCalls(t *testing.T) {
	turn := func(calls ...llms.ToolCall) []llms.Message {
		return []llms.Message{
			{Role: llms.RoleUser, Content: "old"},
			{Role: llms.RoleAssistant, ToolCalls: []llms.ToolCall{geminiCall("old1", "", "")}},
			{Role: llms.RoleTool, ToolCallID: "old1", Content: "x"},
			{Role: llms.RoleAssistant, Content: "done"},
			{Role: llms.RoleUser, Content: "q"},
			{Role: llms.RoleAssistant, ToolCalls: calls},
			{Role: llms.RoleTool, ToolCallID: calls[0].ID, Content: "r"},
		}
	}
	t.Run("own signed parallel calls are untouched", func(t *testing.T) {
		in := turn(geminiCall("a", "own", llms.ProviderGemini), geminiCall("b", "", ""))
		out, skipped := skipValidationForUnsignedCalls(in, "gemini-3-pro")
		if skipped || &out[0] != &in[0] {
			t.Errorf("skipped = %v; own-signed calls must not get the placeholder", skipped)
		}
	})
	t.Run("unsigned first call gets the placeholder, second is left alone", func(t *testing.T) {
		in := turn(geminiCall("a", "", ""), geminiCall("b", "", ""))
		out, skipped := skipValidationForUnsignedCalls(in, "gemini-3.1-flash")
		if !skipped {
			t.Fatal("expected the placeholder")
		}
		calls := out[5].ToolCalls
		if calls[0].Signature != skipSignatureValidator || calls[1].Signature != "" {
			t.Errorf("signatures = %q/%q", calls[0].Signature, calls[1].Signature)
		}
		if in[5].ToolCalls[0].Signature != "" {
			t.Error("input modified")
		}
		if out[1].ToolCalls[0].Signature != "" {
			t.Error("a call from an earlier turn was changed")
		}
	})
	t.Run("models before Gemini 3 are left alone", func(t *testing.T) {
		in := turn(geminiCall("a", "", ""))
		if _, skipped := skipValidationForUnsignedCalls(in, "gemini-2.5-pro"); skipped {
			t.Error("placeholder applied to Gemini 2.5")
		}
	})
}

func TestGenerateContent_ReportsSignatureValidatorSkipped(t *testing.T) {
	c, last := geminiServer(t, `{"candidates":[{"finishReason":"STOP","content":{"role":"model","parts":[{"text":"ok"}]}}]}`)
	msgs := []llms.Message{
		{Role: llms.RoleUser, Content: "q"},
		{Role: llms.RoleAssistant, ToolCalls: []llms.ToolCall{geminiCall("c1", "anthropic-sig", llms.ProviderAnthropic)}},
		{Role: llms.RoleTool, ToolCallID: "c1", Name: "f", Content: "{}"},
	}
	resp, err := c.GenerateContent(context.Background(), msgs, llms.WithModel("gemini-3-pro"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(*last, skipSignatureValidator) || strings.Contains(*last, "anthropic-sig") {
		t.Errorf("request body = %s", *last)
	}
	if len(resp.Adjustments) != 1 || resp.Adjustments[0] != adjustmentSignatureValidatorSkipped {
		t.Errorf("Adjustments = %v", resp.Adjustments)
	}
}

func TestStream_ReportsSignatureValidatorSkipped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}` + "\n\n"))
	}))
	defer server.Close()
	c, err := New(WithAPIKey("k"), WithBaseURL(server.URL), WithAllowPrivateIPs(), WithAllowHTTP())
	if err != nil {
		t.Fatal(err)
	}
	msgs := []llms.Message{
		{Role: llms.RoleUser, Content: "q"},
		{Role: llms.RoleAssistant, ToolCalls: []llms.ToolCall{geminiCall("c1", "", "")}},
		{Role: llms.RoleTool, ToolCallID: "c1", Name: "f", Content: "{}"},
	}
	stream, err := c.Stream(context.Background(), msgs, llms.WithModel("gemini-3-pro"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := llms.CollectStream(stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Adjustments) != 1 || res.Adjustments[0] != adjustmentSignatureValidatorSkipped {
		t.Errorf("stream Adjustments = %v", res.Adjustments)
	}
}
