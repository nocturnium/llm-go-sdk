package anthropic

// Tests for behaviors aligned with the openaicompat and gemini providers
// (structured streaming, zero-arg tool calls, EOF completion, inline system
// validation, JSON-object mode, ExtraBody, URL images, redacted thinking,
// usage totals, and per-generation thinking/sampling shapes).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/anthropicapi"
)

func newTestClientFor(t *testing.T, model string) *Client {
	t.Helper()
	c, err := New(WithAPIKey("test-key"), WithModel(model))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func userMsg(text string) []llms.Message {
	return []llms.Message{{Role: llms.RoleUser, Content: text}}
}

// sseServer serves the given SSE events once, then closes the connection, and
// returns a client pointed at it.
func sseServer(t *testing.T, events []string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range events {
			_, _ = w.Write([]byte(ev + "\n\n"))
		}
	}))
	t.Cleanup(server.Close)
	c, err := New(WithAPIKey("test-key"), WithBaseURL(server.URL+"/v1"), WithAllowPrivateIPs(), WithAllowHTTP())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func collectStream(t *testing.T, chunks <-chan llms.StreamChunk) (content string, final llms.StreamChunk) {
	t.Helper()
	for ch := range chunks {
		if ch.Error != nil {
			t.Fatalf("stream error: %v", ch.Error)
		}
		content += ch.Content
		if ch.Done {
			final = ch
		}
	}
	return content, final
}

const sseMessageStart = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"m","stop_reason":null,"usage":{"input_tokens":10,"output_tokens":0,"cache_read_input_tokens":7,"cache_creation_input_tokens":3}}}`

func TestStream_StructuredOutputExposesToolInputAsContent(t *testing.T) {
	events := []string{
		sseMessageStart,
		`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"person"}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"name\":"}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"Ada\"}"}}`,
		`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
		`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
		`event: message_stop
data: {"type":"message_stop"}`,
	}
	c := sseServer(t, events)
	chunks, err := c.Stream(context.Background(), userMsg("who?"),
		llms.WithJSONSchema("person", json.RawMessage(`{"type":"object"}`), false))
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	content, final := collectStream(t, chunks)
	if content != `{"name":"Ada"}` {
		t.Errorf("content = %q, want structured JSON", content)
	}
	if len(final.ToolCalls) != 1 || final.ToolCalls[0].Function.Name != "person" {
		t.Errorf("expected the structured tool call to be preserved, got %+v", final.ToolCalls)
	}
	// Usage total includes cached tokens: 10 + 7 + 3 + 5.
	if final.Usage == nil || final.Usage.TotalTokens != 25 {
		t.Errorf("usage = %+v, want TotalTokens 25", final.Usage)
	}
}

func TestStream_ZeroArgToolCallYieldsEmptyObject(t *testing.T) {
	events := []string{
		sseMessageStart,
		`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_time","input":{}}}`,
		`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
		`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`,
		`event: message_stop
data: {"type":"message_stop"}`,
	}
	c := sseServer(t, events)
	chunks, err := c.Stream(context.Background(), userMsg("time?"))
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	_, final := collectStream(t, chunks)
	if len(final.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(final.ToolCalls))
	}
	if got := final.ToolCalls[0].Function.Arguments; got != "{}" {
		t.Errorf("Arguments = %q, want {}", got)
	}
	if !json.Valid([]byte(final.ToolCalls[0].Function.Arguments)) {
		t.Error("arguments must be valid JSON")
	}
}

func TestStream_EOFWithoutMessageStopCompletesNormally(t *testing.T) {
	events := []string{
		sseMessageStart,
		`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`,
		`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
		`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		// no message_stop: the server closes the connection
	}
	c := sseServer(t, events)
	chunks, err := c.Stream(context.Background(), userMsg("hi"))
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	content, final := collectStream(t, chunks)
	if content != "Hello" {
		t.Errorf("content = %q", content)
	}
	if !final.Done || final.FinishReason != llms.FinishReasonStop {
		t.Errorf("expected clean Done/stop terminal chunk, got %+v", final)
	}
}

func TestStream_RedactedThinkingCaptured(t *testing.T) {
	events := []string{
		sseMessageStart,
		`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"redacted_thinking","data":"ENC1"}}`,
		`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
		`event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"ok"}}`,
		`event: message_stop
data: {"type":"message_stop"}`,
	}
	c := sseServer(t, events)
	chunks, err := c.Stream(context.Background(), userMsg("hi"))
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	_, final := collectStream(t, chunks)
	if final.Reasoning == nil {
		t.Fatal("expected reasoning carrying redacted blocks")
	}
	if got := redactedThinkingBlocks(final.Reasoning); len(got) != 1 || got[0] != "ENC1" {
		t.Errorf("redacted blocks = %v", got)
	}
}

