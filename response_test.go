package llms

import (
	"encoding/json"
	"reflect"
	"testing"
)

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

func TestResponse_UnmarshalJSON_PreservesReasoning(t *testing.T) {
	data := []byte(`{
		"content": "The answer is 42.",
		"reasoning": {
			"content": "I need to calculate.",
			"tokens": 25,
			"signature": "sig-123",
			"metadata": {"mode": "extended"}
		},
		"finish_reason": "stop",
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 20,
			"total_tokens": 30,
			"reasoning_tokens": 5
		},
		"tool_calls": [{
			"id": "call-1",
			"type": "function",
			"function": {"name": "lookup", "arguments": "{\"q\":\"answer\"}"}
		}]
	}`)

	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.Reasoning == nil {
		t.Fatal("expected reasoning content")
	}
	if resp.Thinking() != resp.Reasoning {
		t.Fatal("expected Thinking method to return reasoning")
	}
	if resp.Reasoning.Content != "I need to calculate." {
		t.Errorf("unexpected reasoning content: %s", resp.Reasoning.Content)
	}
	if resp.Reasoning.Tokens != 25 {
		t.Errorf("unexpected reasoning tokens: %d", resp.Reasoning.Tokens)
	}
	if resp.Reasoning.Signature != "sig-123" {
		t.Errorf("unexpected reasoning signature: %s", resp.Reasoning.Signature)
	}
	if resp.Reasoning.Metadata["mode"] != "extended" {
		t.Errorf("unexpected reasoning metadata: %v", resp.Reasoning.Metadata)
	}
}

func TestResponse_JSONRoundTrip_PreservesReasoningAndOmitsThinking(t *testing.T) {
	resp := Response{
		Content: "The answer is 42.",
		Reasoning: &ReasoningContent{
			Content:   "I need to calculate.",
			Tokens:    25,
			Signature: "sig-123",
			Metadata: map[string]any{
				"mode": "extended",
			},
		},
		FinishReason: FinishReasonStop,
		Usage: Usage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
			ReasoningTokens:  5,
		},
		ToolCalls: []ToolCall{
			{
				ID:   "call-1",
				Type: ToolTypeFunction,
				Function: &FunctionCall{
					Name:      "lookup",
					Arguments: `{"q":"answer"}`,
				},
			},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		t.Fatalf("unmarshal response keys: %v", err)
	}
	if _, ok := keys["reasoning"]; !ok {
		t.Fatal("expected reasoning key")
	}
	if _, ok := keys["thinking"]; ok {
		t.Fatal("did not expect thinking key")
	}

	var out Response
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if out.Content != resp.Content {
		t.Errorf("content mismatch: got %q want %q", out.Content, resp.Content)
	}
	if !reflect.DeepEqual(out.Reasoning, resp.Reasoning) {
		t.Errorf("reasoning mismatch: got %#v want %#v", out.Reasoning, resp.Reasoning)
	}
	if out.Thinking() != out.Reasoning {
		t.Fatal("expected Thinking method to return reasoning")
	}
	if out.FinishReason != resp.FinishReason {
		t.Errorf("finish reason mismatch: got %q want %q", out.FinishReason, resp.FinishReason)
	}
	if out.Usage != resp.Usage {
		t.Errorf("usage mismatch: got %#v want %#v", out.Usage, resp.Usage)
	}
	if !reflect.DeepEqual(out.ToolCalls, resp.ToolCalls) {
		t.Errorf("tool calls mismatch: got %#v want %#v", out.ToolCalls, resp.ToolCalls)
	}
}

