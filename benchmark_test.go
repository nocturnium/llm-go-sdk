package llms

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func BenchmarkMessageConversion(b *testing.B) {
	messages := []Message{
		{Role: RoleSystem, Content: "You are a helpful assistant."},
		{Role: RoleUser, Content: "Hello, how are you today?"},
		{Role: RoleAssistant, Content: "I'm doing well, thank you for asking!"},
		{Role: RoleUser, Content: "Can you help me with a coding problem?"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate message conversion (JSON marshal/unmarshal)
		data, _ := json.Marshal(messages)
		var result []Message
		_ = json.Unmarshal(data, &result)
	}
}

func BenchmarkMessageConversion_Large(b *testing.B) {
	// Simulate a conversation with many messages
	messages := make([]Message, 100)
	for i := 0; i < 100; i++ {
		if i%2 == 0 {
			messages[i] = Message{
				Role:    RoleUser,
				Content: "This is a user message with some content that simulates a real conversation.",
			}
		} else {
			messages[i] = Message{
				Role:    RoleAssistant,
				Content: "This is an assistant response with detailed information and explanations.",
			}
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _ := json.Marshal(messages)
		var result []Message
		_ = json.Unmarshal(data, &result)
	}
}

func BenchmarkToolDefinition(b *testing.B) {
	tool := NewFunctionTool("get_weather", "Get weather for a location", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"location": map[string]any{
				"type":        "string",
				"description": "The city and state, e.g. San Francisco, CA",
			},
			"unit": map[string]any{
				"type": "string",
				"enum": []string{"celsius", "fahrenheit"},
			},
		},
		"required": []string{"location"},
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _ := json.Marshal(tool)
		var result Tool
		_ = json.Unmarshal(data, &result)
	}
}

func BenchmarkParseToolArguments(b *testing.B) {
	type WeatherArgs struct {
		Location string `json:"location"`
		Unit     string `json:"unit"`
	}

	tc := ToolCall{
		ID:   "call_123",
		Type: "function",
		Function: &FunctionCall{
			Name:      "get_weather",
			Arguments: `{"location": "San Francisco, CA", "unit": "celsius"}`,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseToolArguments[WeatherArgs](tc)
	}
}

func BenchmarkParseToolArgumentsMap(b *testing.B) {
	tc := ToolCall{
		ID:   "call_123",
		Type: "function",
		Function: &FunctionCall{
			Name:      "get_weather",
			Arguments: `{"location": "San Francisco, CA", "unit": "celsius", "details": {"humidity": 65, "wind_speed": 10}}`,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseToolArgumentsMap(tc)
	}
}

func BenchmarkToolResult(b *testing.B) {
	result := map[string]any{
		"temperature": 72,
		"condition":   "sunny",
		"humidity":    45,
		"wind_speed":  10,
		"forecast": []map[string]any{
			{"day": "Monday", "high": 75, "low": 60},
			{"day": "Tuesday", "high": 78, "low": 62},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ToolResult("call_123", "get_weather", result)
	}
}

func BenchmarkToolResultString(b *testing.B) {
	result := "The weather in San Francisco is 72°F and sunny with 45% humidity."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ToolResultString("call_123", "get_weather", result)
	}
}

func BenchmarkToolRegistry_Handle(b *testing.B) {
	registry := NewToolRegistry()

	weatherTool := NewFunctionTool("get_weather", "Get weather", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"location": map[string]any{"type": "string"},
		},
	})

	registry.Register(weatherTool, func(_ context.Context, args json.RawMessage) (any, error) {
		var parsed struct {
			Location string `json:"location"`
		}
		_ = json.Unmarshal(args, &parsed)
		return map[string]any{"temperature": 72, "location": parsed.Location}, nil
	})

	tc := ToolCall{
		ID:       "call_123",
		Function: &FunctionCall{Name: "get_weather", Arguments: `{"location": "Boston"}`},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = registry.Handle(context.Background(), tc)
	}
}

func BenchmarkCallOptionsValidate(b *testing.B) {
	opts := &CallOptions{
		Temperature:      float64Ptr(0.7),
		MaxTokens:        intPtr(1000),
		TopP:             float64Ptr(0.9),
		FrequencyPenalty: float64Ptr(0.5),
		PresencePenalty:  float64Ptr(0.5),
		Tools: []Tool{
			NewFunctionTool("test", "Test function", nil),
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = opts.Validate()
	}
}

func BenchmarkValidateMessages(b *testing.B) {
	messages := []Message{
		{Role: RoleSystem, Content: "You are helpful."},
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleAssistant, Content: "Hi there!"},
		{Role: RoleUser, Content: "How are you?"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateMessages(messages)
	}
}

func BenchmarkApplyOptions(b *testing.B) {
	options := []CallOption{
		WithTemperature(0.7),
		WithMaxTokens(1000),
		WithTopP(0.9),
		WithFrequencyPenalty(0.5),
		WithPresencePenalty(0.5),
		WithStopWords([]string{"END", "STOP"}),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ApplyOptions(options...)
	}
}

func BenchmarkResponse_HasToolCalls(b *testing.B) {
	resp := &Response{
		Content: "Here's the result",
		ToolCalls: []ToolCall{
			{ID: "call_1", Function: &FunctionCall{Name: "func1"}},
			{ID: "call_2", Function: &FunctionCall{Name: "func2"}},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = resp.HasToolCalls()
	}
}

func BenchmarkResponse_ToolCall(b *testing.B) {
	resp := &Response{
		ToolCalls: []ToolCall{
			{ID: "call_1", Function: &FunctionCall{Name: "func1"}},
			{ID: "call_2", Function: &FunctionCall{Name: "func2"}},
			{ID: "call_3", Function: &FunctionCall{Name: "func3"}},
			{ID: "call_4", Function: &FunctionCall{Name: "func4"}},
			{ID: "call_5", Function: &FunctionCall{Name: "func5"}},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = resp.ToolCall("call_3")
	}
}

func BenchmarkResponse_ToolCallNames(b *testing.B) {
	resp := &Response{
		ToolCalls: []ToolCall{
			{ID: "call_1", Function: &FunctionCall{Name: "func1"}},
			{ID: "call_2", Function: &FunctionCall{Name: "func2"}},
			{ID: "call_3", Function: &FunctionCall{Name: "func3"}},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = resp.ToolCallNames()
	}
}

func BenchmarkNewFunctionTool(b *testing.B) {
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

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewFunctionTool("get_weather", "Get weather information", params)
	}
}

// Benchmark StreamSender - hot path for streaming responses
func BenchmarkStreamSender_Send(b *testing.B) {
	ctx := context.Background()
	chunks := make(chan StreamChunk, 100)
	sender := NewStreamSender(ctx, chunks, 5*time.Second)

	// Drain channel in background
	go func() {
		for range chunks {
			_ = struct{}{} // Drain channel
		}
	}()

	chunk := StreamChunk{Content: "Hello, this is a test chunk of content."}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sender.Send(chunk)
	}
}

func BenchmarkStreamSender_SendParallel(b *testing.B) {
	ctx := context.Background()
	chunks := make(chan StreamChunk, 1000)
	sender := NewStreamSender(ctx, chunks, 5*time.Second)

	// Drain channel in background
	go func() {
		for range chunks {
			_ = struct{}{} // Drain channel
		}
	}()

	chunk := StreamChunk{Content: "Hello, this is a test chunk of content."}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			sender.Send(chunk)
		}
	})
}

