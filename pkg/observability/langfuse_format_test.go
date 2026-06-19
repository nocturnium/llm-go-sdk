package observability

import (
	"encoding/json"
	"testing"
	"unicode/utf8"

	llms "github.com/nocturnium/llm-go-sdk/v3"
)

func TestFormatInput_Raw(t *testing.T) {
	messages := []llms.Message{
		{Role: llms.RoleSystem, Content: "You are helpful."},
		{Role: llms.RoleUser, Content: "Hello!"},
		{Role: llms.RoleAssistant, Content: "Hi there!"},
		{Role: llms.RoleUser, Content: "How are you?"},
	}

	result := FormatInput(messages, InputFormatRaw)
	if result != "How are you?" {
		t.Errorf("expected last user message 'How are you?', got '%s'", result)
	}
}

func TestFormatInput_Raw_NoUserMessage(t *testing.T) {
	messages := []llms.Message{
		{Role: llms.RoleSystem, Content: "You are helpful."},
		{Role: llms.RoleAssistant, Content: "Hi there!"},
	}

	result := FormatInput(messages, InputFormatRaw)
	if result != "Hi there!" {
		t.Errorf("expected last message 'Hi there!', got '%s'", result)
	}
}

func TestFormatInput_Messages(t *testing.T) {
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Hello!"},
	}

	result := FormatInput(messages, InputFormatMessages)

	// Should be valid JSON
	var parsed []llms.Message
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	if len(parsed) != 1 {
		t.Errorf("expected 1 message, got %d", len(parsed))
	}
}

func TestFormatInput_Structured(t *testing.T) {
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "Hello!"},
		{Role: llms.RoleAssistant, Content: "Hi!"},
	}

	result := FormatInput(messages, InputFormatStructured)

	// Should be valid JSON with messages array
	var parsed struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	if len(parsed.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(parsed.Messages))
	}
	if parsed.Messages[0].Role != "user" {
		t.Errorf("expected role 'user', got '%s'", parsed.Messages[0].Role)
	}
}

func TestFormatInput_Empty(t *testing.T) {
	result := FormatInput([]llms.Message{}, InputFormatRaw)
	if result != "" {
		t.Errorf("expected empty string for empty messages, got '%s'", result)
	}
}

func TestFormatOutput_Raw(t *testing.T) {
	resp := &llms.Response{
		Content:      "Hello there!",
		FinishReason: "stop",
	}

	result := FormatOutput(resp, OutputFormatRaw)
	if result != "Hello there!" {
		t.Errorf("expected 'Hello there!', got '%s'", result)
	}
}

func TestFormatOutput_Response(t *testing.T) {
	resp := &llms.Response{
		Content:      "Hello!",
		FinishReason: "stop",
		Usage: llms.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	result := FormatOutput(resp, OutputFormatResponse)

	// Should be valid JSON
	var parsed llms.Response
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	if parsed.Content != "Hello!" {
		t.Errorf("expected content 'Hello!', got '%s'", parsed.Content)
	}
	if parsed.Usage.TotalTokens != 15 {
		t.Errorf("expected TotalTokens 15, got %d", parsed.Usage.TotalTokens)
	}
}

func TestFormatOutput_Structured(t *testing.T) {
	resp := &llms.Response{
		Content:      "Hello!",
		FinishReason: "stop",
		ToolCalls: []llms.ToolCall{
			{ID: "call-1", Type: "function"},
		},
	}

	result := FormatOutput(resp, OutputFormatStructured)

	var parsed struct {
		Content      string          `json:"content"`
		FinishReason string          `json:"finish_reason"`
		ToolCalls    []llms.ToolCall `json:"tool_calls"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	if parsed.Content != "Hello!" {
		t.Errorf("expected content 'Hello!', got '%s'", parsed.Content)
	}
	if parsed.FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got '%s'", parsed.FinishReason)
	}
	if len(parsed.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(parsed.ToolCalls))
	}
}

func TestFormatOutput_Nil(t *testing.T) {
	result := FormatOutput(nil, OutputFormatRaw)
	if result != "" {
		t.Errorf("expected empty string for nil response, got '%s'", result)
	}
}

func TestFormatMessagesCompact(t *testing.T) {
	messages := []llms.Message{
		{Role: llms.RoleUser, Content: "This is a short message."},
		{Role: llms.RoleAssistant, Content: string(make([]byte, 200))}, // Long message
	}

	result := FormatMessagesCompact(messages)

	var parsed []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	// First message should be untruncated
	if parsed[0].Content != "This is a short message." {
		t.Errorf("short message should not be truncated")
	}

	// Second message should be truncated to 100 chars + "..."
	if len(parsed[1].Content) != 103 {
		t.Errorf("expected truncated length 103, got %d", len(parsed[1].Content))
	}
}

func TestFormatMessagesCompact_Empty(t *testing.T) {
	result := FormatMessagesCompact([]llms.Message{})
	if result != "[]" {
		t.Errorf("expected '[]' for empty messages, got '%s'", result)
	}
}

func TestTruncateContent_UTF8Safe(t *testing.T) {
	result := truncateContent("ab😀cd", 4)
	if !utf8.ValidString(result) {
		t.Fatalf("truncateContent returned invalid UTF-8: %q", result)
	}
	if result != "ab..." {
		t.Errorf("truncateContent returned %q, want %q", result, "ab...")
	}
}