func TestResponse_UnmarshalJSON_NoReasoningLeavesThinkingNil(t *testing.T) {
	var resp Response
	if err := json.Unmarshal([]byte(`{"content":"Simple answer","finish_reason":"stop"}`), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.Reasoning != nil {
		t.Fatal("expected nil reasoning")
	}
	if resp.Thinking() != nil {
		t.Fatal("expected nil thinking")
	}
}

func TestResponse_ThinkingMethodReturnsReasoningFromStructLiteral(t *testing.T) {
	reasoning := &ReasoningContent{Content: "direct literal"}
	resp := &Response{Reasoning: reasoning}

	if resp.Thinking() != reasoning {
		t.Fatal("expected Thinking method to return struct literal reasoning")
	}
}

func TestResponse_ThinkingMethodNilSafe(t *testing.T) {
	var resp *Response
	if resp.Thinking() != nil {
		t.Fatal("expected nil thinking for nil response")
	}
}

func TestStreamChunk_UnmarshalJSON_PreservesReasoning(t *testing.T) {
	data := []byte(`{
		"content": "partial answer",
		"reasoning": {
			"content": "reasoning step 1",
			"tokens": 7,
			"signature": "sig-chunk",
			"metadata": {"mode": "stream"}
		},
		"tool_calls": [{
			"id": "call-1",
			"type": "function",
			"function": {"name": "lookup", "arguments": "{\"q\":\"answer\"}"}
		}],
		"finish_reason": "tool_calls",
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 20,
			"total_tokens": 30,
			"reasoning_tokens": 7
		},
		"done": true
	}`)

	var chunk StreamChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		t.Fatalf("unmarshal stream chunk: %v", err)
	}

	if chunk.Reasoning == nil {
		t.Fatal("expected reasoning content")
	}
	if chunk.Thinking() != chunk.Reasoning {
		t.Fatal("expected Thinking method to return reasoning")
	}
	if chunk.Reasoning.Content != "reasoning step 1" {
		t.Errorf("unexpected reasoning content: %s", chunk.Reasoning.Content)
	}
}

func TestStreamChunk_JSONRoundTrip_PreservesReasoningAndOmitsThinking(t *testing.T) {
	chunk := StreamChunk{
		Content: "partial answer",
		Reasoning: &ReasoningContent{
			Content:   "reasoning step 1",
			Tokens:    7,
			Signature: "sig-chunk",
			Metadata: map[string]any{
				"mode": "stream",
			},
		},
		ToolCalls: []ToolCall{
			{
				ID:   "call-1",
				Type: ToolTypeFunction,
				Function: &FunctionCall{
					Name:      "lookup",
					Arguments: `{"q":"answer"}`,
				},
			},
		},
		FinishReason: FinishReasonToolCalls,
		Usage: &Usage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
			ReasoningTokens:  7,
		},
		Done: true,
	}
	data, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("marshal stream chunk: %v", err)
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		t.Fatalf("unmarshal stream chunk keys: %v", err)
	}
	if _, ok := keys["reasoning"]; !ok {
		t.Fatal("expected reasoning key")
	}
	if _, ok := keys["thinking"]; ok {
		t.Fatal("did not expect thinking key")
	}

	var out StreamChunk
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal stream chunk: %v", err)
	}

	if out.Content != chunk.Content {
		t.Errorf("content mismatch: got %q want %q", out.Content, chunk.Content)
	}
	if !reflect.DeepEqual(out.Reasoning, chunk.Reasoning) {
		t.Errorf("reasoning mismatch: got %#v want %#v", out.Reasoning, chunk.Reasoning)
	}
	if out.Thinking() != out.Reasoning {
		t.Fatal("expected Thinking method to return reasoning")
	}
	if !reflect.DeepEqual(out.ToolCalls, chunk.ToolCalls) {
		t.Errorf("tool calls mismatch: got %#v want %#v", out.ToolCalls, chunk.ToolCalls)
	}
	if out.FinishReason != chunk.FinishReason {
		t.Errorf("finish reason mismatch: got %q want %q", out.FinishReason, chunk.FinishReason)
	}
	if !reflect.DeepEqual(out.Usage, chunk.Usage) {
		t.Errorf("usage mismatch: got %#v want %#v", out.Usage, chunk.Usage)
	}
	if out.Done != chunk.Done {
		t.Errorf("done mismatch: got %t want %t", out.Done, chunk.Done)
	}
}

func TestStreamChunk_UnmarshalJSON_NoReasoningLeavesThinkingNil(t *testing.T) {
	var chunk StreamChunk
	if err := json.Unmarshal([]byte(`{"content":"partial answer","done":true}`), &chunk); err != nil {
		t.Fatalf("unmarshal stream chunk: %v", err)
	}

	if chunk.Reasoning != nil {
		t.Fatal("expected nil reasoning")
	}
	if chunk.Thinking() != nil {
		t.Fatal("expected nil thinking")
	}
}

func TestStreamChunk_ThinkingMethodReturnsReasoningFromStructLiteral(t *testing.T) {
	reasoning := &ReasoningContent{Content: "direct chunk literal"}
	chunk := &StreamChunk{Reasoning: reasoning}

	if chunk.Thinking() != reasoning {
		t.Fatal("expected Thinking method to return struct literal reasoning")
	}
}

func TestStreamChunk_ThinkingMethodNilSafe(t *testing.T) {
	var chunk *StreamChunk
	if chunk.Thinking() != nil {
		t.Fatal("expected nil thinking for nil stream chunk")
	}
}
