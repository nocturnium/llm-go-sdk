package openaicompat

import (
	"encoding/json"
	"reflect"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk"
)

func mustMarshalRawMessage(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestConvertMessages(t *testing.T) {
	messages := []llms.Message{
		{Role: llms.RoleSystem, Content: "You are helpful"},
		{Role: llms.RoleUser, Content: "Hello"},
		{Role: llms.RoleAssistant, Content: "Hi there!"},
	}

	result := ConvertMessages(messages)

	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}

	if result[0].Role != "system" {
		t.Errorf("expected role 'system', got %s", result[0].Role)
	}
	if result[0].ContentValue != "You are helpful" {
		t.Errorf("expected content 'You are helpful', got %v", result[0].ContentValue)
	}
}

func TestConvertMessagesWithToolCalls(t *testing.T) {
	messages := []llms.Message{
		{
			Role:    llms.RoleAssistant,
			Content: "",
			ToolCalls: []llms.ToolCall{
				{
					ID:   "call_123",
					Type: "function",
					Function: &llms.FunctionCall{
						Name:      testGetWeather,
						Arguments: `{"location":"NYC"}`,
					},
				},
			},
		},
	}

	result := ConvertMessages(messages)

	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if len(result[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result[0].ToolCalls))
	}
	if result[0].ToolCalls[0].ID != "call_123" {
		t.Errorf("expected tool call ID 'call_123', got %s", result[0].ToolCalls[0].ID)
	}
	if result[0].ToolCalls[0].Function.Name != testGetWeather {
		t.Errorf("expected function name %q, got %s", testGetWeather, result[0].ToolCalls[0].Function.Name)
	}
}

func TestConvertContentParts(t *testing.T) {
	parts := []llms.ContentPart{
		{Type: "text", Text: "Hello"},
		{
			Type: "image",
			Image: &llms.ImageContent{
				Source:    "url",
				Data:      "https://example.com/image.png",
				MediaType: "image/png",
			},
		},
	}

	result := convertContentParts(parts)

	if len(result) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(result))
	}
	if result[0].Type != "text" {
		t.Errorf("expected type 'text', got %s", result[0].Type)
	}
	if result[0].Text != "Hello" {
		t.Errorf("expected text 'Hello', got %s", result[0].Text)
	}
	if result[1].Type != "image_url" {
		t.Errorf("expected type 'image_url', got %s", result[1].Type)
	}
	if result[1].ImageURL.URL != "https://example.com/image.png" {
		t.Errorf("expected URL 'https://example.com/image.png', got %s", result[1].ImageURL.URL)
	}
}

func TestConvertContentPartsWithBase64(t *testing.T) {
	parts := []llms.ContentPart{
		{
			Type: "image",
			Image: &llms.ImageContent{
				Source:    "base64",
				Data:      "dGVzdA==",
				MediaType: "image/png",
			},
		},
	}

	result := convertContentParts(parts)

	if len(result) != 1 {
		t.Fatalf("expected 1 part, got %d", len(result))
	}
	expectedURL := "data:image/png;base64,dGVzdA=="
	if result[0].ImageURL.URL != expectedURL {
		t.Errorf("expected URL '%s', got %s", expectedURL, result[0].ImageURL.URL)
	}
}

func TestConvertContentPartsWithNilImage(t *testing.T) {
	parts := []llms.ContentPart{
		{Type: "image", Image: nil},
	}

	result := convertContentParts(parts)

	if len(result) != 1 {
		t.Fatalf("expected 1 part, got %d", len(result))
	}
	// When Image is nil, the result should have empty ImageURL
	if result[0].ImageURL != nil {
		t.Error("expected ImageURL to be nil for nil image")
	}
}

