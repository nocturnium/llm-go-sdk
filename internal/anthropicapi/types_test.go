package anthropicapi

import (
	"encoding/json"
	"testing"
)

const (
	testMsgID             = "msg_123"
	testToolID            = "toolu_123"
	testMsgStart          = "message_start"
	testContentBlockStart = "content_block_start"
)

// float64Ptr returns a pointer to the given float64, for building requests whose
// sampling parameters are *float64 (so an explicit 0.0 is distinguishable from unset).
func float64Ptr(v float64) *float64 { return &v }

func TestMessagesRequest_JSON(t *testing.T) {
	req := MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []Message{
			{
				Role: "user",
				Content: []ContentPart{
					{Type: "text", Text: "Hello"},
				},
			},
		},
		Temperature: float64Ptr(0.7),
		System:      []SystemBlock{{Type: "text", Text: "You are helpful."}},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded MessagesRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected model=claude-sonnet-4-20250514, got %s", decoded.Model)
	}
	if decoded.MaxTokens != 1024 {
		t.Errorf("expected max_tokens=1024, got %d", decoded.MaxTokens)
	}
	if len(decoded.System) != 1 || decoded.System[0].Text != "You are helpful." {
		t.Errorf("expected system='You are helpful.', got %#v", decoded.System)
	}
}

func TestMessagesRequest_WithTools(t *testing.T) {
	req := MessagesRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []Message{
			{
				Role:    "user",
				Content: []ContentPart{{Type: "text", Text: "Weather?"}},
			},
		},
		Tools: []Tool{
			{
				Name:        "get_weather",
				Description: "Get weather",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"location": map[string]any{"type": "string"},
					},
				},
			},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded MessagesRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(decoded.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(decoded.Tools))
	}
	if decoded.Tools[0].Name != "get_weather" {
		t.Errorf("expected tool name=get_weather, got %s", decoded.Tools[0].Name)
	}
}

func TestMessagesResponse_JSON(t *testing.T) {
	jsonData := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Hello! How can I help?"}
		],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 10,
			"output_tokens": 8
		}
	}`

	var resp MessagesResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp.ID != testMsgID {
		t.Errorf("expected id=msg_123, got %s", resp.ID)
	}
	if resp.Role != "assistant" {
		t.Errorf("expected role=assistant, got %s", resp.Role)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("expected stop_reason=end_turn, got %s", resp.StopReason)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("expected 1 content, got %d", len(resp.Content))
	}
	if resp.Content[0].Text != "Hello! How can I help?" {
		t.Errorf("unexpected text: %s", resp.Content[0].Text)
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("expected input_tokens=10, got %d", resp.Usage.InputTokens)
	}
}

func TestMessagesResponse_WithToolUse(t *testing.T) {
	jsonData := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [
			{
				"type": "tool_use",
				"id": "toolu_123",
				"name": "get_weather",
				"input": {"location": "Boston"}
			}
		],
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 10, "output_tokens": 20}
	}`

	var resp MessagesResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(resp.Content) != 1 {
		t.Fatalf("expected 1 content, got %d", len(resp.Content))
	}
	if resp.Content[0].Type != "tool_use" {
		t.Errorf("expected type=tool_use, got %s", resp.Content[0].Type)
	}
	if resp.Content[0].ID != testToolID {
		t.Errorf("expected id=toolu_123, got %s", resp.Content[0].ID)
	}
	if resp.Content[0].Name != "get_weather" {
		t.Errorf("expected name=get_weather, got %s", resp.Content[0].Name)
	}
}

func TestContentPart_ToolResult(t *testing.T) {
	part := ContentPart{
		Type:      "tool_result",
		ToolUseID: testToolID,
		Content:   `{"temperature": 72}`,
	}

	data, err := json.Marshal(part)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ContentPart
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Type != "tool_result" {
		t.Errorf("expected type=tool_result, got %s", decoded.Type)
	}
	if decoded.ToolUseID != testToolID {
		t.Errorf("expected tool_use_id=toolu_123, got %s", decoded.ToolUseID)
	}
}

func TestContentPart_ToolResultWithError(t *testing.T) {
	part := ContentPart{
		Type:      "tool_result",
		ToolUseID: testToolID,
		Content:   "Error: Location not found",
		IsError:   true,
	}

	data, err := json.Marshal(part)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ContentPart
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if !decoded.IsError {
		t.Error("expected is_error=true")
	}
}

func TestStreamEvent_MessageStart(t *testing.T) {
	jsonData := `{
		"type": "message_start",
		"message": {
			"id": "msg_123",
			"type": "message",
			"role": "assistant",
			"content": [],
			"model": "claude-sonnet-4-20250514",
			"usage": {"input_tokens": 10, "output_tokens": 0}
		}
	}`

	var event StreamEvent
	if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if event.Type != testMsgStart {
		t.Errorf("expected type=message_start, got %s", event.Type)
	}
	if event.Message == nil {
		t.Fatal("expected message to be set")
	}
	if event.Message.ID != testMsgID {
		t.Errorf("expected id=msg_123, got %s", event.Message.ID)
	}
}

func TestStreamEvent_ContentBlockStart(t *testing.T) {
	jsonData := `{
		"type": "content_block_start",
		"index": 0,
		"content_block": {
			"type": "text",
			"text": ""
		}
	}`

	var event StreamEvent
	if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if event.Type != testContentBlockStart {
		t.Errorf("expected type=content_block_start, got %s", event.Type)
	}
	if event.Index != 0 {
		t.Errorf("expected index=0, got %d", event.Index)
	}
	if event.ContentBlock == nil {
		t.Fatal("expected content_block to be set")
	}
}

func TestStreamDelta_TextDelta(t *testing.T) {
	delta := StreamDelta{
		Type: "text_delta",
		Text: "Hello",
	}

	data, err := json.Marshal(delta)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded StreamDelta
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Type != "text_delta" {
		t.Errorf("expected type=text_delta, got %s", decoded.Type)
	}
	if decoded.Text != "Hello" {
		t.Errorf("expected text=Hello, got %s", decoded.Text)
	}
}

func TestToolChoiceTypes(t *testing.T) {
	tests := []struct {
		name     string
		choice   any
		expected string
	}{
		{"auto", ToolChoiceAuto{Type: "auto"}, `{"type":"auto"}`},
		{"any", ToolChoiceAny{Type: "any"}, `{"type":"any"}`},
		{"tool", ToolChoiceTool{Type: "tool", Name: "get_weather"}, `{"type":"tool","name":"get_weather"}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.choice)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}
			if string(data) != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, string(data))
			}
		})
	}
}
