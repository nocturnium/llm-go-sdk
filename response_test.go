package llms

import "testing"

func TestThinkingContent_Basic(t *testing.T) {
	tc := &ThinkingContent{
		Content: "Let me think about this...",
		Tokens:  50,
		Metadata: map[string]any{
			"mode": "enabled",
		},
	}

	if tc.Content != "Let me think about this..." {
		t.Errorf("expected content, got %s", tc.Content)
	}
	if tc.Tokens != 50 {
		t.Errorf("expected 50 tokens, got %d", tc.Tokens)
	}
	if tc.Metadata["mode"] != "enabled" {
		t.Errorf("expected mode=enabled, got %v", tc.Metadata["mode"])
	}
}

func TestResponse_WithThinking(t *testing.T) {
	resp := &Response{
		Content: "The answer is 42.",
		Thinking: &ThinkingContent{
			Content: "I need to calculate...",
			Tokens:  25,
		},
		FinishReason: "stop",
	}

	if resp.Thinking == nil {
		t.Fatal("expected thinking content")
	}
	if resp.Thinking.Content != "I need to calculate..." {
		t.Errorf("unexpected thinking content: %s", resp.Thinking.Content)
	}
}

func TestResponse_NilThinking(t *testing.T) {
	resp := &Response{
		Content:      "Simple answer",
		FinishReason: "stop",
	}

	if resp.Thinking != nil {
		t.Error("expected nil thinking for providers without reasoning")
	}
}

func TestStreamChunk_WithThinking(t *testing.T) {
	chunk := StreamChunk{
		Content: "partial answer",
		Thinking: &ThinkingContent{
			Content: "reasoning step 1",
		},
	}

	if chunk.Thinking == nil {
		t.Fatal("expected thinking in chunk")
	}
	if chunk.Thinking.Content != "reasoning step 1" {
		t.Errorf("unexpected thinking: %s", chunk.Thinking.Content)
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
