package llms

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

const (
	testGetWeather = "get_weather"
	testCall2      = "call_2"
)

func TestParseToolArguments(t *testing.T) {
	type WeatherArgs struct {
		Location string `json:"location"`
		Unit     string `json:"unit"`
	}

	t.Run("valid arguments", func(t *testing.T) {
		tc := ToolCall{
			ID:   "call_123",
			Type: "function",
			Function: &FunctionCall{
				Name:      testGetWeather,
				Arguments: `{"location": "Boston", "unit": "celsius"}`,
			},
		}

		args, err := ParseToolArguments[WeatherArgs](tc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if args.Location != "Boston" {
			t.Errorf("expected location=Boston, got %s", args.Location)
		}
		if args.Unit != "celsius" {
			t.Errorf("expected unit=celsius, got %s", args.Unit)
		}
	})

	t.Run("no function", func(t *testing.T) {
		tc := ToolCall{ID: "call_123"}

		_, err := ParseToolArguments[WeatherArgs](tc)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, ErrToolCallFailed) {
			t.Errorf("expected ErrToolCallFailed, got %v", err)
		}
	})

	t.Run("empty arguments", func(t *testing.T) {
		tc := ToolCall{
			ID:       "call_123",
			Function: &FunctionCall{Name: "test", Arguments: ""},
		}

		_, err := ParseToolArguments[WeatherArgs](tc)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		tc := ToolCall{
			ID:       "call_123",
			Function: &FunctionCall{Name: "test", Arguments: "not json"},
		}

		_, err := ParseToolArguments[WeatherArgs](tc)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, ErrInvalidToolResponse) {
			t.Errorf("expected ErrInvalidToolResponse, got %v", err)
		}
	})
}

