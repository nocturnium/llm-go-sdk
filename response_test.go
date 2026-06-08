package llms

import "testing"

func TestReasoningContent_Basic(t *testing.T) {
	rc := &ReasoningContent{
		Content: "Let me think about this...",
		Tokens:  50,
		Metadata: map[string]any{
			"mode": "enabled",
		},
	}

	if rc.Content != "Let me think about this..." {
		t.Errorf("expected content, got %s", rc.Content)
	}
	if rc.Tokens != 50 {
		t.Errorf("expected 50 tokens, got %d", rc.Tokens)
	}
	if rc.Metadata["mode"] != "enabled" {
		t.Errorf("expected mode=enabled, got %v", rc.Metadata["mode"])
	}
}

func TestResponse_WithReasoning(t *testing.T) {
	resp := &Response{
		Content: "The answer is 42.",
		Reasoning: &ReasoningContent{
			Content: "I need to calculate...",
			Tokens:  25,
		},
		FinishReason: "stop",
	}

	if resp.Reasoning == nil {
		t.Fatal("expected reasoning content")
	}
	if resp.Reasoning.Content != "I need to calculate..." {
		t.Errorf("unexpected reasoning content: %s", resp.Reasoning.Content)
	}
}

func TestResponse_NilReasoning(t *testing.T) {
	resp := &Response{
		Content:      "Simple answer",
		FinishReason: "stop",
	}

	if resp.Reasoning != nil {
		t.Error("expected nil reasoning for providers without reasoning")
	}
}

func TestStreamChunk_WithReasoning(t *testing.T) {
	chunk := StreamChunk{
		Content: "partial answer",
		Reasoning: &ReasoningContent{
			Content: "reasoning step 1",
		},
	}

	if chunk.Reasoning == nil {
		t.Fatal("expected reasoning in chunk")
	}
	if chunk.Reasoning.Content != "reasoning step 1" {
		t.Errorf("unexpected reasoning: %s", chunk.Reasoning.Content)
	}
}

func TestResponse_WithSearchResults(t *testing.T) {
	resp := &Response{
		Content: "Based on my search...",
		SearchResults: []SearchResult{
			{Title: "Result 1", URL: "https://example.com/1"},
			{Title: "Result 2", URL: "https://example.com/2"},
		},
	}

	if len(resp.SearchResults) != 2 {
		t.Errorf("expected 2 search results, got %d", len(resp.SearchResults))
	}
}
