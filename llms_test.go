package llms

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const (
	testStop   = "stop"
	testSearch = "search"
)

type testPlainLLM struct{}

func (testPlainLLM) GenerateContent(context.Context, []Message, ...CallOption) (*Response, error) {
	return &Response{}, nil
}

func (testPlainLLM) Stream(context.Context, []Message, ...CallOption) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk)
	close(ch)
	return ch, nil
}

func (testPlainLLM) Provider() Provider { return ProviderOpenAI }
func (testPlainLLM) Model() string      { return "test-model" }

type testWrapper struct {
	LLM
}

func (w testWrapper) Unwrap() LLM {
	return w.LLM
}

func mustMarshalRawMessage(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestProviderConstants(t *testing.T) {
	tests := []struct {
		provider Provider
		expected string
	}{
		{ProviderOpenAI, "openai"},
		{ProviderTogetherAI, "togetherai"},
		{ProviderAnthropic, "anthropic"},
		{ProviderGemini, "gemini"},
		{ProviderFeatherless, "featherless"},
	}

	for _, tt := range tests {
		if string(tt.provider) != tt.expected {
			t.Errorf("expected provider %s, got %s", tt.expected, tt.provider)
		}
	}
}

func TestRoleConstants(t *testing.T) {
	tests := []struct {
		role     Role
		expected string
	}{
		{RoleSystem, "system"},
		{RoleUser, "user"},
		{RoleAssistant, "assistant"},
		{RoleTool, "tool"},
	}

	for _, tt := range tests {
		if string(tt.role) != tt.expected {
			t.Errorf("expected role %s, got %s", tt.expected, tt.role)
		}
	}
}

func TestDefaultCallOptions(t *testing.T) {
	opts := DefaultCallOptions()

	if opts.Model != "" {
		t.Errorf("expected default model to be empty, got %s", opts.Model)
	}
	if opts.Temperature != nil {
		t.Errorf("expected default temperature to be nil (unset), got %v", *opts.Temperature)
	}
	if opts.MaxTokens != nil {
		t.Errorf("expected default max tokens to be nil (unset), got %d", *opts.MaxTokens)
	}
	if opts.TopP != nil {
		t.Errorf("expected default top_p to be nil (unset), got %v", *opts.TopP)
	}
	if opts.FrequencyPenalty != nil {
		t.Errorf("expected default frequency penalty to be nil (unset), got %v", *opts.FrequencyPenalty)
	}
	if opts.PresencePenalty != nil {
		t.Errorf("expected default presence penalty to be nil (unset), got %v", *opts.PresencePenalty)
	}
	if opts.StopWords != nil {
		t.Error("expected default stop words to be nil")
	}
	if opts.Tools != nil {
		t.Error("expected default tools to be nil")
	}
	if opts.ToolChoice != nil {
		t.Error("expected default tool choice to be nil")
	}
	if opts.ResponseFormat != nil {
		t.Error("expected default response format to be nil")
	}
}

func TestWithTemperature(t *testing.T) {
	opts := ApplyOptions(WithTemperature(0.5))
	if opts.Temperature == nil || *opts.Temperature != 0.5 {
		t.Errorf("expected temperature to be 0.5, got %v", opts.Temperature)
	}
}

func TestWithMaxTokens(t *testing.T) {
	opts := ApplyOptions(WithMaxTokens(2048))
	if opts.MaxTokens == nil || *opts.MaxTokens != 2048 {
		t.Errorf("expected max tokens to be 2048, got %v", opts.MaxTokens)
	}
}

func TestWithTopP(t *testing.T) {
	opts := ApplyOptions(WithTopP(0.9))
	if opts.TopP == nil || *opts.TopP != 0.9 {
		t.Errorf("expected top_p to be 0.9, got %v", opts.TopP)
	}
}

func TestWithFrequencyPenalty(t *testing.T) {
	opts := ApplyOptions(WithFrequencyPenalty(0.5))
	if opts.FrequencyPenalty == nil || *opts.FrequencyPenalty != 0.5 {
		t.Errorf("expected frequency penalty to be 0.5, got %v", opts.FrequencyPenalty)
	}
}

func TestWithPresencePenalty(t *testing.T) {
	opts := ApplyOptions(WithPresencePenalty(0.3))
	if opts.PresencePenalty == nil || *opts.PresencePenalty != 0.3 {
		t.Errorf("expected presence penalty to be 0.3, got %v", opts.PresencePenalty)
	}
}

func TestWithStopWords(t *testing.T) {
	stopWords := []string{testStop, "end"}
	opts := ApplyOptions(WithStopWords(stopWords))
	if len(opts.StopWords) != 2 {
		t.Errorf("expected 2 stop words, got %d", len(opts.StopWords))
	}
	if opts.StopWords[0] != testStop || opts.StopWords[1] != "end" {
		t.Errorf("unexpected stop words: %v", opts.StopWords)
	}
}

func TestApplyMultipleOptions(t *testing.T) {
	opts := ApplyOptions(
		WithTemperature(0.8),
		WithMaxTokens(4096),
		WithTopP(0.95),
		WithFrequencyPenalty(0.2),
		WithPresencePenalty(0.1),
		WithStopWords([]string{"done"}),
	)

	if opts.Temperature == nil || *opts.Temperature != 0.8 {
		t.Errorf("expected temperature 0.8, got %v", opts.Temperature)
	}
	if opts.MaxTokens == nil || *opts.MaxTokens != 4096 {
		t.Errorf("expected max tokens 4096, got %v", opts.MaxTokens)
	}
	if opts.TopP == nil || *opts.TopP != 0.95 {
		t.Errorf("expected top_p 0.95, got %v", opts.TopP)
	}
	if opts.FrequencyPenalty == nil || *opts.FrequencyPenalty != 0.2 {
		t.Errorf("expected frequency penalty 0.2, got %v", opts.FrequencyPenalty)
	}
	if opts.PresencePenalty == nil || *opts.PresencePenalty != 0.1 {
		t.Errorf("expected presence penalty 0.1, got %v", opts.PresencePenalty)
	}
	if len(opts.StopWords) != 1 || opts.StopWords[0] != "done" {
		t.Errorf("unexpected stop words: %v", opts.StopWords)
	}
}

func TestErrorVariables(t *testing.T) {
	if ErrProviderNotSupported == nil {
		t.Error("ErrProviderNotSupported should not be nil")
	}
	if ErrMissingAPIKey == nil {
		t.Error("ErrMissingAPIKey should not be nil")
	}

	if ErrProviderNotSupported.Error() != "provider not supported" {
		t.Errorf("unexpected error message: %s", ErrProviderNotSupported.Error())
	}
	if ErrMissingAPIKey.Error() != "API key is required" {
		t.Errorf("unexpected error message: %s", ErrMissingAPIKey.Error())
	}
}

func TestMessageStruct(t *testing.T) {
	msg := Message{
		Role:    RoleUser,
		Content: "Hello, world!",
	}

	if msg.Role != RoleUser {
		t.Errorf("expected role user, got %s", msg.Role)
	}
	if msg.Content != "Hello, world!" {
		t.Errorf("expected content 'Hello, world!', got %s", msg.Content)
	}
}

func TestResponseStruct(t *testing.T) {
	resp := Response{
		Content:      "Response content",
		FinishReason: testStop,
		Usage: Usage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}

	if resp.Content != "Response content" {
		t.Errorf("unexpected content: %s", resp.Content)
	}
	if resp.FinishReason != testStop {
		t.Errorf("unexpected finish reason: %s", resp.FinishReason)
	}
	if resp.Usage.PromptTokens != 10 {
		t.Errorf("expected prompt tokens 10, got %d", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 20 {
		t.Errorf("expected completion tokens 20, got %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 30 {
		t.Errorf("expected total tokens 30, got %d", resp.Usage.TotalTokens)
	}
}

func TestAsCapableProvider(t *testing.T) {
	base := NewMockLLM(WithMockCapabilities(Capabilities{Streaming: true}))
	wrapped := testWrapper{LLM: base}

	cp, ok := AsCapableProvider(wrapped)
	if !ok {
		t.Fatal("expected wrapped capable provider to be detected")
	}
	if cp == nil {
		t.Fatal("expected CapableProvider implementation")
	}
	if !cp.Capabilities().Streaming {
		t.Fatal("expected capabilities from unwrapped provider")
	}

	cp, ok = AsCapableProvider(testPlainLLM{})
	if ok {
		t.Fatal("expected non-capable LLM to return ok=false")
	}
	if cp != nil {
		t.Fatal("expected nil CapableProvider for non-capable LLM")
	}
}

func TestSupportsReasoning(t *testing.T) {
	llm := NewMockLLM(WithMockCapabilities(Capabilities{Reasoning: true}))
	if !SupportsReasoning(llm) {
		t.Fatal("expected reasoning support")
	}
	if !SupportsReasoning(testWrapper{LLM: llm}) {
		t.Fatal("expected reasoning support through wrapper")
	}
	if SupportsReasoning(NewMockLLM()) {
		t.Fatal("expected default mock to not support reasoning")
	}
	if SupportsReasoning(testPlainLLM{}) {
		t.Fatal("expected non-capable LLM to not support reasoning")
	}
}

func TestSupportsPromptCaching(t *testing.T) {
	llm := NewMockLLM(WithMockCapabilities(Capabilities{PromptCaching: true}))
	if !SupportsPromptCaching(llm) {
		t.Fatal("expected prompt caching support")
	}
	if !SupportsPromptCaching(testWrapper{LLM: llm}) {
		t.Fatal("expected prompt caching support through wrapper")
	}
	if SupportsPromptCaching(NewMockLLM()) {
		t.Fatal("expected default mock to not support prompt caching")
	}
	if SupportsPromptCaching(testPlainLLM{}) {
		t.Fatal("expected non-capable LLM to not support prompt caching")
	}
}

func TestMessageMarshal_SnakeCase(t *testing.T) {
	msg := Message{
		Role:       RoleTool,
		Content:    "tool result",
		Parts:      []ContentPart{NewTextPart("tool result")},
		ToolCalls:  []ToolCall{{ID: "call-1", Type: ToolTypeFunction}},
		ToolCallID: "call-1",
		Name:       "get_weather",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}

	assertNoJSONKeys(t, data, "Role", "Content", "Parts", "ToolCalls", "ToolCallID", "Name")
	assertHasJSONKeys(t, data, "role", "content", "parts", "tool_calls", "tool_call_id", "name")
}

func TestMessageMarshal_OmitsOptionalFields(t *testing.T) {
	data, err := json.Marshal(Message{Role: RoleUser})
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}

	assertHasJSONKeys(t, data, "role")
	assertNoJSONKeys(t, data, "content", "parts", "tool_calls", "tool_call_id", "name")
}

func TestResponseMarshal_SnakeCase(t *testing.T) {
	resp := Response{
		Content: "answer",
		Reasoning: &ReasoningContent{
			Content:  "reasoning",
			Tokens:   7,
			Metadata: map[string]any{"mode": "enabled"},
		},
		FinishReason: "stop",
		Usage: Usage{
			PromptTokens:        10,
			CompletionTokens:    5,
			TotalTokens:         15,
			CacheReadTokens:     2,
			CacheCreationTokens: 3,
		},
		ToolCalls: []ToolCall{{ID: "call-1", Type: ToolTypeFunction}},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	assertNoJSONKeys(t, data,
		"Content", "Reasoning", "Thinking", "thinking", "FinishReason", "Usage", "ToolCalls", "SearchResults",
		"PromptTokens", "CompletionTokens", "TotalTokens",
	)
	assertHasJSONKeys(t, data,
		"content", "reasoning", "finish_reason", "usage", "tool_calls",
		"prompt_tokens", "completion_tokens", "total_tokens", "cache_read_tokens", "cache_creation_tokens",
	)
}

func TestUsageMarshal_SnakeCase(t *testing.T) {
	usage := Usage{
		PromptTokens:        10,
		CompletionTokens:    5,
		TotalTokens:         15,
		CacheReadTokens:     2,
		CacheCreationTokens: 3,
	}

	data, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("marshal usage: %v", err)
	}

	assertNoJSONKeys(t, data, "PromptTokens", "CompletionTokens", "TotalTokens", "CacheReadTokens", "CacheCreationTokens")
	assertHasJSONKeys(t, data, "prompt_tokens", "completion_tokens", "total_tokens", "cache_read_tokens", "cache_creation_tokens")
}

func assertHasJSONKeys(t *testing.T, data []byte, keys ...string) {
	t.Helper()
	jsonText := string(data)
	for _, key := range keys {
		if !strings.Contains(jsonText, `"`+key+`"`) {
			t.Fatalf("expected JSON key %q in %s", key, jsonText)
		}
	}
}

func assertNoJSONKeys(t *testing.T, data []byte, keys ...string) {
	t.Helper()
	jsonText := string(data)
	for _, key := range keys {
		if strings.Contains(jsonText, `"`+key+`"`) {
			t.Fatalf("unexpected JSON key %q in %s", key, jsonText)
		}
	}
}

// Tool-related tests

func TestToolChoiceConstants(t *testing.T) {
	if ToolChoiceAuto != "auto" {
		t.Errorf("expected ToolChoiceAuto to be 'auto', got %s", ToolChoiceAuto)
	}
	if ToolChoiceNone != "none" {
		t.Errorf("expected ToolChoiceNone to be 'none', got %s", ToolChoiceNone)
	}
	if ToolChoiceRequired != "required" {
		t.Errorf("expected ToolChoiceRequired to be 'required', got %s", ToolChoiceRequired)
	}
}

func TestNewFunctionTool(t *testing.T) {
	params := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"location": map[string]any{
				"type":        "string",
				"description": "The city and state",
			},
		},
		"required": []string{"location"},
	}

	tool := NewFunctionTool("get_weather", "Get the current weather", params)

	if tool.Type != ToolTypeFunction {
		t.Errorf("expected tool type %q, got %s", toolTypeFunction, tool.Type)
	}
	if tool.Function == nil {
		t.Fatal("expected function to be set")
	}
	if tool.Function.Name != "get_weather" {
		t.Errorf("expected function name 'get_weather', got %s", tool.Function.Name)
	}
	if tool.Function.Description != "Get the current weather" {
		t.Errorf("unexpected description: %s", tool.Function.Description)
	}
	if tool.Function.Parameters == nil {
		t.Error("expected parameters to be set")
	}
}

func TestWithTools(t *testing.T) {
	tools := []Tool{
		NewFunctionTool("test_func", "A test function", nil),
	}

	opts := ApplyOptions(WithTools(tools))

	if len(opts.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(opts.Tools))
	}
	if opts.Tools[0].Function.Name != "test_func" {
		t.Errorf("unexpected tool name: %s", opts.Tools[0].Function.Name)
	}
}

