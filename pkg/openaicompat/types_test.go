package openaicompat

import (
	"encoding/json"
	"testing"
)

const (
	testGPT4        = "gpt-4"
	testGetWeather  = "get_weather"
	testChatCmpl123 = "chatcmpl-123"
	testStop        = "stop"
	testHello       = "Hello"
)

// float64Ptr returns a pointer to the given float64, for building requests whose
// sampling parameters are *float64 (so an explicit 0.0 is distinguishable from unset).
func float64Ptr(v float64) *float64 { return &v }

func TestChatCompletionRequest_JSON(t *testing.T) {
	req := ChatCompletionRequest{
		Model: testGPT4,
		Messages: []ChatMessage{
			{Role: "system", ContentValue: "You are helpful."},
			{Role: "user", ContentValue: testHello},
		},
		Temperature: float64Ptr(0.7),
		MaxTokens:   intPtr(100),
		TopP:        float64Ptr(0.9),
		Stream:      true,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ChatCompletionRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Model != testGPT4 {
		t.Errorf("expected model=gpt-4, got %s", decoded.Model)
	}
	if len(decoded.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(decoded.Messages))
	}
	if decoded.Temperature == nil || *decoded.Temperature != 0.7 {
		t.Errorf("expected temperature=0.7, got %v", decoded.Temperature)
	}
	if decoded.MaxTokens == nil || *decoded.MaxTokens != 100 {
		t.Errorf("expected max_tokens=100, got %v", decoded.MaxTokens)
	}
	if !decoded.Stream {
		t.Error("expected stream=true")
	}
}

// TestChatCompletionRequest_TemperatureZeroIsSerialized proves that an explicit
// temperature/top_p of 0.0 is serialized to the wire, while an unset (nil)
// value is omitted so the provider applies its own default.
func TestChatCompletionRequest_TemperatureZeroIsSerialized(t *testing.T) {
	t.Run("explicit zero is sent", func(t *testing.T) {
		req := ChatCompletionRequest{
			Model:       testGPT4,
			Messages:    []ChatMessage{{Role: "user", ContentValue: testHello}},
			Temperature: float64Ptr(0),
			TopP:        float64Ptr(0),
		}

		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		temp, ok := m["temperature"]
		if !ok {
			t.Fatalf("expected temperature key to be present, got %s", data)
		}
		if temp != float64(0) {
			t.Errorf("expected temperature=0, got %v", temp)
		}

		topP, ok := m["top_p"]
		if !ok {
			t.Fatalf("expected top_p key to be present, got %s", data)
		}
		if topP != float64(0) {
			t.Errorf("expected top_p=0, got %v", topP)
		}
	})

	t.Run("unset is omitted", func(t *testing.T) {
		req := ChatCompletionRequest{
			Model:    testGPT4,
			Messages: []ChatMessage{{Role: "user", ContentValue: testHello}},
			// Temperature and TopP left nil (unset).
		}

		data, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if _, ok := m["temperature"]; ok {
			t.Errorf("expected temperature to be omitted, got %s", data)
		}
		if _, ok := m["top_p"]; ok {
			t.Errorf("expected top_p to be omitted, got %s", data)
		}
	})
}

func TestChatCompletionRequest_WithTools(t *testing.T) {
	req := ChatCompletionRequest{
		Model: testGPT4,
		Messages: []ChatMessage{
			{Role: "user", ContentValue: "What's the weather?"},
		},
		Tools: []Tool{
			{
				Type: "function",
				Function: &FunctionDefinition{
					Name:        testGetWeather,
					Description: "Get weather for a location",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"location": map[string]any{
								"type": "string",
							},
						},
					},
				},
			},
		},
		ToolChoice: "auto",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ChatCompletionRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(decoded.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(decoded.Tools))
	}
	if decoded.Tools[0].Function.Name != testGetWeather {
		t.Errorf("expected function name=get_weather, got %s", decoded.Tools[0].Function.Name)
	}
}

