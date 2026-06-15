package llms

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type scriptedLLM struct {
	mu          sync.Mutex
	responses   []*Response
	always      *Response
	calls       int
	lastOptions *CallOptions
}

func (m *scriptedLLM) Call(context.Context, string, ...CallOption) (string, error) {
	return "", nil
}

func (m *scriptedLLM) GenerateContent(ctx context.Context, _ []Message, options ...CallOption) (*Response, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls++
	m.lastOptions = ApplyOptions(options...)
	if m.always != nil {
		return m.always, nil
	}
	if m.calls > len(m.responses) {
		return &Response{}, nil
	}
	return m.responses[m.calls-1], nil
}

func (m *scriptedLLM) Stream(context.Context, []Message, ...CallOption) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk)
	close(ch)
	return ch, nil
}

func (m *scriptedLLM) Provider() Provider {
	return ProviderOpenAI
}

func (m *scriptedLLM) Model() string {
	return "scripted-model"
}

func (m *scriptedLLM) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func TestRunToolsExecutesToolsAndReturnsFinalTranscript(t *testing.T) {
	firstToolCall := ToolCall{
		ID:   "call-slow",
		Type: "function",
		Function: &FunctionCall{
			Name:      "slow_tool",
			Arguments: `{}`,
		},
	}
	secondToolCall := ToolCall{
		ID:   "call-fast",
		Type: "function",
		Function: &FunctionCall{
			Name:      "fast_tool",
			Arguments: `{}`,
		},
	}
	finalAnswer := "tools completed"

	llm := &scriptedLLM{
		responses: []*Response{
			{
				Content:   "calling tools",
				ToolCalls: []ToolCall{firstToolCall, secondToolCall},
			},
			{Content: finalAnswer},
		},
	}

	var executed int32
	registry := NewToolRegistry()
	registry.Register(NewFunctionTool("slow_tool", "Slow tool", nil), func(_ json.RawMessage) (any, error) {
		atomic.AddInt32(&executed, 1)
		time.Sleep(10 * time.Millisecond)
		return "slow-result", nil
	})
	registry.Register(NewFunctionTool("fast_tool", "Fast tool", nil), func(_ json.RawMessage) (any, error) {
		atomic.AddInt32(&executed, 1)
		return "fast-result", nil
	})

	steps := make([]int, 0, 2)
	messages := make([]Message, 1, 4)
	messages[0] = Message{Role: RoleUser, Content: "run tools"}

	resp, transcript, err := RunTools(
		context.Background(),
		llm,
		messages,
		registry,
		WithToolConcurrency(2),
		WithOnStep(func(iteration int, _ *Response) {
			steps = append(steps, iteration)
		}),
	)
	if err != nil {
		t.Fatalf("RunTools() error = %v", err)
	}
	if resp == nil || resp.Content != finalAnswer {
		t.Fatalf("RunTools() response content = %q, want %q", responseContent(resp), finalAnswer)
	}
	if atomic.LoadInt32(&executed) != 2 {
		t.Fatalf("executed tools = %d, want 2", executed)
	}
	if llm.callCount() != 2 {
		t.Fatalf("GenerateContent calls = %d, want 2", llm.callCount())
	}
	if len(steps) != 2 || steps[0] != 1 || steps[1] != 2 {
		t.Fatalf("onStep iterations = %v, want [1 2]", steps)
	}
	if llm.lastOptions == nil || len(llm.lastOptions.Tools) != 2 {
		t.Fatalf("GenerateContent received %d tools, want 2", len(llm.lastOptions.Tools))
	}

	extendedMessages := messages[:cap(messages)]
	if extendedMessages[1].Role != "" {
		t.Fatalf("RunTools mutated caller backing array at index 1: %+v", extendedMessages[1])
	}

	if len(transcript) != 5 {
		t.Fatalf("transcript length = %d, want 5: %+v", len(transcript), transcript)
	}
	if transcript[0].Role != messages[0].Role || transcript[0].Content != messages[0].Content {
		t.Fatalf("transcript[0] = %+v, want original message %+v", transcript[0], messages[0])
	}
	assistantWithTools := transcript[1]
	if assistantWithTools.Role != RoleAssistant || assistantWithTools.Content != "calling tools" || len(assistantWithTools.ToolCalls) != 2 {
		t.Fatalf("assistant tool message = %+v, want assistant with two tool calls", assistantWithTools)
	}
	if transcript[2].Role != RoleTool || transcript[2].ToolCallID != "call-slow" || transcript[2].Name != "slow_tool" || transcript[2].Content != "slow-result" {
		t.Fatalf("first tool result = %+v, want slow result in first call order", transcript[2])
	}
	if transcript[3].Role != RoleTool || transcript[3].ToolCallID != "call-fast" || transcript[3].Name != "fast_tool" || transcript[3].Content != "fast-result" {
		t.Fatalf("second tool result = %+v, want fast result in second call order", transcript[3])
	}
	if transcript[4].Role != RoleAssistant || transcript[4].Content != finalAnswer || len(transcript[4].ToolCalls) != 0 {
		t.Fatalf("final assistant message = %+v, want final answer", transcript[4])
	}
}