func TestFormatImageURL(t *testing.T) {
	tests := []struct {
		name     string
		img      *llms.ImageContent
		expected string
	}{
		{
			name: "URL source",
			img: &llms.ImageContent{
				Source: "url",
				Data:   "https://example.com/image.png",
			},
			expected: "https://example.com/image.png",
		},
		{
			name: "Base64 source",
			img: &llms.ImageContent{
				Source:    "base64",
				Data:      "dGVzdA==",
				MediaType: "image/png",
			},
			expected: "data:image/png;base64,dGVzdA==",
		},
		{
			name: "Base64 jpeg",
			img: &llms.ImageContent{
				Source:    "base64",
				Data:      "abc123",
				MediaType: "image/jpeg",
			},
			expected: "data:image/jpeg;base64,abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatImageURL(tt.img)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestConvertTools(t *testing.T) {
	tools := []llms.Tool{
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        testGetWeather,
				Description: "Get weather information",
				Parameters: mustMarshalRawMessage(t, map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{"type": "string"},
					},
				}),
				Strict: true,
			},
		},
	}

	result := convertTools(tools)

	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}
	if result[0].Type != "function" {
		t.Errorf("expected type 'function', got %s", result[0].Type)
	}
	if result[0].Function.Name != testGetWeather {
		t.Errorf("expected name %q, got %s", testGetWeather, result[0].Function.Name)
	}
	if !result[0].Function.Strict {
		t.Error("expected Strict to be true")
	}
}

