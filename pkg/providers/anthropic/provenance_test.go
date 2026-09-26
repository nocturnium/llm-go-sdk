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

func TestStream_IdentityAndToolCallContract(t *testing.T) {
	events := []string{
		sseMessageStart,
		`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-1"}}`,
		`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
		`event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"lookup"}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
		`event: content_block_stop
data: {"type":"content_block_stop","index":1}`,
		`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
		`event: message_stop
data: {"type":"message_stop"}`,
	}
	c := sseServer(t, events)
	chunks, err := c.Stream(context.Background(), userMsg("go"))
	if err != nil {
		t.Fatal(err)
	}
	var final llms.StreamChunk
	for ch := range chunks {
		if !ch.Done && len(ch.ToolCalls) > 0 {
			t.Errorf("non-final chunk carries tool calls: %+v", ch.ToolCalls)
		}
		if ch.Reasoning != nil && ch.Reasoning.Provider != llms.ProviderAnthropic {
			t.Errorf("reasoning chunk stamp = %q", ch.Reasoning.Provider)
		}
		if ch.Done {
			final = ch
		}
	}
	if final.Provider != llms.ProviderAnthropic || final.Model != c.Model() || final.ModelVersion != "m" {
		t.Errorf("final identity = %q/%q/%q, want anthropic/%s/m", final.Provider, final.Model, final.ModelVersion, c.Model())
	}
	if final.Reasoning == nil || final.Reasoning.Signature != "sig-1" || final.Reasoning.Provider != llms.ProviderAnthropic {
		t.Errorf("final reasoning = %+v, want the stamped signature", final.Reasoning)
	}
	if len(final.ToolCalls) != 1 || final.ToolCalls[0].ID != "toolu_1" {
		t.Errorf("final tool calls = %+v", final.ToolCalls)
	}
}

// TestGenerateContent_DropsForeignThinking checks that a thinking block another
// provider signed is not sent (Anthropic rejects a signature it did not issue)
// while Anthropic's own block is, and that the response is stamped.
func TestGenerateContent_DropsForeignThinking(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-reported","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":1}}`))
	}))
	defer server.Close()
	c, err := New(WithAPIKey("k"), WithBaseURL(server.URL+"/v1"), WithAllowPrivateIPs(), WithAllowHTTP())
	if err != nil {
		t.Fatal(err)
	}

	msgs := []llms.Message{
		{Role: llms.RoleUser, Content: "q1"},
		{Role: llms.RoleAssistant, Content: "a1", Reasoning: &llms.ReasoningContent{
			Content: "own thought", Signature: "own-sig", Provider: llms.ProviderAnthropic,
		}},
		{Role: llms.RoleUser, Content: "q2"},
		{Role: llms.RoleAssistant, Content: "a2", Reasoning: &llms.ReasoningContent{
			Content: "gemini thought", Signature: "gemini-sig", Provider: llms.ProviderGemini,
		}},
		{Role: llms.RoleUser, Content: "q3"},
	}
	resp, err := c.GenerateContent(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "own-sig") {
		t.Errorf("own thinking block missing from request: %s", body)
	}
	if strings.Contains(body, "gemini-sig") || strings.Contains(body, "gemini thought") {
		t.Errorf("foreign thinking block sent: %s", body)
	}
	if resp.Provider != llms.ProviderAnthropic || resp.Model != c.Model() || resp.ModelVersion != "claude-reported" {
		t.Errorf("identity = %q/%q/%q", resp.Provider, resp.Model, resp.ModelVersion)
	}
}
