package openaicompat

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v4"
	"github.com/nocturnium/llm-go-sdk/v4/internal/httpclient"
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

	// A nil image produces NO content part — previously it left a zero-value
	// {"type":""} entry that OpenAI rejects with a 400 on the whole request.
	if len(result) != 0 {
		t.Fatalf("expected 0 parts for a nil image, got %d: %+v", len(result), result)
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
			input:    &llms.ToolChoice{Mode: llms.ToolChoiceAuto},
			expected: "auto",
		},
		{
			name:     "string none",
			input:    &llms.ToolChoice{Mode: llms.ToolChoiceNone},
			expected: "none",
		},
		{
			name: "ToolChoice struct",
			input: &llms.ToolChoice{
				Mode: llms.ToolChoiceTool,
				Tool: testGetWeather,
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

func TestAppendOrMergeToolCall_NegativeIndexNoPanic(t *testing.T) {
	neg := -1
	// A negative index must NOT panic on calls[-1]; it falls through to ID-based
	// matching and is appended as a new call.
	calls := appendOrMergeToolCall(nil, ToolCall{Index: &neg, ID: "x", Function: &FunctionCall{Name: "a"}})
	if len(calls) != 1 || calls[0].ID != "x" {
		t.Fatalf("expected one call via ID fallback, got %+v", calls)
	}
}

func TestAppendOrMergeToolCall_HugeIndexBounded(t *testing.T) {
	huge := 1 << 30
	// An absurd index must be rejected, not back-filled (OOM guard).
	calls := appendOrMergeToolCall(nil, ToolCall{Index: &huge, ID: "x"})
	if len(calls) != 0 {
		t.Fatalf("expected absurd index rejected, got %d calls", len(calls))
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

func TestConvertResponse_ReasoningOnlyDoesNotDuplicateIntoContent(t *testing.T) {
	resp := &ChatCompletionResponse{
		Choices: []Choice{
			{
				Message: &ChatMessage{
					Role:             "assistant",
					ReasoningContent: "SECRET",
				},
				FinishReason: "stop",
			},
		},
	}

	result := ConvertResponse(resp)

	if result.Content != "" {
		t.Errorf("expected empty visible content, got %q", result.Content)
	}
	if result.Reasoning == nil {
		t.Fatal("expected Reasoning to be populated")
	}
	if result.Reasoning.Content != "SECRET" {
		t.Errorf("expected reasoning SECRET, got %q", result.Reasoning.Content)
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

func TestBuildChatRequest_OpenAIReasoningModel(t *testing.T) {
	opts := llms.ApplyOptions(
		llms.WithMaxTokens(1000),
		llms.WithTemperature(0.7),
		llms.WithTopP(0.9),
		llms.WithFrequencyPenalty(0.5),
		llms.WithPresencePenalty(0.25),
	)
	req := BuildChatRequest("o3-mini", nil, opts, false)

	if req.MaxTokens != nil {
		t.Error("o-series: max_tokens must be omitted")
	}
	if req.MaxCompletionTokens == nil || *req.MaxCompletionTokens != 1000 {
		t.Errorf("o-series: expected max_completion_tokens=1000, got %v", req.MaxCompletionTokens)
	}
	if req.Temperature != nil {
		t.Error("o-series: temperature must be dropped")
	}
	if req.TopP != nil {
		t.Error("o-series: top_p must be dropped")
	}
	if req.FrequencyPenalty != nil {
		t.Error("o-series: frequency_penalty must be dropped")
	}
	if req.PresencePenalty != nil {
		t.Error("o-series: presence_penalty must be dropped")
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["max_tokens"]; ok {
		t.Error("o-series wire: max_tokens must not appear")
	}
	if m["max_completion_tokens"] != float64(1000) {
		t.Errorf("o-series wire: max_completion_tokens=%v", m["max_completion_tokens"])
	}
	if _, ok := m["temperature"]; ok {
		t.Error("o-series wire: temperature must not appear")
	}
	if _, ok := m["top_p"]; ok {
		t.Error("o-series wire: top_p must not appear")
	}
	if _, ok := m["frequency_penalty"]; ok {
		t.Error("o-series wire: frequency_penalty must not appear")
	}
	if _, ok := m["presence_penalty"]; ok {
		t.Error("o-series wire: presence_penalty must not appear")
	}
}

func TestBuildChatRequest_NonReasoningModelUnchanged(t *testing.T) {
	opts := llms.ApplyOptions(llms.WithMaxTokens(1000), llms.WithTemperature(0.7))
	req := BuildChatRequest("gpt-4o", nil, opts, false)

	if req.MaxTokens == nil || *req.MaxTokens != 1000 {
		t.Error("gpt-4o must keep max_tokens")
	}
	if req.MaxCompletionTokens != nil {
		t.Error("gpt-4o must not use max_completion_tokens")
	}
	if req.Temperature == nil {
		t.Error("gpt-4o must keep temperature")
	}
}

func TestBuildChatRequest_StreamOptionsSerialization(t *testing.T) {
	messages := []llms.Message{{Role: llms.RoleUser, Content: "Hello"}}
	opts := llms.ApplyOptions()

	streamReq := BuildChatRequest("gpt-4o", messages, opts, true)
	streamData, err := json.Marshal(streamReq)
	if err != nil {
		t.Fatalf("marshal streaming request: %v", err)
	}
	var streamBody map[string]any
	if err := json.Unmarshal(streamData, &streamBody); err != nil {
		t.Fatalf("unmarshal streaming request: %v", err)
	}
	if streamBody["stream"] != true {
		t.Fatalf("expected stream=true in streaming JSON, got %v in %s", streamBody["stream"], streamData)
	}
	streamOptions, ok := streamBody["stream_options"].(map[string]any)
	if !ok {
		t.Fatalf("expected stream_options object in streaming JSON, got %v in %s", streamBody["stream_options"], streamData)
	}
	if streamOptions["include_usage"] != true {
		t.Fatalf("expected stream_options.include_usage=true, got %v in %s", streamOptions["include_usage"], streamData)
	}

	nonStreamReq := BuildChatRequest("gpt-4o", messages, opts, false)
	nonStreamData, err := json.Marshal(nonStreamReq)
	if err != nil {
		t.Fatalf("marshal non-streaming request: %v", err)
	}
	var nonStreamBody map[string]any
	if err := json.Unmarshal(nonStreamData, &nonStreamBody); err != nil {
		t.Fatalf("unmarshal non-streaming request: %v", err)
	}
	if _, ok := nonStreamBody["stream"]; ok {
		t.Fatalf("expected stream omitted in non-streaming JSON, got %s", nonStreamData)
	}
	if _, ok := nonStreamBody["stream_options"]; ok {
		t.Fatalf("expected stream_options omitted in non-streaming JSON, got %s", nonStreamData)
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

func TestBuildChatRequest_PenaltyZeroSerialization(t *testing.T) {
	messages := []llms.Message{{Role: llms.RoleUser, Content: "Hello"}}

	t.Run("explicit zero is sent", func(t *testing.T) {
		opts := llms.ApplyOptions(
			llms.WithFrequencyPenalty(0),
			llms.WithPresencePenalty(0),
		)
		req := BuildChatRequest("gpt-4o", messages, opts, false)

		if req.FrequencyPenalty == nil || *req.FrequencyPenalty != 0 {
			t.Fatalf("expected frequency penalty pointer to explicit 0, got %v", req.FrequencyPenalty)
		}
		if req.PresencePenalty == nil || *req.PresencePenalty != 0 {
			t.Fatalf("expected presence penalty pointer to explicit 0, got %v", req.PresencePenalty)
		}

		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got, ok := m["frequency_penalty"]; !ok || got != float64(0) {
			t.Errorf("expected frequency_penalty=0, got value=%v present=%v json=%s", got, ok, data)
		}
		if got, ok := m["presence_penalty"]; !ok || got != float64(0) {
			t.Errorf("expected presence_penalty=0, got value=%v present=%v json=%s", got, ok, data)
		}
	})

	t.Run("unset is omitted", func(t *testing.T) {
		opts := llms.ApplyOptions()
		req := BuildChatRequest("gpt-4o", messages, opts, false)

		if req.FrequencyPenalty != nil {
			t.Fatalf("expected frequency penalty to be nil, got %v", req.FrequencyPenalty)
		}
		if req.PresencePenalty != nil {
			t.Fatalf("expected presence penalty to be nil, got %v", req.PresencePenalty)
		}

		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if _, ok := m["frequency_penalty"]; ok {
			t.Errorf("expected frequency_penalty to be omitted, got %s", data)
		}
		if _, ok := m["presence_penalty"]; ok {
			t.Errorf("expected presence_penalty to be omitted, got %s", data)
		}
	})
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
		ToolChoice: &llms.ToolChoice{Mode: llms.ToolChoiceAuto},
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

func testStreamReader(body io.ReadCloser) *StreamReader {
	return &StreamReader{sseReader: httpclient.NewSSEReader(body)}
}

func testSSEStream(t *testing.T, chunks ...StreamChunk) *StreamReader {
	t.Helper()

	var b strings.Builder
	for _, chunk := range chunks {
		data, err := json.Marshal(chunk)
		if err != nil {
			t.Fatalf("marshal stream chunk: %v", err)
		}
		b.WriteString("data: ")
		b.Write(data)
		b.WriteString("\n\n")
	}
	b.WriteString("data: [DONE]\n\n")

	return testStreamReader(io.NopCloser(strings.NewReader(b.String())))
}

func TestProcessStream_ReasoningNotVisibleAndNotDoubled(t *testing.T) {
	stream := testSSEStream(t,
		StreamChunk{Choices: []Choice{{Delta: &ChatMessage{ReasoningContent: "SECRET"}}}},
		StreamChunk{Choices: []Choice{{Delta: &ChatMessage{ContentValue: "Hello"}}}},
		StreamChunk{Choices: []Choice{{FinishReason: "stop"}}},
	)
	chunks := make(chan llms.StreamChunk, 8)
	sender := llms.NewStreamSender(context.Background(), chunks, time.Second)

	ProcessStream(context.Background(), stream, chunks, sender, "test", nil)

	var content strings.Builder
	var reasoning strings.Builder
	var final llms.StreamChunk
	for chunk := range chunks {
		content.WriteString(chunk.Content)
		if chunk.Reasoning != nil {
			reasoning.WriteString(chunk.Reasoning.Content)
		}
		if chunk.Done || chunk.Error != nil {
			final = chunk
		}
	}

	if got := content.String(); got != "Hello" {
		t.Errorf("expected visible content Hello, got %q", got)
	}
	if strings.Contains(content.String(), "SECRET") {
		t.Fatalf("reasoning leaked into visible content: %q", content.String())
	}
	if got := reasoning.String(); got != "SECRET" {
		t.Errorf("expected reasoning once, got %q", got)
	}
	if final.Reasoning != nil {
		t.Errorf("expected final chunk not to resend reasoning, got %+v", final.Reasoning)
	}
}

func TestProcessStream_ContextCancelUnblocksRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pr, _ := io.Pipe()
	stream := testStreamReader(pr)
	chunks := make(chan llms.StreamChunk, 2)
	sender := llms.NewStreamSender(ctx, chunks, time.Second)
	done := make(chan struct{})

	go func() {
		defer close(done)
		ProcessStream(ctx, stream, chunks, sender, "test", nil)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ProcessStream did not exit after context cancellation")
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

// TestConvertContentParts_SkipsNilImageAndUnknown pins that a nil image or an
// unknown part type is skipped, not emitted as a malformed {"type":""} entry
// that OpenAI 400s on the whole request.
func TestConvertContentParts_SkipsNilImageAndUnknown(t *testing.T) {
	parts := []llms.ContentPart{
		{Type: llms.PartTypeText, Text: "hello"},
		{Type: llms.PartTypeImage, Image: nil}, // nil image → skip, not {"type":""}
		{Type: llms.PartType("audio")},         // unknown part type → skip
	}
	out := convertContentParts(parts)
	for _, c := range out {
		if c.Type == "" {
			t.Errorf("emitted a malformed empty-type content part: %+v", out)
		}
	}
	if len(out) != 1 || out[0].Type != "text" || out[0].Text != "hello" {
		t.Errorf("expected only the text part, got %+v", out)
	}
}
