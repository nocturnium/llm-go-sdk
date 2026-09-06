package gemini

import (
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/geminiapi"
)

// TestThoughtSignature_ToolCallRoundTrip verifies the round-trip-critical Gemini
// thoughtSignature is captured off the functionCall part on the way in and
// echoed back onto the reconstructed functionCall part on the way out. Dropping
// it degrades / breaks Gemini 2.5+ thinking on turn 2+.
func TestThoughtSignature_ToolCallRoundTrip(t *testing.T) {
	const sig = "Cq4BAcu98i_signed_thought_blob=="

	// Inbound: a completed response whose functionCall part carries a signature.
	resp := &geminiapi.GenerateContentResponse{
		Candidates: []geminiapi.Candidate{{
			FinishReason: "STOP",
			Content: &geminiapi.Content{Parts: []geminiapi.Part{{
				FunctionCall:     &geminiapi.FunctionCall{Name: "get_weather", Args: map[string]any{"q": "NYC"}},
				ThoughtSignature: sig,
			}}},
		}},
	}
	got := convertResponse(resp)
	if len(got.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(got.ToolCalls))
	}
	if got.ToolCalls[0].Signature != sig {
		t.Fatalf("inbound: ToolCall.Signature = %q, want %q", got.ToolCalls[0].Signature, sig)
	}

	// Outbound: feed the captured tool call back as an assistant turn (exactly
	// as RunTools does, copying resp.ToolCalls) and assert the signature rides
	// back out on the functionCall part.
	msgs := []llms.Message{{Role: llms.RoleAssistant, ToolCalls: got.ToolCalls}}
	contents, _ := convertMessages(msgs)
	if len(contents) != 1 || len(contents[0].Parts) != 1 {
		t.Fatalf("expected 1 content with 1 part, got %+v", contents)
	}
	part := contents[0].Parts[0]
	if part.FunctionCall == nil {
		t.Fatalf("expected a functionCall part, got %+v", part)
	}
	if part.ThoughtSignature != sig {
		t.Fatalf("outbound: functionCall ThoughtSignature = %q, want %q", part.ThoughtSignature, sig)
	}
}

// TestThoughtSignature_ReasoningCapture verifies the thought-part signature is
// preserved on ReasoningContent for callers that inspect it.
func TestThoughtSignature_ReasoningCapture(t *testing.T) {
	const sig = "thought_part_sig_abc123"
	resp := &geminiapi.GenerateContentResponse{
		Candidates: []geminiapi.Candidate{{
			FinishReason: "STOP",
			Content: &geminiapi.Content{Parts: []geminiapi.Part{
				{Text: "let me think", Thought: true, ThoughtSignature: sig},
				{Text: "the answer is 42"},
			}},
		}},
	}
	got := convertResponse(resp)
	if got.Reasoning == nil {
		t.Fatal("expected reasoning content")
	}
	if got.Reasoning.Signature != sig {
		t.Fatalf("Reasoning.Signature = %q, want %q", got.Reasoning.Signature, sig)
	}
	if got.Content != "the answer is 42" {
		t.Fatalf("Content = %q, want the non-thought text", got.Content)
	}
}

// TestThoughtSignature_AbsentIsEmpty guards against emitting a spurious
// signature when the provider does not issue one (the common non-thinking case).
func TestThoughtSignature_AbsentIsEmpty(t *testing.T) {
	resp := &geminiapi.GenerateContentResponse{
		Candidates: []geminiapi.Candidate{{
			FinishReason: "STOP",
			Content: &geminiapi.Content{Parts: []geminiapi.Part{{
				FunctionCall: &geminiapi.FunctionCall{Name: "ping"},
			}}},
		}},
	}
	got := convertResponse(resp)
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].Signature != "" {
		t.Fatalf("expected empty signature, got %+v", got.ToolCalls)
	}
	// And the re-emitted part must not invent one.
	contents, _ := convertMessages([]llms.Message{{Role: llms.RoleAssistant, ToolCalls: got.ToolCalls}})
	if contents[0].Parts[0].ThoughtSignature != "" {
		t.Fatalf("outbound invented a signature: %q", contents[0].Parts[0].ThoughtSignature)
	}
}