func TestWithToolChoiceConstructors(t *testing.T) {
	opts := ApplyOptions(WithToolChoiceAuto())
	if opts.ToolChoice == nil || opts.ToolChoice.Type != ToolChoiceAuto {
		t.Errorf("expected tool choice 'auto', got %v", opts.ToolChoice)
	}

	opts = ApplyOptions(WithToolChoiceNone())
	if opts.ToolChoice == nil || opts.ToolChoice.Type != ToolChoiceNone {
		t.Errorf("expected tool choice 'none', got %v", opts.ToolChoice)
	}

	opts = ApplyOptions(WithToolChoiceRequired())
	if opts.ToolChoice == nil || opts.ToolChoice.Type != ToolChoiceRequired {
		t.Errorf("expected tool choice 'required', got %v", opts.ToolChoice)
	}

	opts = ApplyOptions(WithToolChoiceTool("specific_function"))
	if opts.ToolChoice == nil || opts.ToolChoice.Function == nil {
		t.Fatal("expected tool choice function reference")
	}
	if opts.ToolChoice.Type != ToolChoiceType(toolTypeFunction) {
		t.Errorf("expected tool choice type %q, got %q", toolTypeFunction, opts.ToolChoice.Type)
	}
	if opts.ToolChoice.Function.Name != "specific_function" {
		t.Errorf("unexpected function name: %s", opts.ToolChoice.Function.Name)
	}
}