func TestConvertResponse_RedactedThinkingRoundTrip(t *testing.T) {
	resp := &anthropicapi.MessagesResponse{
		StopReason: "end_turn",
		Content: []anthropicapi.ContentPart{
			{Type: "thinking", Thinking: "hmm", Signature: "sig"},
			{Type: "redacted_thinking", Data: "ENC"},
			{Type: "text", Text: "answer"},
		},
	}
	r := convertResponse(resp)
	if r.Reasoning == nil || r.Reasoning.Signature != "sig" {
		t.Fatalf("reasoning = %+v", r.Reasoning)
	}
	// Replay on the next turn: thinking first, then redacted, then text.
	msgs, err := convertMessages([]llms.Message{
		{Role: llms.RoleAssistant, Content: "answer", Reasoning: r.Reasoning},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := msgs[0].Content
	if len(got) != 3 || got[0].Type != "thinking" || got[1].Type != "redacted_thinking" || got[1].Data != "ENC" || got[2].Type != "text" {
		t.Errorf("replayed content = %+v", got)
	}
}

func TestGenerateContent_RejectsMidConversationSystemMessage(t *testing.T) {
	c := newTestClientFor(t, "claude-sonnet-4-20250514")
	msgs := []llms.Message{
		{Role: llms.RoleUser, Content: "hi"},
		{Role: llms.RoleSystem, Content: "be terse"},
	}
	if _, err := c.GenerateContent(context.Background(), msgs); err == nil {
		t.Error("GenerateContent: expected validation error for inline system message")
	}
	if _, err := c.Stream(context.Background(), msgs); err == nil {
		t.Error("Stream: expected validation error for inline system message")
	}
}

func TestBuildRequest_JSONObjectModeForcesGenericTool(t *testing.T) {
	c := newTestClientFor(t, "claude-sonnet-4-20250514")
	req, err := c.buildRequest(userMsg("x"), llms.ApplyOptions(llms.WithJSONMode()), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != structuredOutputToolName {
		t.Fatalf("tools = %+v", req.Tools)
	}
	tc, ok := req.ToolChoice.(anthropicapi.ToolChoiceTool)
	if !ok || tc.Name != structuredOutputToolName {
		t.Errorf("tool_choice = %#v", req.ToolChoice)
	}
	schema, _ := json.Marshal(req.Tools[0].InputSchema)
	if !strings.Contains(string(schema), `"type":"object"`) {
		t.Errorf("schema = %s", schema)
	}
}

func TestConvertResponse_JSONObjectModeReturnsToolInput(t *testing.T) {
	resp := &anthropicapi.MessagesResponse{
		StopReason: "tool_use",
		Content: []anthropicapi.ContentPart{
			{Type: "tool_use", ID: "t1", Name: structuredOutputToolName, Input: json.RawMessage(`{"a":1}`)},
		},
	}
	r := convertResponse(resp, structuredOutputToolNameFor(llms.ApplyOptions(llms.WithJSONMode())))
	if r.Content != `{"a":1}` {
		t.Errorf("content = %q", r.Content)
	}
}

func TestBuildRequest_ExtraBodyMergedIntoWire(t *testing.T) {
	c := newTestClientFor(t, "claude-sonnet-4-20250514")
	req, err := c.buildRequest(userMsg("x"), llms.ApplyOptions(
		llms.WithExtraBodyParam("metadata", map[string]any{"user_id": "u1"}),
		llms.WithExtraBodyParam("model", "must-not-override"),
	), false)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	md, _ := wire["metadata"].(map[string]any)
	if md["user_id"] != "u1" {
		t.Errorf("metadata not merged: %s", body)
	}
	if wire["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("standard field must win over ExtraBody: %v", wire["model"])
	}
}

func TestConvertContentParts_URLImage(t *testing.T) {
	parts, err := convertContentParts([]llms.ContentPart{{
		Type:  llms.PartTypeImage,
		Image: &llms.ImageContent{Source: llms.ImageSourceURL, Data: "https://example.com/a.png"},
	}})
	if err != nil {
		t.Fatalf("URL images must be supported: %v", err)
	}
	if len(parts) != 1 || parts[0].Source == nil || parts[0].Source.Type != "url" || parts[0].Source.URL != "https://example.com/a.png" {
		t.Errorf("parts = %+v", parts)
	}
	b, _ := json.Marshal(parts[0].Source)
	if strings.Contains(string(b), "media_type") || strings.Contains(string(b), `"data"`) {
		t.Errorf("url source must omit base64 fields: %s", b)
	}

	_, err = convertContentParts([]llms.ContentPart{{
		Type:  llms.PartTypeImage,
		Image: &llms.ImageContent{Source: llms.ImageSource("ftp")},
	}})
	if !errors.Is(err, ErrUnsupportedImageSource) {
		t.Errorf("unknown source should error, got %v", err)
	}
}

func TestClassifyModel(t *testing.T) {
	cases := map[string]modelGeneration{
		"claude-3-5-sonnet-20241022": genLegacy,
		"claude-sonnet-4-20250514":   genLegacy,
		"claude-opus-4-1":            genLegacy,
		"claude-opus-4-5-20251101":   genLegacy,
		"claude-haiku-4-5":           genLegacy,
		"claude-opus-4-6":            gen46,
		"claude-sonnet-4-6":          gen46,
		"claude-opus-4-7":            gen47,
		"claude-opus-4-8":            gen47,
		"claude-opus-5":              gen47,
		"claude-sonnet-5":            gen47,
		"claude-fable-5":             genAlwaysOn,
		"claude-fable-5-1":           genAlwaysOn,
		"claude-mythos-5-1":          genAlwaysOn,
	}
	for model, want := range cases {
		if got := classifyModel(model); got != want {
			t.Errorf("classifyModel(%q) = %v, want %v", model, got, want)
		}
	}
}

func TestBuildRequest_SamplingParamsByGeneration(t *testing.T) {
	temp := 0.3
	for model, wantSent := range map[string]bool{
		"claude-opus-4-1":   true,
		"claude-opus-4-6":   true,
		"claude-sonnet-4-6": true,
		"claude-opus-4-7":   false,
		"claude-opus-4-8":   false,
		"claude-opus-5":     false,
		"claude-sonnet-5":   false,
		"claude-fable-5-1":  false,
	} {
		c := newTestClientFor(t, model)
		req, err := c.buildRequest(userMsg("x"), llms.ApplyOptions(llms.WithTemperature(temp), llms.WithTopP(0.9)), false)
		if err != nil {
			t.Fatal(err)
		}
		if sent := req.Temperature != nil; sent != wantSent {
			t.Errorf("%s: temperature sent=%v, want %v", model, sent, wantSent)
		}
		if sent := req.TopP != nil; sent != wantSent {
			t.Errorf("%s: top_p sent=%v, want %v", model, sent, wantSent)
		}
	}
}

func TestBuildRequest_ThinkingShapeByGeneration(t *testing.T) {
	opts := llms.ApplyOptions(llms.WithReasoningEffort(llms.ReasoningEffortHigh), llms.WithReasoningBudget(8192))

	t.Run("legacy uses budget", func(t *testing.T) {
		req, _ := newTestClientFor(t, "claude-opus-4-5").buildRequest(userMsg("x"), opts, false)
		if req.Thinking == nil || req.Thinking.Type != "enabled" || req.Thinking.BudgetTokens != 8192 {
			t.Errorf("thinking = %+v", req.Thinking)
		}
		if req.OutputConfig != nil {
			t.Errorf("legacy must not send output_config, got %+v", req.OutputConfig)
		}
	})

	for _, model := range []string{"claude-opus-4-6", "claude-opus-4-8", "claude-sonnet-5"} {
		t.Run(model+" adaptive", func(t *testing.T) {
			req, _ := newTestClientFor(t, model).buildRequest(userMsg("x"), opts, false)
			if req.Thinking == nil || req.Thinking.Type != "adaptive" || req.Thinking.BudgetTokens != 0 {
				t.Errorf("thinking = %+v", req.Thinking)
			}
			if req.OutputConfig == nil || req.OutputConfig.Effort != "high" {
				t.Errorf("output_config = %+v", req.OutputConfig)
			}
			b, _ := json.Marshal(req)
			if strings.Contains(string(b), "budget_tokens") {
				t.Errorf("budget_tokens must not be on the wire: %s", b)
			}
		})
	}

	t.Run("fable omits thinking", func(t *testing.T) {
		req, _ := newTestClientFor(t, "claude-fable-5-1").buildRequest(userMsg("x"), opts, false)
		if req.Thinking != nil {
			t.Errorf("fable must omit thinking, got %+v", req.Thinking)
		}
		if req.OutputConfig == nil || req.OutputConfig.Effort != "high" {
			t.Errorf("output_config = %+v", req.OutputConfig)
		}
	})

	t.Run("explicit disable", func(t *testing.T) {
		off := llms.ApplyOptions(llms.WithReasoning(llms.ReasoningConfig{Enabled: boolPtr(false)}))
		req, _ := newTestClientFor(t, "claude-opus-4-8").buildRequest(userMsg("x"), off, false)
		if req.Thinking == nil || req.Thinking.Type != "disabled" {
			t.Errorf("4.8 disable = %+v", req.Thinking)
		}
		req, _ = newTestClientFor(t, "claude-fable-5-1").buildRequest(userMsg("x"), off, false)
		if req.Thinking != nil {
			t.Errorf("fable must never send thinking, got %+v", req.Thinking)
		}
		req, _ = newTestClientFor(t, "claude-opus-4-1").buildRequest(userMsg("x"), off, false)
		if req.Thinking != nil {
			t.Errorf("legacy disable is the default; got %+v", req.Thinking)
		}
	})

	t.Run("minimal maps to low", func(t *testing.T) {
		req, _ := newTestClientFor(t, "claude-opus-5").buildRequest(userMsg("x"),
			llms.ApplyOptions(llms.WithReasoningEffort(llms.ReasoningEffortMinimal)), false)
		if req.OutputConfig == nil || req.OutputConfig.Effort != "low" {
			t.Errorf("output_config = %+v", req.OutputConfig)
		}
	})
}

func TestBuildRequest_ForcedToolChoiceSoftenedOnFable51(t *testing.T) {
	tool := llms.Tool{Type: llms.ToolTypeFunction, Function: &llms.FunctionDefinition{Name: "f"}}
	c := newTestClientFor(t, "claude-fable-5-1")
	req, err := c.buildRequest(userMsg("x"), llms.ApplyOptions(llms.WithTools([]llms.Tool{tool}), llms.WithToolChoiceRequired()), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := req.ToolChoice.(anthropicapi.ToolChoiceAuto); !ok {
		t.Errorf("tool_choice = %#v, want auto", req.ToolChoice)
	}

	// Structured output on Fable 5.1 also falls back to auto plus an instruction.
	req, err = c.buildRequest(userMsg("x"), llms.ApplyOptions(llms.WithJSONMode()), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := req.ToolChoice.(anthropicapi.ToolChoiceAuto); !ok {
		t.Errorf("structured tool_choice = %#v, want auto", req.ToolChoice)
	}
	if len(req.System) == 0 || !strings.Contains(req.System[len(req.System)-1].Text, structuredOutputToolName) {
		t.Errorf("expected instruction naming the tool, got %+v", req.System)
	}

	// Fable 5 (not 5.1) still accepts a forced choice.
	req, _ = newTestClientFor(t, "claude-fable-5").buildRequest(userMsg("x"), llms.ApplyOptions(llms.WithJSONMode()), false)
	if _, ok := req.ToolChoice.(anthropicapi.ToolChoiceTool); !ok {
		t.Errorf("fable-5 tool_choice = %#v, want tool", req.ToolChoice)
	}
}

func boolPtr(b bool) *bool { return &b }
