package llmadk

import (
	"bytes"
	"encoding/base64"
	"errors"
	"reflect"
	"testing"
	"time"

	"google.golang.org/genai"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func rtFor(hide bool, history ...string) *responseTranslator {
	tr := &translation{hideThoughts: hide, historyIDs: map[string]bool{}}
	for _, id := range history {
		tr.historyIDs[id] = true
	}
	return &responseTranslator{tr: tr, fallbackProvider: llms.ProviderAnthropic, fallbackModel: "fallback-model"}
}

func TestConvert_PartsFinishAndUsage(t *testing.T) {
	cost := 0.25
	resp := &llms.Response{
		ID:           "resp_1",
		Content:      "answer",
		Reasoning:    &llms.ReasoningContent{Content: "thinking", Signature: "sig", Provider: llms.ProviderAnthropic, Model: "m"},
		ToolCalls:    []llms.ToolCall{toolCall("toolu_0123456789", "f", `{"a":1}`)},
		FinishReason: llms.FinishReasonToolCalls,
		Usage:        llms.Usage{PromptTokens: 10, CacheReadTokens: 80, CacheCreationTokens: 5, CompletionTokens: 30, ReasoningTokens: 12, Cost: &cost},
		Provider:     llms.ProviderAnthropic, Model: "m", ModelVersion: "m-2026",
		Adjustments: []string{"anthropic.thinking_suspended"},
	}
	out, err := rtFor(false).convert(resp)
	if err != nil {
		t.Fatal(err)
	}
	parts := out.Content.Parts
	if len(parts) != 3 || !parts[0].Thought || parts[0].Text != "thinking" || parts[1].Text != "answer" || parts[2].FunctionCall.Name != "f" {
		t.Fatalf("parts = %+v", parts)
	}
	if parts[2].FunctionCall.ID != "toolu_0123456789" || parts[2].FunctionCall.Args["a"] != 1.0 {
		t.Errorf("function call = %+v", parts[2].FunctionCall)
	}
	if out.FinishReason != genai.FinishReasonStop || !out.TurnComplete || out.Partial {
		t.Errorf("finish = %s turnComplete=%v partial=%v", out.FinishReason, out.TurnComplete, out.Partial)
	}
	u := out.UsageMetadata
	// Prompt includes the cache; candidates exclude the thoughts.
	if u.PromptTokenCount != 95 || u.CandidatesTokenCount != 18 || u.ThoughtsTokenCount != 12 || u.CachedContentTokenCount != 80 || u.TotalTokenCount != 125 {
		t.Errorf("usage = %+v", u)
	}
	md := out.CustomMetadata
	if md[MetadataCostUSD] != 0.25 || md[MetadataProvider] != "anthropic" || md[MetadataResponseID] != "resp_1" || md[MetadataCacheCreationTokens] != 5 {
		t.Errorf("metadata = %+v", md)
	}
	if out.ModelVersion != "m-2026" {
		t.Errorf("model version = %q", out.ModelVersion)
	}
}

func TestConvert_FinishReasons(t *testing.T) {
	tests := []struct {
		reason  llms.FinishReason
		content string
		want    genai.FinishReason
		code    string
	}{
		{llms.FinishReasonStop, "x", genai.FinishReasonStop, ""},
		{"", "x", genai.FinishReasonStop, ""},
		{llms.FinishReasonLength, "x", genai.FinishReasonMaxTokens, ""},
		{llms.FinishReasonLength, "", genai.FinishReasonMaxTokens, "MAX_TOKENS"},
		{llms.FinishReasonContentFilter, "", genai.FinishReasonSafety, "SAFETY"},
		{llms.FinishReasonStop, "", genai.FinishReasonStop, ""},
	}
	for _, tt := range tests {
		out, err := rtFor(true).convert(&llms.Response{Content: tt.content, FinishReason: tt.reason})
		if err != nil {
			t.Fatal(err)
		}
		if out.FinishReason != tt.want || out.ErrorCode != tt.code {
			t.Errorf("%q/%q: finish %s code %q, want %s %q", tt.reason, tt.content, out.FinishReason, out.ErrorCode, tt.want, tt.code)
		}
		if tt.want == genai.FinishReasonStop && out.Content == nil {
			t.Errorf("%q: STOP without content; ADK drops such an event", tt.reason)
		}
	}
}

// TestConvert_ToolCallIDs checks that IDs are unique across the session: ADK
// pairs calls with results over the whole history.
func TestConvert_ToolCallIDs(t *testing.T) {
	resp := &llms.Response{ToolCalls: []llms.ToolCall{
		toolCall("", "a", "{}"),
		toolCall("call_0", "b", "{}"),
		toolCall("adk-123456789", "c", "{}"),
		toolCall("seen_before_1", "d", "{}"),
		toolCall("toolu_0123456789", "e", "{}"),
		toolCall("toolu_0123456789", "f", "{}"),
	}}
	out, err := rtFor(true, "seen_before_1").convert(resp)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for i, p := range out.Content.Parts {
		id := p.FunctionCall.ID
		if ids[id] || id == "" || id == "call_0" || id == "seen_before_1" || id[:4] == "adk-" {
			t.Errorf("call %d kept a colliding ID %q", i, id)
		}
		ids[id] = true
	}
	if out.Content.Parts[4].FunctionCall.ID != "toolu_0123456789" {
		t.Errorf("a unique provider ID was replaced: %q", out.Content.Parts[4].FunctionCall.ID)
	}
}

func TestConvert_ArgumentsMustBeAnObject(t *testing.T) {
	_, err := rtFor(true).convert(&llms.Response{ToolCalls: []llms.ToolCall{toolCall("toolu_0123456789", "f", "[1,2]")}})
	if !errors.Is(err, ErrFunctionCallArgs) {
		t.Errorf("err = %v", err)
	}
	out, err := rtFor(true).convert(&llms.Response{ToolCalls: []llms.ToolCall{toolCall("toolu_0123456789", "f", "")}})
	if err != nil || out.Content.Parts[0].FunctionCall.Args == nil {
		t.Errorf("empty arguments: %v %+v", err, out)
	}
}

// TestConvert_HiddenThoughts checks the thought-only rule and that hidden text
// still round-trips through the envelope.
func TestConvert_HiddenThoughts(t *testing.T) {
	rc := &llms.ReasoningContent{Content: "secret", Signature: "sig", Provider: llms.ProviderAnthropic}
	out, err := rtFor(true).convert(&llms.Response{Reasoning: rc, Content: "answer"})
	if err != nil {
		t.Fatal(err)
	}
	thought := out.Content.Parts[0]
	if !thought.Thought || thought.Text != "" {
		t.Fatalf("hidden thought shows text: %+v", thought)
	}
	if thought.ThoughtSignature != nil {
		t.Error("another provider's envelope is in ThoughtSignature, which Gemini validates")
	}
	env, res := partSignature(thought)
	if res != sigEnvelope || env.Content == nil || *env.Content != "secret" || env.Signature != "sig" {
		t.Errorf("envelope = %+v (%v)", env, res)
	}

	out, err = rtFor(true).convert(&llms.Response{Reasoning: rc, FinishReason: llms.FinishReasonLength})
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != nil || out.ErrorCode != "MAX_TOKENS" {
		t.Errorf("thought-only turn = %+v; want no parts, so ADK does not re-call the model", out)
	}
}

func TestConvert_StampsUnstampedReasoning(t *testing.T) {
	rc := &llms.ReasoningContent{Content: "r", Signature: "s"}
	if _, err := rtFor(false).convert(&llms.Response{Reasoning: rc, Content: "x"}); err != nil {
		t.Fatal(err)
	}
	if rc.Provider != llms.ProviderAnthropic || rc.Model != "fallback-model" {
		t.Errorf("stamp = %q/%q", rc.Provider, rc.Model)
	}
	router := &responseTranslator{tr: &translation{historyIDs: map[string]bool{}}}
	rc2 := &llms.ReasoningContent{Content: "r"}
	if _, err := router.convert(&llms.Response{Reasoning: rc2, Content: "x"}); err != nil {
		t.Fatal(err)
	}
	if rc2.Provider != "" {
		t.Errorf("reasoning behind a router was stamped %q", rc2.Provider)
	}
}

func TestResolveStructured(t *testing.T) {
	only := &llms.Response{Content: "prose", ToolCalls: []llms.ToolCall{toolCall("x", setModelResponseName, `{"answer":"42"}`)}, FinishReason: llms.FinishReasonToolCalls}
	resolveStructured(only)
	if only.Content != `{"answer":"42"}` || len(only.ToolCalls) != 0 || only.FinishReason != llms.FinishReasonStop {
		t.Errorf("only set_model_response: %+v", only)
	}
	mixed := &llms.Response{ToolCalls: []llms.ToolCall{toolCall("x", setModelResponseName, `{}`), toolCall("y", "lookup", `{}`)}}
	resolveStructured(mixed)
	if len(mixed.ToolCalls) != 1 || mixed.ToolCalls[0].Function.Name != "lookup" || mixed.Content != "" {
		t.Errorf("mixed calls: %+v", mixed)
	}
}

func TestEnvelope_RoundTrip(t *testing.T) {
	rc := &llms.ReasoningContent{
		Content: "text", Signature: "sig", Tokens: 7, Provider: llms.ProviderOpenAI, Model: "gpt",
		Metadata: map[string]any{"openai_responses_reasoning_items": []any{map[string]any{"id": "rs_1", "encrypted_content": "E"}}},
	}
	env, res := decodeEnvelope(thoughtEnvelope(rc, true))
	if res != sigEnvelope {
		t.Fatalf("decode = %v", res)
	}
	if got := env.reasoning("ignored"); !reflect.DeepEqual(got, rc) {
		t.Errorf("round trip:\n got %+v\nwant %+v", got, rc)
	}
	env, _ = decodeEnvelope(thoughtEnvelope(rc, false))
	if got := env.reasoning("visible"); got.Content != "visible" {
		t.Errorf("text without c = %q", got.Content)
	}
}

func TestDecodeSignature(t *testing.T) {
	if _, res := nativeSignature(nil); res != sigNone {
		t.Errorf("nil = %v", res)
	}
	native := []byte{0xde, 0xad}
	env, res := nativeSignature(native)
	if res != sigNative || env.Provider != llms.ProviderGemini || env.Signature != base64.StdEncoding.EncodeToString(native) {
		t.Errorf("native = %+v %v", env, res)
	}
	for name, b := range map[string][]byte{
		"corrupt JSON":    append(append([]byte("llmgo"), envelopeVersion), '{'),
		"unknown version": append([]byte("llmgo"), 9, '{', '}'),
		"oversized":       append(append([]byte("llmgo"), envelopeVersion), bytes.Repeat([]byte(" "), maxEnvelopeSize)...),
	} {
		if _, res := decodeEnvelope(b); res != sigInvalid {
			t.Errorf("%s = %v, want sigInvalid", name, res)
		}
	}
}

func FuzzEnvelopeDecode(f *testing.F) {
	f.Add(thoughtEnvelope(&llms.ReasoningContent{Content: "c", Signature: "s"}, true))
	f.Add([]byte("llmgo\x01{}"))
	f.Add([]byte{1, 2, 3})
	f.Fuzz(func(t *testing.T, b []byte) {
		env, res := decodeEnvelope(b)
		if res == sigEnvelope {
			_ = env.reasoning("")
		}
	})
}

// TestConvert_GeminiSignaturesStayNative checks that Gemini's signatures are
// stored as the raw bytes Gemini issued, which ADK's own Gemini model can send
// back, and that the bridge reads them back as Gemini's.
func TestConvert_GeminiSignaturesStayNative(t *testing.T) {
	raw := []byte{0x0a, 0x01, 0x02, 0xff}
	sig := base64.StdEncoding.EncodeToString(raw)
	resp := &llms.Response{
		Reasoning: &llms.ReasoningContent{Content: "t", Signature: sig, Provider: llms.ProviderGemini},
		ToolCalls: []llms.ToolCall{{ID: "call_123456", Type: llms.ToolTypeFunction, Function: &llms.FunctionCall{Name: "f", Arguments: "{}"},
			Signature: sig, SignatureProvider: llms.ProviderGemini}},
	}
	out, err := rtFor(true).convert(resp)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range out.Content.Parts {
		if !bytes.Equal(p.ThoughtSignature, raw) {
			t.Errorf("part %d signature = %x, want Gemini's raw bytes %x", i, p.ThoughtSignature, raw)
		}
		env, res := partSignature(p)
		if res != sigNative || env.Signature != sig || env.Provider != llms.ProviderGemini {
			t.Errorf("part %d decodes as %+v (%v)", i, env, res)
		}
	}
}

// TestConvert_GeminiReasoningWithoutSignature covers Gemini 3, which signs the
// function call and not the thought: hidden thought text is not kept, since
// Gemini never needs it back and an envelope would reach ADK's Gemini model.
func TestConvert_GeminiReasoningWithoutSignature(t *testing.T) {
	resp := &llms.Response{Content: "x", Reasoning: &llms.ReasoningContent{Content: "thinking", Provider: llms.ProviderGemini}}
	out, err := rtFor(true).convert(resp)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range out.Content.Parts {
		if p.Thought {
			t.Errorf("hidden unsigned Gemini thought kept: %+v", p)
		}
	}
	out, err = rtFor(false).convert(&llms.Response{Content: "x", Reasoning: &llms.ReasoningContent{Content: "thinking", Provider: llms.ProviderGemini}})
	if err != nil {
		t.Fatal(err)
	}
	if p := out.Content.Parts[0]; !p.Thought || p.Text != "thinking" || p.ThoughtSignature != nil {
		t.Errorf("shown Gemini thought = %+v", p)
	}
}

// TestPartSignature_Corrupt rejects malformed PartMetadata envelopes and
// reads whatever ThoughtSignature holds as Gemini's.
func TestPartSignature_Corrupt(t *testing.T) {
	for _, v := range []any{42, "not base64!", base64.StdEncoding.EncodeToString([]byte("plain"))} {
		if _, res := partSignature(&genai.Part{PartMetadata: map[string]any{partMetadataKey: v}}); res != sigInvalid {
			t.Errorf("PartMetadata %v = %v, want sigInvalid", v, res)
		}
	}
	if got, res := partSignature(&genai.Part{ThoughtSignature: []byte("llmgo-looking bytes")}); res != sigNative || got.Provider != llms.ProviderGemini {
		t.Errorf("ThoughtSignature = %+v (%v), want Gemini's", got, res)
	}
}

// TestConvert_PlaceholderOnForeignFirstCall checks that the first call of a
// response without Gemini's own signature carries Google's placeholder, so ADK's
// Gemini model can take over the session, and that only the first does.
func TestConvert_PlaceholderOnForeignFirstCall(t *testing.T) {
	out, err := rtFor(true).convert(&llms.Response{ToolCalls: []llms.ToolCall{
		toolCall("toolu_0123456789", "a", "{}"), toolCall("toolu_9876543210", "b", "{}"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out.Content.Parts[0].ThoughtSignature); got != geminiSkipValidator {
		t.Errorf("first call signature = %q", got)
	}
	if out.Content.Parts[1].ThoughtSignature != nil {
		t.Errorf("second call got a signature: %q", out.Content.Parts[1].ThoughtSignature)
	}
}

func TestConvert_SearchResultsAsGrounding(t *testing.T) {
	out, err := rtFor(true).convert(&llms.Response{Content: "x", SearchResults: []llms.SearchResult{{Title: "Go 1.27", URL: "https://go.dev/doc/go1.27"}}})
	if err != nil {
		t.Fatal(err)
	}
	gm := out.GroundingMetadata
	if gm == nil || len(gm.GroundingChunks) != 1 || gm.GroundingChunks[0].Web.URI != "https://go.dev/doc/go1.27" || gm.GroundingChunks[0].Web.Title != "Go 1.27" {
		t.Errorf("grounding = %+v", gm)
	}
}

func TestOptions(t *testing.T) {
	m, err := NewModel(newFake(), WithName("custom"), WithTimeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name() != "custom" || m.LLM() == nil {
		t.Errorf("name = %q", m.Name())
	}
	if got := m.timeout(&translation{}); got != time.Second {
		t.Errorf("timeout = %v", got)
	}
	if got := m.timeout(&translation{timeout: 2 * time.Second}); got != 2*time.Second {
		t.Errorf("request timeout = %v, want it to win", got)
	}
	if _, err := NewModel(nil); err == nil {
		t.Error("NewModel(nil) succeeded")
	}
}