func TestWithJSONMode(t *testing.T) {
	opts := ApplyOptions(WithJSONMode())
	if opts.ResponseFormat == nil {
		t.Fatal("expected response format")
	}
	if opts.ResponseFormat.Type != ResponseFormatJSONObject {
		t.Errorf("expected response format %q, got %q", ResponseFormatJSONObject, opts.ResponseFormat.Type)
	}
}

func TestToolCallStruct(t *testing.T) {
	tc := ToolCall{
		ID:   "call_123",
		Type: ToolTypeFunction,
		Function: &FunctionCall{
			Name:      "get_weather",
			Arguments: `{"location": "San Francisco"}`,
		},
	}

	if tc.ID != "call_123" {
		t.Errorf("unexpected ID: %s", tc.ID)
	}
	if tc.Type != ToolTypeFunction {
		t.Errorf("unexpected type: %s", tc.Type)
	}
	if tc.Function.Name != "get_weather" {
		t.Errorf("unexpected function name: %s", tc.Function.Name)
	}
	if tc.Function.Arguments != `{"location": "San Francisco"}` {
		t.Errorf("unexpected arguments: %s", tc.Function.Arguments)
	}
}

func TestResponseWithToolCalls(t *testing.T) {
	resp := Response{
		Content:      "",
		FinishReason: FinishReasonToolCalls,
		ToolCalls: []ToolCall{
			{
				ID:   "call_abc",
				Type: ToolTypeFunction,
				Function: &FunctionCall{
					Name:      testSearch,
					Arguments: `{"query": "test"}`,
				},
			},
		},
	}

	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Function.Name != testSearch {
		t.Errorf("unexpected function name: %s", resp.ToolCalls[0].Function.Name)
	}
}