func TestConvertToolChoice(t *testing.T) {
	tests := []struct {
		name     string
		input    *llms.ToolChoice
		expected any
	}{
		{
			name:     "string auto",
			input:    &llms.ToolChoice{Type: llms.ToolChoiceAuto},
			expected: "auto",
		},
		{
			name:     "string none",
			input:    &llms.ToolChoice{Type: llms.ToolChoiceNone},
			expected: "none",
		},
		{
			name: "ToolChoice struct",
			input: &llms.ToolChoice{
				Type: llms.ToolChoiceType("function"),
				Function: &llms.FunctionReference{
					Name: testGetWeather,
				},
			},
			expected: ToolChoiceFunction{
				Type: "function",
				Function: &FunctionSelector{
					Name: testGetWeather,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertToolChoice(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestConvertToolCallsToLLMs(t *testing.T) {
	toolCalls := []ToolCall{
		{
			ID:   "call_123",
			Type: "function",
			Function: &FunctionCall{
				Name:      testGetWeather,
				Arguments: `{"location":"NYC"}`,
			},
		},
	}

	result := convertToolCallsToLLMs(toolCalls)

	if len(result) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result))
	}
	if result[0].ID != "call_123" {
		t.Errorf("expected ID 'call_123', got %s", result[0].ID)
	}
	if result[0].Function.Name != testGetWeather {
		t.Errorf("expected function name %q, got %s", testGetWeather, result[0].Function.Name)
	}
}

func TestConvertToolCallsToLLMsEmpty(t *testing.T) {
	result := convertToolCallsToLLMs(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}

	result = convertToolCallsToLLMs([]ToolCall{})
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestAppendOrMergeToolCall(t *testing.T) {
	// Test appending a new tool call
	calls := []llms.ToolCall{}
	delta := ToolCall{
		ID:   "call_123",
		Type: "function",
		Function: &FunctionCall{
			Name:      testGetWeather,
			Arguments: `{"loc`,
		},
	}

	calls = appendOrMergeToolCall(calls, delta)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Arguments != `{"loc` {
		t.Errorf("expected arguments '{\"loc', got %s", calls[0].Function.Arguments)
	}

	// Test merging with existing tool call
	delta2 := ToolCall{
		ID:   "call_123",
		Type: "function",
		Function: &FunctionCall{
			Arguments: `ation":"NYC"}`,
		},
	}

	calls = appendOrMergeToolCall(calls, delta2)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call after merge, got %d", len(calls))
	}
	if calls[0].Function.Arguments != `{"location":"NYC"}` {
		t.Errorf("expected merged arguments, got %s", calls[0].Function.Arguments)
	}
}

func intPtr(i int) *int {
	return &i
}

func TestAppendOrMergeToolCallWithIndex(t *testing.T) {
	// Test OpenAI streaming pattern: first delta has index, id, and name
	calls := []llms.ToolCall{}
	delta1 := ToolCall{
		Index: intPtr(0),
		ID:    "call_abc123",
		Type:  "function",
		Function: &FunctionCall{
			Name:      "browser",
			Arguments: "",
		},
	}

	calls = appendOrMergeToolCall(calls, delta1)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].ID != "call_abc123" {
		t.Errorf("expected ID 'call_abc123', got %s", calls[0].ID)
	}
	if calls[0].Function.Name != "browser" {
		t.Errorf("expected function name 'browser', got %s", calls[0].Function.Name)
	}

	// Second delta: only index and arguments (no id, no name - typical streaming pattern)
	delta2 := ToolCall{
		Index: intPtr(0),
		Function: &FunctionCall{
			Arguments: `{"action": "fetch_text", `,
		},
	}

	calls = appendOrMergeToolCall(calls, delta2)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call after merge, got %d", len(calls))
	}
	if calls[0].ID != "call_abc123" {
		t.Errorf("ID should be preserved, got %s", calls[0].ID)
	}
	if calls[0].Function.Name != "browser" {
		t.Errorf("function name should be preserved, got %s", calls[0].Function.Name)
	}
	if calls[0].Function.Arguments != `{"action": "fetch_text", ` {
		t.Errorf("expected arguments to be appended, got %s", calls[0].Function.Arguments)
	}

	// Third delta: more arguments
	delta3 := ToolCall{
		Index: intPtr(0),
		Function: &FunctionCall{
			Arguments: `"url": "https://example.com"}`,
		},
	}

	calls = appendOrMergeToolCall(calls, delta3)

	expectedArgs := `{"action": "fetch_text", "url": "https://example.com"}`
	if calls[0].Function.Arguments != expectedArgs {
		t.Errorf("expected merged arguments %s, got %s", expectedArgs, calls[0].Function.Arguments)
	}
}

func TestAppendOrMergeToolCallMultipleWithIndex(t *testing.T) {
	// Test multiple tool calls being accumulated
	calls := []llms.ToolCall{}

	// First tool call, first delta
	calls = appendOrMergeToolCall(calls, ToolCall{
		Index: intPtr(0),
		ID:    "call_1",
		Type:  "function",
		Function: &FunctionCall{
			Name:      "get_weather",
			Arguments: "",
		},
	})

	// Second tool call, first delta
	calls = appendOrMergeToolCall(calls, ToolCall{
		Index: intPtr(1),
		ID:    "call_2",
		Type:  "function",
		Function: &FunctionCall{
			Name:      "get_time",
			Arguments: "",
		},
	})

	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}

	// Arguments for first tool call
	calls = appendOrMergeToolCall(calls, ToolCall{
		Index: intPtr(0),
		Function: &FunctionCall{
			Arguments: `{"city": "NYC"}`,
		},
	})

	// Arguments for second tool call
	calls = appendOrMergeToolCall(calls, ToolCall{
		Index: intPtr(1),
		Function: &FunctionCall{
			Arguments: `{"timezone": "EST"}`,
		},
	})

	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls after merges, got %d", len(calls))
	}

	if calls[0].ID != "call_1" {
		t.Errorf("expected first call ID 'call_1', got %s", calls[0].ID)
	}
	if calls[0].Function.Name != "get_weather" {
		t.Errorf("expected first call name 'get_weather', got %s", calls[0].Function.Name)
	}
	if calls[0].Function.Arguments != `{"city": "NYC"}` {
		t.Errorf("expected first call arguments, got %s", calls[0].Function.Arguments)
	}

	if calls[1].ID != "call_2" {
		t.Errorf("expected second call ID 'call_2', got %s", calls[1].ID)
	}
	if calls[1].Function.Name != "get_time" {
		t.Errorf("expected second call name 'get_time', got %s", calls[1].Function.Name)
	}
	if calls[1].Function.Arguments != `{"timezone": "EST"}` {
		t.Errorf("expected second call arguments, got %s", calls[1].Function.Arguments)
	}
}

func TestAppendOrMergeToolCallIndexWithNilFunction(t *testing.T) {
	// Test handling of delta with index but no existing function
	calls := []llms.ToolCall{}

	// First delta creates entry with index but no function yet
	calls = appendOrMergeToolCall(calls, ToolCall{
		Index: intPtr(0),
		ID:    "call_1",
		Type:  "function",
	})

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}

	// Second delta adds function details
	calls = appendOrMergeToolCall(calls, ToolCall{
		Index: intPtr(0),
		Function: &FunctionCall{
			Name:      "test_func",
			Arguments: `{"key": "value"}`,
		},
	})

	if calls[0].Function == nil {
		t.Fatal("expected function to be set")
	}
	if calls[0].Function.Name != "test_func" {
		t.Errorf("expected function name 'test_func', got %s", calls[0].Function.Name)
	}
	if calls[0].Function.Arguments != `{"key": "value"}` {
		t.Errorf("expected arguments, got %s", calls[0].Function.Arguments)
	}
}

