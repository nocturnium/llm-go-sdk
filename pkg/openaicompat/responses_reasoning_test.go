package openaicompat

import (
	"encoding/json"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v3"
)

// TestReasoningInputItemMarshalsSummary guards the wire-format invariant the live
// API enforces: a "reasoning" input item must always carry a "summary" key (even
// when empty), while other item kinds must not emit a summary at all.
func TestReasoningInputItemMarshalsSummary(t *testing.T) {
	b, err := json.Marshal(ResponsesInputItem{Type: "reasoning", ID: "rs_1", EncryptedContent: "E"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"summary":[]`) {
		t.Errorf("reasoning input item must include an (empty) summary array, got %s", b)
	}

	b2, err := json.Marshal(ResponsesInputItem{
		Type:    "message",
		Role:    "user",
		Content: []ResponsesContentPart{{Type: "input_text", Text: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b2), "summary") {
		t.Errorf("non-reasoning item must not emit summary, got %s", b2)
	}
}

// TestResponsesReasoningRoundTrip verifies the encrypted-reasoning round-trip:
// items are captured off a response into ReasoningContent.Metadata, and replayed
// as "reasoning" input items (before their assistant message) on a follow-up,
// so a stateless multi-turn conversation preserves the model's thinking.
func TestResponsesReasoningRoundTrip(t *testing.T) {
	resp := &ResponsesResponse{
		ID:     "resp_1",
		Status: "completed",
		Output: []ResponsesOutputItem{
			{
				Type:             "reasoning",
				ID:               "rs_abc",
				EncryptedContent: "ENCRYPTED",
				Summary:          []ResponsesSummaryPart{{Type: "summary_text", Text: "thinking..."}},
			},
			{
				Type:    "message",
				Role:    "assistant",
				Content: []ResponsesOutputContent{{Type: "output_text", Text: "Hello"}},
			},
		},
	}

	converted := ConvertResponsesResponse(resp)
	if converted.Reasoning == nil {
		t.Fatal("expected Reasoning to be set")
	}
	items, ok := converted.Reasoning.Metadata[MetadataKeyResponsesReasoning].([]ResponsesReasoningItem)
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 stored reasoning item, got metadata %#v", converted.Reasoning.Metadata)
	}
	if items[0].ID != "rs_abc" || items[0].EncryptedContent != "ENCRYPTED" {
		t.Errorf("reasoning item not captured faithfully: %#v", items[0])
	}

	// Round-trip: an assistant message carrying that reasoning, then a new user turn.
	history := []llms.Message{
		{Role: llms.RoleUser, Content: "first"},
		{Role: llms.RoleAssistant, Content: "Hello", Reasoning: converted.Reasoning},
		{Role: llms.RoleUser, Content: "second"},
	}
	req := BuildResponsesRequest("o4-mini", history, &llms.CallOptions{
		ExtraBody: map[string]any{"include_encrypted_reasoning": true},
	}, false)

	foundInclude := false
	for _, inc := range req.Include {
		if inc == "reasoning.encrypted_content" {
			foundInclude = true
		}
	}
	if !foundInclude {
		t.Errorf("expected Include to contain reasoning.encrypted_content, got %v", req.Include)
	}

	reasoningIdx, assistantIdx := -1, -1
	for i, it := range req.Input {
		switch {
		case it.Type == "reasoning" && it.EncryptedContent == "ENCRYPTED":
			reasoningIdx = i
		case it.Type == "message" && it.Role == "assistant":
			assistantIdx = i
		}
	}
	if reasoningIdx == -1 {
		t.Fatal("expected the encrypted reasoning item to be replayed as an input item")
	}
	if assistantIdx == -1 || reasoningIdx > assistantIdx {
		t.Errorf("reasoning item (idx %d) must precede its assistant message (idx %d)", reasoningIdx, assistantIdx)
	}
}

// TestResponsesReasoning_NoEncryptedContent verifies the default path is unchanged
// when no encrypted content is present: summary text still surfaces, but nothing is
// stashed for round-tripping and no reasoning input items are emitted.
func TestResponsesReasoning_NoEncryptedContent(t *testing.T) {
	resp := &ResponsesResponse{
		ID:     "resp_2",
		Status: "completed",
		Output: []ResponsesOutputItem{
			{Type: "reasoning", Summary: []ResponsesSummaryPart{{Type: "summary_text", Text: "t"}}},
			{Type: "message", Role: "assistant", Content: []ResponsesOutputContent{{Type: "output_text", Text: "Hi"}}},
		},
	}

	converted := ConvertResponsesResponse(resp)
	if converted.Reasoning == nil || converted.Reasoning.Content != "t" {
		t.Fatalf("expected summary reasoning text to be set, got %#v", converted.Reasoning)
	}
	if _, ok := converted.Reasoning.Metadata[MetadataKeyResponsesReasoning]; ok {
		t.Error("expected no stored reasoning items when encrypted_content is absent")
	}

	req := BuildResponsesRequest("o4-mini", []llms.Message{
		{Role: llms.RoleAssistant, Content: "Hi", Reasoning: converted.Reasoning},
		{Role: llms.RoleUser, Content: "next"},
	}, &llms.CallOptions{}, false)
	for _, it := range req.Input {
		if it.Type == "reasoning" {
			t.Error("did not expect a reasoning input item without encrypted content")
		}
	}
	if len(req.Include) != 0 {
		t.Errorf("expected no Include without the opt-in flag, got %v", req.Include)
	}
}
