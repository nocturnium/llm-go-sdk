package llmadk

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

func simpleRequest() *model.LLMRequest {
	return &model.LLMRequest{Contents: []*genai.Content{user(text("q"))}, Config: &genai.GenerateContentConfig{
		ThinkingConfig: &genai.ThinkingConfig{IncludeThoughts: true},
	}}
}

func streamedResponse() *llms.Response {
	return &llms.Response{
		Content:      "one two three",
		Reasoning:    &llms.ReasoningContent{Content: "hmm", Signature: "sig"},
		ToolCalls:    []llms.ToolCall{toolCall("toolu_0123456789", "f", `{"a":1}`)},
		FinishReason: llms.FinishReasonToolCalls,
		Usage:        llms.Usage{PromptTokens: 3, CompletionTokens: 4},
	}
}

// TestStream_PartialsThenOneFinal checks ADK's streaming contract: partial
// responses carry deltas only, and exactly one complete response ends the turn
// with everything ADK acts on. It must equal the unary result.
func TestStream_PartialsThenOneFinal(t *testing.T) {
	fake := newFake(streamedResponse(), streamedResponse())
	m, err := NewModel(fake)
	if err != nil {
		t.Fatal(err)
	}
	var partials []*model.LLMResponse
	var finals []*model.LLMResponse
	for resp, err := range m.GenerateContent(context.Background(), simpleRequest(), true) {
		if err != nil {
			t.Fatal(err)
		}
		if resp.Partial {
			partials = append(partials, resp)
			for _, p := range resp.Content.Parts {
				if p.FunctionCall != nil {
					t.Error("a partial response carries a function call")
				}
			}
		} else {
			finals = append(finals, resp)
		}
	}
	if len(partials) != 4 || len(finals) != 1 {
		t.Fatalf("partials=%d finals=%d, want 4 and 1", len(partials), len(finals))
	}
	if !partials[0].Content.Parts[0].Thought || partials[1].Content.Parts[0].Text != "one " {
		t.Errorf("partials = %+v %+v", partials[0].Content.Parts[0], partials[1].Content.Parts[0])
	}

	var unary *model.LLMResponse
	for resp, err := range m.GenerateContent(context.Background(), simpleRequest(), false) {
		if err != nil {
			t.Fatal(err)
		}
		unary = resp
	}
	final := finals[0]
	if len(final.Content.Parts) != len(unary.Content.Parts) || final.FinishReason != unary.FinishReason ||
		final.UsageMetadata.TotalTokenCount != unary.UsageMetadata.TotalTokenCount {
		t.Errorf("stream final differs from unary:\nstream %+v\nunary  %+v", final, unary)
	}
	for i := range final.Content.Parts {
		f, u := final.Content.Parts[i], unary.Content.Parts[i]
		if f.Text != u.Text || f.Thought != u.Thought || (f.FunctionCall == nil) != (u.FunctionCall == nil) {
			t.Errorf("part %d: stream %+v unary %+v", i, f, u)
		}
	}
	if final.CustomMetadata[MetadataProvider] != "anthropic" {
		t.Errorf("stream final lost the served identity: %+v", final.CustomMetadata)
	}
}

// TestStream_EarlyBreak checks that a consumer that stops ranging never gets
// another yield and that the provider's stream goroutine still exits.
func TestStream_EarlyBreak(t *testing.T) {
	fake := newFake(streamedResponse())
	fake.streamDone = make(chan struct{})
	m, _ := NewModel(fake)
	calls := 0
	for range m.GenerateContent(context.Background(), simpleRequest(), true) {
		calls++
		break // a yield after this would panic in the range-over-func runtime
	}
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
	select {
	case <-fake.streamDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the provider's stream goroutine did not exit after the consumer stopped")
	}
}

func TestStream_NoTerminalChunk(t *testing.T) {
	fake := newFake(streamedResponse())
	fake.noTerminal = true
	m, _ := NewModel(fake)
	var lastErr error
	for _, err := range m.GenerateContent(context.Background(), simpleRequest(), true) {
		if err != nil {
			lastErr = err
		}
	}
	if !errors.Is(lastErr, ErrIncompleteStream) || !errors.Is(lastErr, llms.ErrStreamInterrupted) {
		t.Errorf("err = %v, want ErrIncompleteStream", lastErr)
	}
}

func TestStream_ContextCanceled(t *testing.T) {
	m, _ := NewModel(newFake(streamedResponse()))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var lastErr error
	for _, err := range m.GenerateContent(ctx, simpleRequest(), true) {
		if err != nil {
			lastErr = err
		}
	}
	if !errors.Is(lastErr, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", lastErr)
	}
}

func TestGenerateContent_RerangeCallsAgain(t *testing.T) {
	fake := newFake()
	m, _ := NewModel(fake)
	seq := m.GenerateContent(context.Background(), simpleRequest(), false)
	responses := 0
	for range seq {
		responses++
	}
	for range seq {
		responses++
	}
	if responses != 2 {
		t.Errorf("responses = %d, want one per range", responses)
	}
	if fake.callCount() != 2 {
		t.Errorf("calls = %d, want one per range", fake.callCount())
	}
}

func TestGenerateContent_WrapsProviderErrors(t *testing.T) {
	m, _ := NewModel(errLLM{newFake()})
	for _, err := range m.GenerateContent(context.Background(), simpleRequest(), false) {
		if !errors.Is(err, llms.ErrRateLimited) {
			t.Errorf("err = %v, want it to match llms.ErrRateLimited", err)
		}
	}
}

type errLLM struct{ *fakeLLM }

func (errLLM) GenerateContent(context.Context, []llms.Message, ...llms.CallOption) (*llms.Response, error) {
	return nil, llms.ErrRateLimited
}

func TestCapabilityChecks(t *testing.T) {
	fake := newFake()
	fake.caps = &llms.Capabilities{}
	req := &model.LLMRequest{Contents: []*genai.Content{user(text("q"))}, Config: &genai.GenerateContentConfig{
		Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{Name: "f"}}}},
	}}
	strict, _ := NewModel(fake, WithCapabilityChecks())
	for _, err := range strict.GenerateContent(context.Background(), req, false) {
		if !errors.Is(err, ErrCapability) {
			t.Errorf("with checks: %v", err)
		}
	}
	lenient, _ := NewModel(fake)
	for _, err := range lenient.GenerateContent(context.Background(), req, false) {
		if err != nil {
			t.Errorf("checks are opt-in: %v", err)
		}
	}
}

// stuckLLM streams one chunk and then neither sends nor closes, as a
// third-party provider that ignores cancellation might.
type stuckLLM struct{ *fakeLLM }

func (stuckLLM) Stream(context.Context, []llms.Message, ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	ch := make(chan llms.StreamChunk, 1)
	ch <- llms.StreamChunk{Content: "partial "}
	return ch, nil
}

// TestStream_CancelMidStream checks that canceling the caller's context while
// a stream is in flight ends the turn with the context's error, even when the
// provider never closes its channel.
func TestStream_CancelMidStream(t *testing.T) {
	m, _ := NewModel(stuckLLM{newFake()})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		var last error
		for resp, err := range m.GenerateContent(ctx, simpleRequest(), true) {
			if resp != nil && resp.Partial {
				cancel()
			}
			if err != nil {
				last = err
			}
		}
		done <- last
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not end after cancellation")
	}
}