func TestConvertResponse(t *testing.T) {
	resp := &ChatCompletionResponse{
		Choices: []Choice{
			{
				Message: &ChatMessage{
					ContentValue: "Hello!",
				},
				FinishReason: "stop",
			},
		},
		Usage: &Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	result := ConvertResponse(resp)

	if result.Content != "Hello!" {
		t.Errorf("expected content 'Hello!', got %s", result.Content)
	}
	if result.FinishReason != "stop" {
		t.Errorf("expected finish reason 'stop', got %s", result.FinishReason)
	}
	if result.Usage.TotalTokens != 15 {
		t.Errorf("expected 15 total tokens, got %d", result.Usage.TotalTokens)
	}
}

func TestConvertResponseEmpty(t *testing.T) {
	resp := &ChatCompletionResponse{
		Choices: []Choice{},
	}

	result := ConvertResponse(resp)

	if result.Content != "" {
		t.Errorf("expected empty content, got %s", result.Content)
	}
}

func TestConvertResponse_WithReasoningContent(t *testing.T) {
	resp := &ChatCompletionResponse{
		Choices: []Choice{
			{
				Message: &ChatMessage{
					Role:             "assistant",
					ContentValue:     "The answer is 42.",
					ReasoningContent: "Let me calculate step by step...",
				},
				FinishReason: "stop",
			},
		},
		Usage: &Usage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}

	result := ConvertResponse(resp)

	if result.Content != "The answer is 42." {
		t.Errorf("expected content 'The answer is 42.', got %s", result.Content)
	}
	if result.Reasoning == nil {
		t.Fatal("expected Reasoning to be populated")
	}
	if result.Reasoning.Content != "Let me calculate step by step..." {
		t.Errorf("expected reasoning content, got %s", result.Reasoning.Content)
	}
}

func TestConvertResponse_NoReasoningContent(t *testing.T) {
	resp := &ChatCompletionResponse{
		Choices: []Choice{
			{
				Message: &ChatMessage{
					Role:         "assistant",
					ContentValue: "Simple answer",
				},
				FinishReason: "stop",
			},
		},
	}

	result := ConvertResponse(resp)

	if result.Reasoning != nil {
		t.Error("expected Reasoning to be nil when no reasoning content")
	}
}

func TestConvertResponse_ReasoningTokens(t *testing.T) {
	resp := &ChatCompletionResponse{
		Choices: []Choice{{
			Message:      &ChatMessage{Role: "assistant", ContentValue: "ok", Reasoning: "deliberating"},
			FinishReason: "stop",
		}},
		Usage: &Usage{
			PromptTokens:     10,
			CompletionTokens: 40,
			TotalTokens:      50,
			CompletionTokensDetails: &struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			}{ReasoningTokens: 25},
		},
	}

	result := ConvertResponse(resp)

	if result.Usage.ReasoningTokens != 25 {
		t.Errorf("expected Usage.ReasoningTokens=25, got %d", result.Usage.ReasoningTokens)
	}
	if result.Reasoning == nil || result.Reasoning.Tokens != 25 {
		t.Errorf("expected Reasoning.Tokens=25, got %+v", result.Reasoning)
	}
}