func TestMessageWithToolFields(t *testing.T) {
	msg := Message{
		Role:       RoleTool,
		Content:    "result data",
		ToolCallID: "call_xyz",
		Name:       "my_tool",
	}

	if msg.Role != RoleTool {
		t.Errorf("expected role tool, got %s", msg.Role)
	}
	if msg.ToolCallID != "call_xyz" {
		t.Errorf("expected ToolCallID 'call_xyz', got %s", msg.ToolCallID)
	}
	if msg.Name != "my_tool" {
		t.Errorf("expected Name 'my_tool', got %s", msg.Name)
	}
}

// StreamChunk tests

func TestStreamChunkStruct(t *testing.T) {
	usage := &Usage{
		PromptTokens:     10,
		CompletionTokens: 20,
		TotalTokens:      30,
	}

	chunk := StreamChunk{
		Content:      "Hello",
		ToolCalls:    []ToolCall{{ID: "call_1", Type: "function"}},
		FinishReason: testStop,
		Usage:        usage,
		Error:        nil,
		Done:         true,
	}

	if chunk.Content != "Hello" {
		t.Errorf("expected content 'Hello', got %s", chunk.Content)
	}
	if len(chunk.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(chunk.ToolCalls))
	}
	if chunk.FinishReason != testStop {
		t.Errorf("expected finish reason 'stop', got %s", chunk.FinishReason)
	}
	if chunk.Usage == nil {
		t.Error("expected usage to be set")
	}
	if chunk.Usage.TotalTokens != 30 {
		t.Errorf("expected total tokens 30, got %d", chunk.Usage.TotalTokens)
	}
	if chunk.Error != nil {
		t.Errorf("expected no error, got %v", chunk.Error)
	}
	if !chunk.Done {
		t.Error("expected done to be true")
	}
}