func TestChatCompletionRequest_OmitEmpty(t *testing.T) {
	req := ChatCompletionRequest{
		Model: testGPT4,
		Messages: []ChatMessage{
			{Role: "user", ContentValue: testHello},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var rawMap map[string]any
	if err := json.Unmarshal(data, &rawMap); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Check that optional fields are omitted
	if _, exists := rawMap["tools"]; exists {
		t.Error("expected tools to be omitted")
	}
	if _, exists := rawMap["stream"]; exists {
		t.Error("expected stream to be omitted when false")
	}
	if _, exists := rawMap["response_format"]; exists {
		t.Error("expected response_format to be omitted")
	}
}

func TestChatCompletionResponse_JSON(t *testing.T) {
	jsonData := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"created": 1677652288,
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "Hello! How can I help?"
			},
			"finish_reason": "stop"
		}],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 5,
			"total_tokens": 15
		}
	}`

	var resp ChatCompletionResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp.ID != testChatCmpl123 {
		t.Errorf("expected id=chatcmpl-123, got %s", resp.ID)
	}
	if resp.Model != testGPT4 {
		t.Errorf("expected model=gpt-4, got %s", resp.Model)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.ContentValue != "Hello! How can I help?" {
		t.Errorf("unexpected content: %s", resp.Choices[0].Message.ContentValue)
	}
	if resp.Choices[0].FinishReason != testStop {
		t.Errorf("expected finish_reason=stop, got %s", resp.Choices[0].FinishReason)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("expected total_tokens=15, got %d", resp.Usage.TotalTokens)
	}
}

func TestChatCompletionResponse_WithToolCalls(t *testing.T) {
	jsonData := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"created": 1677652288,
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": null,
				"tool_calls": [{
					"id": "call_abc123",
					"type": "function",
					"function": {
						"name": "get_weather",
						"arguments": "{\"location\": \"Boston\"}"
					}
				}]
			},
			"finish_reason": "tool_calls"
		}]
	}`

	var resp ChatCompletionResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.Choices[0].Message.ToolCalls))
	}

	tc := resp.Choices[0].Message.ToolCalls[0]
	if tc.ID != "call_abc123" {
		t.Errorf("expected id=call_abc123, got %s", tc.ID)
	}
	if tc.Function.Name != testGetWeather {
		t.Errorf("expected function name=get_weather, got %s", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"location": "Boston"}` {
		t.Errorf("unexpected arguments: %s", tc.Function.Arguments)
	}
}

func TestStreamChunk_JSON(t *testing.T) {
	jsonData := `{
		"id": "chatcmpl-123",
		"object": "chat.completion.chunk",
		"created": 1677652288,
		"model": "gpt-4",
		"choices": [{
			"index": 0,
			"delta": {
				"content": "Hello"
			},
			"finish_reason": null
		}]
	}`

	var chunk StreamChunk
	if err := json.Unmarshal([]byte(jsonData), &chunk); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if chunk.ID != testChatCmpl123 {
		t.Errorf("expected id=chatcmpl-123, got %s", chunk.ID)
	}
	if len(chunk.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(chunk.Choices))
	}
	if chunk.Choices[0].Delta.ContentValue != testHello {
		t.Errorf("expected delta content=Hello, got %s", chunk.Choices[0].Delta.ContentValue)
	}
}

func TestToolChoiceFunction_JSON(t *testing.T) {
	tc := ToolChoiceFunction{
		Type: "function",
		Function: &FunctionSelector{
			Name: testGetWeather,
		},
	}

	data, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	expected := `{"type":"function","function":{"name":"get_weather"}}`
	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}
}

func TestChatMessage_WithToolCallID(t *testing.T) {
	msg := ChatMessage{
		Role:         "tool",
		ContentValue: `{"temperature": 72}`,
		ToolCallID:   "call_abc123",
		Name:         testGetWeather,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ChatMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ToolCallID != "call_abc123" {
		t.Errorf("expected tool_call_id=call_abc123, got %s", decoded.ToolCallID)
	}
	if decoded.Name != testGetWeather {
		t.Errorf("expected name=get_weather, got %s", decoded.Name)
	}
}

func TestResponseFormat_JSON(t *testing.T) {
	rf := ResponseFormat{Type: "json_object"}

	data, err := json.Marshal(rf)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	expected := `{"type":"json_object"}`
	if string(data) != expected {
		t.Errorf("expected %s, got %s", expected, string(data))
	}
}