func TestRunToolsMaxIterations(t *testing.T) {
	llm := &scriptedLLM{
		always: &Response{
			Content: "still calling",
			ToolCalls: []ToolCall{{
				ID:   "call-repeat",
				Type: "function",
				Function: &FunctionCall{
					Name:      "repeat_tool",
					Arguments: `{}`,
				},
			}},
		},
	}
	registry := NewToolRegistry()
	registry.Register(NewFunctionTool("repeat_tool", "Repeat tool", nil), func(_ json.RawMessage) (any, error) {
		return "repeat-result", nil
	})

	resp, transcript, err := RunTools(context.Background(), llm, []Message{{Role: RoleUser, Content: "loop"}}, registry, WithMaxIterations(3))
	if !errors.Is(err, ErrMaxIterations) {
		t.Fatalf("RunTools() error = %v, want ErrMaxIterations", err)
	}
	if resp == nil {
		t.Fatal("RunTools() response is nil, want last response")
	}
	if len(transcript) == 0 {
		t.Fatal("RunTools() transcript is empty, want partial transcript")
	}
	if llm.callCount() != 3 {
		t.Fatalf("GenerateContent calls = %d, want 3", llm.callCount())
	}
}

func TestRunToolsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	llm := &scriptedLLM{
		responses: []*Response{{Content: "not reached"}},
	}
	registry := NewToolRegistry()

	resp, transcript, err := RunTools(ctx, llm, []Message{{Role: RoleUser, Content: "cancel"}}, registry)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunTools() error = %v, want context.Canceled", err)
	}
	if resp != nil {
		t.Fatalf("RunTools() response = %+v, want nil", resp)
	}
	if len(transcript) != 1 {
		t.Fatalf("transcript length = %d, want original message only", len(transcript))
	}
	if llm.callCount() != 0 {
		t.Fatalf("GenerateContent calls = %d, want 0", llm.callCount())
	}
}

func responseContent(resp *Response) string {
	if resp == nil {
		return ""
	}
	return resp.Content
}

// TestRunTools_ForwardsCallOptions covers WithCallOptions (#3): forwarded call
// options reach each model turn, and registry tools still win the Tools field.
func TestRunTools_ForwardsCallOptions(t *testing.T) {
	llm := &scriptedLLM{responses: []*Response{{Content: "done"}}}
	registry := NewToolRegistry()
	registry.Register(NewFunctionTool("noop", "noop", nil), func(_ json.RawMessage) (any, error) { return "ok", nil })
	otherTool := NewFunctionTool("other", "other", nil)

	_, _, err := RunTools(
		context.Background(), llm,
		[]Message{{Role: RoleUser, Content: "hi"}},
		registry,
		WithCallOptions(WithTemperature(0.5), WithMaxTokens(123), WithTools([]Tool{otherTool})),
	)
	if err != nil {
		t.Fatalf("RunTools() error = %v", err)
	}
	if llm.lastOptions == nil {
		t.Fatal("no call options captured")
	}
	if llm.lastOptions.Temperature == nil || *llm.lastOptions.Temperature != 0.5 {
		t.Errorf("temperature not forwarded: %v", llm.lastOptions.Temperature)
	}
	if llm.lastOptions.MaxTokens == nil || *llm.lastOptions.MaxTokens != 123 {
		t.Errorf("max tokens not forwarded: %v", llm.lastOptions.MaxTokens)
	}
	if len(llm.lastOptions.Tools) != 1 || llm.lastOptions.Tools[0].Function == nil || llm.lastOptions.Tools[0].Function.Name != "noop" {
		t.Errorf("registry tools should win precedence over forwarded WithTools, got %+v", llm.lastOptions.Tools)
	}
}