func TestStreamChunkWithError(t *testing.T) {
	testErr := errors.New("test error")
	chunk := StreamChunk{
		Error: testErr,
		Done:  true,
	}

	if chunk.Error == nil {
		t.Error("expected error to be set")
	}
	if chunk.Error.Error() != "test error" {
		t.Errorf("unexpected error message: %s", chunk.Error.Error())
	}
	if !chunk.Done {
		t.Error("expected done to be true")
	}
}

func TestStreamChunkPartial(t *testing.T) {
	// Partial chunk during streaming
	chunk := StreamChunk{
		Content: "partial content",
		Done:    false,
	}

	if chunk.Content != "partial content" {
		t.Errorf("unexpected content: %s", chunk.Content)
	}
	if chunk.Done {
		t.Error("expected done to be false for partial chunk")
	}
	if chunk.Usage != nil {
		t.Error("expected usage to be nil for partial chunk")
	}
}

// WithModel tests

func TestWithModel(t *testing.T) {
	opts := ApplyOptions(WithModel("gpt-4-turbo"))
	if opts.Model != "gpt-4-turbo" {
		t.Errorf("expected model 'gpt-4-turbo', got %s", opts.Model)
	}
}

func TestWithModelOverride(t *testing.T) {
	// Test that WithModel can override in combination with other options
	opts := ApplyOptions(
		WithTemperature(0.5),
		WithModel("claude-3-opus"),
		WithMaxTokens(2048),
	)

	if opts.Model != "claude-3-opus" {
		t.Errorf("expected model 'claude-3-opus', got %s", opts.Model)
	}
	if opts.Temperature == nil || *opts.Temperature != 0.5 {
		t.Errorf("expected temperature 0.5, got %v", opts.Temperature)
	}
	if opts.MaxTokens == nil || *opts.MaxTokens != 2048 {
		t.Errorf("expected max tokens 2048, got %v", opts.MaxTokens)
	}
}

