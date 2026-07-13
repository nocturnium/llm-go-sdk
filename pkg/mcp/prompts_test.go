package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	llms "github.com/nocturnium/llm-go-sdk/v5"
)

func TestClient_ListPromptsPagination(t *testing.T) {
	m := newMockTransport()
	m.queue(methodPromptsList, listPromptsResult{
		Prompts:    []Prompt{{Name: "greet"}},
		NextCursor: "page2",
	})
	m.queue(methodPromptsList, listPromptsResult{
		Prompts: []Prompt{{Name: "summarize", Arguments: []PromptArgument{{Name: "text", Required: true}}}},
	})
	c := mustClient(t, m)

	prompts, err := c.ListPrompts(context.Background())
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	if len(prompts) != 2 || prompts[0].Name != "greet" || prompts[1].Name != "summarize" {
		t.Errorf("expected [greet summarize] across pages, got %+v", prompts)
	}
	if len(prompts[1].Arguments) != 1 || !prompts[1].Arguments[0].Required {
		t.Errorf("prompt arguments not propagated: %+v", prompts[1].Arguments)
	}
	if call, ok := m.lastCall(methodPromptsList); ok {
		var p listPromptsParams
		_ = json.Unmarshal(call.params, &p)
		if p.Cursor != "page2" {
			t.Errorf("expected cursor=page2 on second call, got %q", p.Cursor)
		}
	}
}

// A server that returns the same non-empty nextCursor forever must terminate
// quickly via the cursor-cycle guard rather than looping until ctx cancellation.
func TestClient_ListPromptsCursorCycleGuard(t *testing.T) {
	m := newMockTransport()
	m.queue(methodPromptsList, listPromptsResult{
		Prompts:    []Prompt{{Name: "greet"}},
		NextCursor: "loop",
	})
	c := mustClient(t, m)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	prompts, err := c.ListPrompts(ctx)
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("ListPrompts looped until context timeout instead of breaking on a repeated cursor")
	}
	if len(prompts) == 0 {
		t.Fatalf("expected at least one prompt, got %+v", prompts)
	}
	if calls := m.callCount(methodPromptsList); calls != 2 {
		t.Errorf("expected exactly 2 list calls before the guard trips, got %d", calls)
	}
}

func TestClient_ListPromptsContextCanceled(t *testing.T) {
	m := newMockTransport()
	c := mustClient(t, m)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.ListPrompts(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestClient_GetPrompt(t *testing.T) {
	m := newMockTransport()
	m.queue(methodPromptsGet, GetPromptResult{
		Description: "a greeting",
		Messages: []PromptMessage{
			{Role: "user", Content: ContentBlock{Type: "text", Text: "Hi {name}"}},
			{Role: "assistant", Content: ContentBlock{Type: "text", Text: "Hello there"}},
		},
	})
	c := mustClient(t, m)

	res, err := c.GetPrompt(context.Background(), "greet", map[string]string{"name": "Ada"})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if res.Description != "a greeting" || len(res.Messages) != 2 {
		t.Errorf("unexpected prompt result: %+v", res)
	}
	// The request must carry the prompt name and arguments.
	call, ok := m.lastCall(methodPromptsGet)
	if !ok {
		t.Fatal("no prompts/get recorded")
	}
	var p getPromptParams
	if err := json.Unmarshal(call.params, &p); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if p.Name != "greet" || p.Arguments["name"] != "Ada" {
		t.Errorf("unexpected get-prompt params: %+v", p)
	}
}

// A protocol-level failure on prompts/get must surface as an inspectable *RPCError.
func TestClient_GetPromptRPCError(t *testing.T) {
	m := newMockTransport()
	m.errs[methodPromptsGet] = &RPCError{Code: CodeInvalidParams, Message: "missing argument"}
	c := mustClient(t, m)

	_, err := c.GetPrompt(context.Background(), "greet", nil)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("error = %v, want *RPCError", err)
	}
	if rpcErr.Code != CodeInvalidParams {
		t.Errorf("code = %d, want %d", rpcErr.Code, CodeInvalidParams)
	}
}

// The prompt->llms.Message bridge must map roles and text content, and treat
// non-user/assistant roles as user turns so the result is valid RunTools input.
func TestGetPromptResult_LLMMessages(t *testing.T) {
	res := &GetPromptResult{Messages: []PromptMessage{
		{Role: "user", Content: ContentBlock{Type: "text", Text: "question"}},
		{Role: "assistant", Content: ContentBlock{Type: "text", Text: "answer"}},
		{Role: "user", Content: ContentBlock{Type: "image", Text: "ignored"}},
		{Role: "system", Content: ContentBlock{Type: "text", Text: "fallback"}},
	}}

	msgs := res.LLMMessages()
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(msgs))
	}
	if msgs[0].Role != llms.RoleUser || msgs[0].Content != "question" {
		t.Errorf("msg[0] = %+v, want user/question", msgs[0])
	}
	if msgs[1].Role != llms.RoleAssistant || msgs[1].Content != "answer" {
		t.Errorf("msg[1] = %+v, want assistant/answer", msgs[1])
	}
	// A non-text content block contributes no text.
	if msgs[2].Content != "" {
		t.Errorf("msg[2] content = %q, want empty (non-text block)", msgs[2].Content)
	}
	// An unexpected role maps to user so the message stays valid.
	if msgs[3].Role != llms.RoleUser || msgs[3].Content != "fallback" {
		t.Errorf("msg[3] = %+v, want user/fallback", msgs[3])
	}

	// The bridged messages must validate as RunTools input.
	if err := llms.ValidateMessages(msgs); err != nil {
		t.Errorf("bridged messages failed validation: %v", err)
	}
}

func TestGetPromptResult_LLMMessagesEmpty(t *testing.T) {
	if msgs := (*GetPromptResult)(nil).LLMMessages(); msgs != nil {
		t.Errorf("nil result: expected nil messages, got %+v", msgs)
	}
	if msgs := (&GetPromptResult{}).LLMMessages(); msgs != nil {
		t.Errorf("empty result: expected nil messages, got %+v", msgs)
	}
}