func TestParseToolArgumentsMap(t *testing.T) {
	tc := ToolCall{
		Function: &FunctionCall{
			Name:      "test",
			Arguments: `{"key": "value", "number": 42}`,
		},
	}

	args, err := ParseToolArgumentsMap(tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if args["key"] != "value" {
		t.Errorf("expected key=value, got %v", args["key"])
	}
	if args["number"] != float64(42) {
		t.Errorf("expected number=42, got %v", args["number"])
	}
}

func TestToolResult(t *testing.T) {
	t.Run("string result", func(t *testing.T) {
		msg, err := ToolResult("call_123", testGetWeather, "sunny")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if msg.Role != RoleTool {
			t.Errorf("expected role=tool, got %s", msg.Role)
		}
		if msg.ToolCallID != "call_123" {
			t.Errorf("expected tool_call_id=call_123, got %s", msg.ToolCallID)
		}
		if msg.Name != testGetWeather {
			t.Errorf("expected name=get_weather, got %s", msg.Name)
		}
		if msg.Content != "sunny" {
			t.Errorf("expected content=sunny, got %s", msg.Content)
		}
	})

	t.Run("map result", func(t *testing.T) {
		result := map[string]any{"temperature": 72, "condition": "sunny"}
		msg, err := ToolResult("call_123", testGetWeather, result)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var parsed map[string]any
		if err := json.Unmarshal([]byte(msg.Content), &parsed); err != nil {
			t.Fatalf("content is not valid JSON: %v", err)
		}

		if parsed["temperature"] != float64(72) {
			t.Errorf("unexpected temperature: %v", parsed["temperature"])
		}
	})

	t.Run("bytes result", func(t *testing.T) {
		msg, err := ToolResult("call_123", "test", []byte("raw bytes"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if msg.Content != "raw bytes" {
			t.Errorf("expected content='raw bytes', got %s", msg.Content)
		}
	})
}

func TestToolResultString(t *testing.T) {
	msg := ToolResultString("call_123", "test", "result value")

	if msg.Role != RoleTool {
		t.Errorf("expected role=tool, got %s", msg.Role)
	}
	if msg.Content != "result value" {
		t.Errorf("expected content='result value', got %s", msg.Content)
	}
}

func TestToolResultError(t *testing.T) {
	msg := ToolResultError("call_123", "test", errors.New("something went wrong"))

	if msg.Role != RoleTool {
		t.Errorf("expected role=tool, got %s", msg.Role)
	}
	if msg.Content != "Error: something went wrong" {
		t.Errorf("unexpected content: %s", msg.Content)
	}
}

func TestResponse_HasToolCalls(t *testing.T) {
	t.Run("with tool calls", func(t *testing.T) {
		resp := &Response{
			ToolCalls: []ToolCall{{ID: "call_123"}},
		}
		if !resp.HasToolCalls() {
			t.Error("expected HasToolCalls to return true")
		}
	})

	t.Run("without tool calls", func(t *testing.T) {
		resp := &Response{}
		if resp.HasToolCalls() {
			t.Error("expected HasToolCalls to return false")
		}
	})
}

func TestResponse_ToolCall(t *testing.T) {
	resp := &Response{
		ToolCalls: []ToolCall{
			{ID: "call_1"},
			{ID: testCall2},
			{ID: "call_3"},
		},
	}

	t.Run("found", func(t *testing.T) {
		tc := resp.ToolCall(testCall2)
		if tc == nil {
			t.Fatal("expected tool call, got nil")
		}
		if tc.ID != testCall2 {
			t.Errorf("expected id=call_2, got %s", tc.ID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		tc := resp.ToolCall("call_999")
		if tc != nil {
			t.Errorf("expected nil, got %v", tc)
		}
	})
}

func TestResponse_ToolCallByName(t *testing.T) {
	resp := &Response{
		ToolCalls: []ToolCall{
			{ID: "call_1", Function: &FunctionCall{Name: "func_a"}},
			{ID: testCall2, Function: &FunctionCall{Name: "func_b"}},
		},
	}

	t.Run("found", func(t *testing.T) {
		tc := resp.ToolCallByName("func_b")
		if tc == nil {
			t.Fatal("expected tool call, got nil")
		}
		if tc.ID != testCall2 {
			t.Errorf("expected id=call_2, got %s", tc.ID)
		}
	})

	t.Run("not found", func(t *testing.T) {
		tc := resp.ToolCallByName("func_c")
		if tc != nil {
			t.Errorf("expected nil, got %v", tc)
		}
	})
}

func TestResponse_ToolCallNames(t *testing.T) {
	resp := &Response{
		ToolCalls: []ToolCall{
			{Function: &FunctionCall{Name: "func_a"}},
			{Function: &FunctionCall{Name: "func_b"}},
			{Function: nil}, // No function
		},
	}

	names := resp.ToolCallNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
	if names[0] != "func_a" || names[1] != "func_b" {
		t.Errorf("unexpected names: %v", names)
	}
}

func TestToolRegistry(t *testing.T) {
	registry := NewToolRegistry()

	weatherTool := NewFunctionTool(testGetWeather, "Get weather", map[string]any{
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
		return map[string]any{
			"location":    parsed.Location,
			"temperature": 72,
		}, nil
	})

	t.Run("Tools returns registered tools", func(t *testing.T) {
		tools := registry.Tools()
		if len(tools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(tools))
		}
		if tools[0].Function.Name != testGetWeather {
			t.Errorf("expected name=get_weather, got %s", tools[0].Function.Name)
		}
	})

	t.Run("Handle executes handler", func(t *testing.T) {
		tc := ToolCall{
			ID:       "call_123",
			Function: &FunctionCall{Name: testGetWeather, Arguments: `{"location": "Boston"}`},
		}

		msg, err := registry.Handle(context.Background(), tc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if msg.Role != RoleTool {
			t.Errorf("expected role=tool, got %s", msg.Role)
		}

		var result map[string]any
		_ = json.Unmarshal([]byte(msg.Content), &result)
		if result["location"] != "Boston" {
			t.Errorf("expected location=Boston, got %v", result["location"])
		}
	})

	t.Run("Handle returns error for unknown tool", func(t *testing.T) {
		tc := ToolCall{
			ID:       "call_123",
			Function: &FunctionCall{Name: "unknown_tool", Arguments: "{}"},
		}

		_, err := registry.Handle(context.Background(), tc)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, ErrToolNotFound) {
			t.Errorf("expected ErrToolNotFound, got %v", err)
		}
	})
}

func TestToolRegistry_HandleAll(t *testing.T) {
	registry := NewToolRegistry()

	registry.Register(
		NewFunctionTool("tool_a", "Tool A", nil),
		func(_ context.Context, _ json.RawMessage) (any, error) {
			return "result_a", nil
		},
	)

	registry.Register(
		NewFunctionTool("tool_b", "Tool B", nil),
		func(_ context.Context, _ json.RawMessage) (any, error) {
			return "result_b", nil
		},
	)

	resp := &Response{
		ToolCalls: []ToolCall{
			{ID: "call_1", Function: &FunctionCall{Name: "tool_a", Arguments: "{}"}},
			{ID: testCall2, Function: &FunctionCall{Name: "tool_b", Arguments: "{}"}},
		},
	}

	messages, err := registry.HandleAll(context.Background(), resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	if messages[0].ToolCallID != "call_1" {
		t.Errorf("expected tool_call_id=call_1, got %s", messages[0].ToolCallID)
	}
	if messages[1].ToolCallID != testCall2 {
		t.Errorf("expected tool_call_id=call_2, got %s", messages[1].ToolCallID)
	}
}

func TestToolRegistry_ContextPassedToHandlers(t *testing.T) {
	type contextKey string
	const key contextKey = "request-id"

	type Args struct {
		Value int `json:"value"`
	}

	ctx := context.WithValue(context.Background(), key, "ctx-123")
	registry := NewToolRegistry()
	RegisterFunc(registry, NewFunctionTool("typed", "Typed tool", nil), func(ctx context.Context, args Args) (any, error) {
		if got := ctx.Value(key); got != "ctx-123" {
			t.Fatalf("context value = %v, want ctx-123", got)
		}
		return args.Value * 3, nil
	})
	registry.Register(NewFunctionTool("raw", "Raw tool", nil), func(ctx context.Context, _ json.RawMessage) (any, error) {
		if got := ctx.Value(key); got != "ctx-123" {
			t.Fatalf("context value = %v, want ctx-123", got)
		}
		return "raw-ok", nil
	})

	msg, err := registry.Handle(ctx, ToolCall{
		ID:       "call_typed",
		Function: &FunctionCall{Name: "typed", Arguments: `{"value": 7}`},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if msg.Content != "21" {
		t.Fatalf("Handle content = %q, want 21", msg.Content)
	}

	messages, err := registry.HandleAll(ctx, &Response{ToolCalls: []ToolCall{
		{ID: "call_raw", Function: &FunctionCall{Name: "raw", Arguments: `{}`}},
	}})
	if err != nil {
		t.Fatalf("HandleAll: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "raw-ok" {
		t.Fatalf("HandleAll messages = %+v, want raw-ok", messages)
	}
}

func TestRegisterFunc(t *testing.T) {
	type Args struct {
		Value int `json:"value"`
	}

	registry := NewToolRegistry()
	tool := NewFunctionTool("double", "Double a value", nil)

	RegisterFunc(registry, tool, func(_ context.Context, args Args) (any, error) {
		return args.Value * 2, nil
	})

	tc := ToolCall{
		ID:       "call_123",
		Function: &FunctionCall{Name: "double", Arguments: `{"value": 21}`},
	}

	msg, err := registry.Handle(context.Background(), tc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if msg.Content != "42" {
		t.Errorf("expected content=42, got %s", msg.Content)
	}
}