func TestApplyOptionsEmpty(t *testing.T) {
	// Applying no options should return defaults
	opts := ApplyOptions()

	if opts.Temperature != nil {
		t.Errorf("expected default temperature to be nil (unset), got %v", *opts.Temperature)
	}
	if opts.MaxTokens != nil {
		t.Errorf("expected default max tokens to be nil (unset), got %d", *opts.MaxTokens)
	}
}

func TestFunctionDefinitionStruct(t *testing.T) {
	params := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
		},
	}

	fd := FunctionDefinition{
		Name:        testSearch,
		Description: "Search for information",
		Parameters:  mustMarshalRawMessage(t, params),
		Strict:      true,
	}

	if fd.Name != testSearch {
		t.Errorf("unexpected name: %s", fd.Name)
	}
	if fd.Description != "Search for information" {
		t.Errorf("unexpected description: %s", fd.Description)
	}
	if !fd.Strict {
		t.Error("expected strict to be true")
	}
}

func TestToolStruct(t *testing.T) {
	tool := Tool{
		Type: "function",
		Function: &FunctionDefinition{
			Name:        "test",
			Description: "Test function",
		},
	}

	if tool.Type != "function" {
		t.Errorf("unexpected type: %s", tool.Type)
	}
	if tool.Function == nil {
		t.Error("expected function to be set")
	}
	if tool.Function.Name != "test" {
		t.Errorf("unexpected function name: %s", tool.Function.Name)
	}
}