// Benchmark CostTracker - hot path for usage tracking
func BenchmarkCostTracker_Record(b *testing.B) {
	tracker := NewCostTracker()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tracker.Record(ProviderOpenAI, "gpt-4", Usage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		})
	}
}

func BenchmarkCostTracker_RecordParallel(b *testing.B) {
	tracker := NewCostTracker()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			tracker.Record(ProviderOpenAI, "gpt-4", Usage{
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
			})
		}
	})
}

func BenchmarkCostTracker_GetTotalCost(b *testing.B) {
	tracker := NewCostTracker()
	// Pre-populate with some data
	for i := 0; i < 100; i++ {
		tracker.Record(ProviderOpenAI, "gpt-4", Usage{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tracker.GetTotalCost()
	}
}

// Benchmark LogEntry creation (common operation in logging middleware)
func BenchmarkLogEntry_Creation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = &LogEntry{
			RequestID: "req_12345678",
			Timestamp: time.Now(),
			Provider:  ProviderOpenAI,
			Model:     "gpt-4",
			Operation: "GenerateContent",
		}
	}
}

// Benchmark ToolRegistry - hot path for tool handling
func BenchmarkToolRegistry_HandleParallel(b *testing.B) {
	registry := NewToolRegistry()

	weatherTool := NewFunctionTool("get_weather", "Get weather", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"location": map[string]any{"type": "string"},
		},
	})

	registry.Register(weatherTool, func(_ context.Context, _ json.RawMessage) (any, error) {
		return map[string]any{"temperature": 72}, nil
	})

	tc := ToolCall{
		ID:       "call_123",
		Function: &FunctionCall{Name: "get_weather", Arguments: `{"location": "Boston"}`},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = registry.Handle(context.Background(), tc)
		}
	})
}

// Benchmark ApplyOptions with many options - hot path for request building
func BenchmarkApplyOptions_Full(b *testing.B) {
	tools := []Tool{
		NewFunctionTool("func1", "Description 1", nil),
		NewFunctionTool("func2", "Description 2", nil),
		NewFunctionTool("func3", "Description 3", nil),
	}

	options := []CallOption{
		WithTemperature(0.7),
		WithMaxTokens(1000),
		WithTopP(0.9),
		WithFrequencyPenalty(0.5),
		WithPresencePenalty(0.5),
		WithStopWords([]string{"END", "STOP", "DONE"}),
		WithTools(tools),
		WithModel("gpt-4"),
		WithJSONMode(),
		WithStreamBufferSize(100),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ApplyOptions(options...)
	}
}

// Benchmark ValidateMessages with large conversation
func BenchmarkValidateMessages_Large(b *testing.B) {
	messages := make([]Message, 50)
	for i := 0; i < 50; i++ {
		switch {
		case i == 0:
			messages[i] = Message{Role: RoleSystem, Content: "System prompt"}
		case i%2 == 1:
			messages[i] = Message{Role: RoleUser, Content: "User message " + string(rune('A'+i))}
		default:
			messages[i] = Message{Role: RoleAssistant, Content: "Assistant response"}
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateMessages(messages)
	}
}