func TestBuildChatRequest_ReasoningEffort(t *testing.T) {
	opts := llms.ApplyOptions(llms.WithReasoningEffort(llms.ReasoningEffortHigh))
	req := BuildChatRequest("gpt-5", nil, opts, false)

	if req.ReasoningEffort != "high" {
		t.Errorf("expected reasoning_effort=high, got %q", req.ReasoningEffort)
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["reasoning_effort"] != "high" {
		t.Errorf("expected reasoning_effort=high in JSON, got %v", m["reasoning_effort"])
	}
}

func TestBuildChatRequest_ThinkingToggle(t *testing.T) {
	// An enabled reasoning toggle maps to the boolean "thinking" extension, which
	// BuildChatRequest flattens into the top-level request JSON (Z.AI/Qwen style).
	enabled := true
	opts := llms.ApplyOptions(llms.WithReasoning(llms.ReasoningConfig{Enabled: &enabled}))
	req := BuildChatRequest("glm-4.6", nil, opts, false)

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	thinking, ok := m["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("expected thinking object in request JSON, got %v", m["thinking"])
	}
	if thinking["type"] != "enabled" {
		t.Errorf("expected thinking.type=enabled, got %v", thinking["type"])
	}
}

func TestBuildChatRequest_ReasoningDoesNotMutateOptions(t *testing.T) {
	// Translating reasoning into ExtraBody must not mutate the caller's option map.
	enabled := true
	opts := llms.ApplyOptions(
		llms.WithExtraBodyParam("foo", "bar"),
		llms.WithReasoning(llms.ReasoningConfig{Enabled: &enabled}),
	)
	_ = BuildChatRequest("glm-4.6", nil, opts, false)
	if _, exists := opts.ExtraBody["thinking"]; exists {
		t.Error("BuildChatRequest leaked the thinking key into the caller's ExtraBody")
	}
}

func TestConvertEmbeddingResponse(t *testing.T) {
	resp := &EmbeddingResponse{
		Data: []EmbeddingData{
			{
				Index:     0,
				Embedding: []float32{0.1, 0.2, 0.3},
				Object:    "embedding",
			},
		},
		Model: "text-embedding-3-small",
		Usage: EmbeddingUsage{
			PromptTokens: 5,
			TotalTokens:  5,
		},
	}

	result := ConvertEmbeddingResponse(resp)

	if len(result.Embeddings) != 1 {
		t.Fatalf("expected 1 embedding, got %d", len(result.Embeddings))
	}
	if result.Model != "text-embedding-3-small" {
		t.Errorf("expected model 'text-embedding-3-small', got %s", result.Model)
	}
	if len(result.Embeddings[0].Vector) != 3 {
		t.Errorf("expected 3 dimensions, got %d", len(result.Embeddings[0].Vector))
	}
}

func TestBuildChatRequest(t *testing.T) {
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Hello"},
	}
	opts := &llms.CallOptions{
		Temperature: float64Ptr(0.7),
		MaxTokens:   intPtr(100),
		TopP:        float64Ptr(0.9),
		ResponseFormat: &llms.ResponseFormat{
			Type: llms.ResponseFormatJSONObject,
		},
	}

	req := BuildChatRequest("gpt-4", messages, opts, false)

	if req.Model != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got %s", req.Model)
	}
	if req.Temperature == nil || *req.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", req.Temperature)
	}
	if req.MaxTokens == nil || *req.MaxTokens != 100 {
		t.Errorf("expected max tokens 100, got %v", req.MaxTokens)
	}
	if req.Stream != false {
		t.Error("expected stream to be false")
	}
	if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_object" {
		t.Error("expected response format to be json_object")
	}
}

func TestBuildChatRequestWithTools(t *testing.T) {
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Hello"},
	}
	opts := &llms.CallOptions{
		Tools: []llms.Tool{
			{
				Type: "function",
				Function: &llms.FunctionDefinition{
					Name: "test",
				},
			},
		},
		ToolChoice: &llms.ToolChoice{Type: llms.ToolChoiceAuto},
	}

	req := BuildChatRequest("gpt-4", messages, opts, true)

	if len(req.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(req.Tools))
	}
	if req.ToolChoice != "auto" {
		t.Errorf("expected tool choice 'auto', got %v", req.ToolChoice)
	}
	if !req.Stream {
		t.Error("expected stream to be true")
	}
}

func TestProcessStream_ReasoningContentDelta(t *testing.T) {
	// Verify ChatMessage.ReasoningContent is accessible during streaming
	delta := &ChatMessage{
		Role:             "assistant",
		ReasoningContent: "thinking step...",
	}

	// Content should fall back to ReasoningContent if Content is empty
	// This is already implemented in types.go
	content := delta.Content()
	if content != "thinking step..." {
		t.Errorf("expected reasoning content fallback, got %s", content)
	}

	// Verify we can check ReasoningContent directly
	if delta.ReasoningContent != "thinking step..." {
		t.Errorf("expected ReasoningContent to be accessible, got %s", delta.ReasoningContent)
	}
}