func TestFunctionReferenceStruct(t *testing.T) {
	ref := FunctionReference{
		Name: "my_function",
	}

	if ref.Name != "my_function" {
		t.Errorf("unexpected name: %s", ref.Name)
	}
}

func TestToolChoiceStruct(t *testing.T) {
	choice := ToolChoice{
		Type: "function",
		Function: &FunctionReference{
			Name: "specific_func",
		},
	}

	if choice.Type != "function" {
		t.Errorf("unexpected type: %s", choice.Type)
	}
	if choice.Function == nil {
		t.Error("expected function reference to be set")
	}
	if choice.Function.Name != "specific_func" {
		t.Errorf("unexpected function name: %s", choice.Function.Name)
	}
}

func TestUsageStruct(t *testing.T) {
	usage := Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	if usage.PromptTokens != 100 {
		t.Errorf("expected prompt tokens 100, got %d", usage.PromptTokens)
	}
	if usage.CompletionTokens != 50 {
		t.Errorf("expected completion tokens 50, got %d", usage.CompletionTokens)
	}
	if usage.TotalTokens != 150 {
		t.Errorf("expected total tokens 150, got %d", usage.TotalTokens)
	}
}

func TestCallOptionsStruct(t *testing.T) {
	tools := []Tool{
		NewFunctionTool("test", "Test function", nil),
	}

	temp := 0.8
	topP := 0.95
	opts := CallOptions{
		Model:            "gpt-4",
		Temperature:      &temp,
		MaxTokens:        intPtr(2048),
		TopP:             &topP,
		FrequencyPenalty: float64Ptr(0.5),
		PresencePenalty:  float64Ptr(0.3),
		StopWords:        []string{testStop},
		Tools:            tools,
		ToolChoice:       &ToolChoice{Type: ToolChoiceAuto},
		ResponseFormat:   &ResponseFormat{Type: ResponseFormatJSONObject},
	}

	if opts.Model != "gpt-4" {
		t.Errorf("unexpected model: %s", opts.Model)
	}
	if opts.Temperature == nil || *opts.Temperature != 0.8 {
		t.Errorf("unexpected temperature: %v", opts.Temperature)
	}
	if opts.MaxTokens == nil || *opts.MaxTokens != 2048 {
		t.Errorf("unexpected max tokens: %v", opts.MaxTokens)
	}
	if opts.TopP == nil || *opts.TopP != 0.95 {
		t.Errorf("unexpected top p: %v", opts.TopP)
	}
	if opts.FrequencyPenalty == nil || *opts.FrequencyPenalty != 0.5 {
		t.Errorf("unexpected frequency penalty: %v", opts.FrequencyPenalty)
	}
	if opts.PresencePenalty == nil || *opts.PresencePenalty != 0.3 {
		t.Errorf("unexpected presence penalty: %v", opts.PresencePenalty)
	}
	if len(opts.StopWords) != 1 {
		t.Errorf("expected 1 stop word, got %d", len(opts.StopWords))
	}
	if len(opts.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(opts.Tools))
	}
	if opts.ToolChoice == nil || opts.ToolChoice.Type != ToolChoiceAuto {
		t.Errorf("unexpected tool choice: %v", opts.ToolChoice)
	}
	if opts.ResponseFormat == nil || opts.ResponseFormat.Type != ResponseFormatJSONObject {
		t.Error("expected response format to be json_object")
	}
}

func TestMessageWithToolCalls(t *testing.T) {
	msg := Message{
		Role:    RoleAssistant,
		Content: "I'll help you with that.",
		ToolCalls: []ToolCall{
			{
				ID:   "call_1",
				Type: "function",
				Function: &FunctionCall{
					Name:      testSearch,
					Arguments: `{"query": "test"}`,
				},
			},
		},
	}

	if msg.Role != RoleAssistant {
		t.Errorf("expected assistant role, got %s", msg.Role)
	}
	if len(msg.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].Function.Name != testSearch {
		t.Errorf("unexpected function name: %s", msg.ToolCalls[0].Function.Name)
	}
}
