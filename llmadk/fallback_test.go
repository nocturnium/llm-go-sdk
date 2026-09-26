package llmadk

import (
	"context"
	"sync"
	"testing"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/middleware/resilience"
)

// failAfter serves the first n calls from the wrapped LLM and answers every
// later one with a 503, which a fallback chain treats as a reason to move on.
type failAfter struct {
	llms.LLM
	mu    sync.Mutex
	n     int
	calls int
}

func (f *failAfter) fail() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.calls > f.n
}

func (f *failAfter) GenerateContent(ctx context.Context, m []llms.Message, o ...llms.CallOption) (*llms.Response, error) {
	if f.fail() {
		return nil, &llms.APIError{StatusCode: 503, Message: "primary down"}
	}
	return f.LLM.GenerateContent(ctx, m, o...)
}

func (f *failAfter) Stream(ctx context.Context, m []llms.Message, o ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	if f.fail() {
		return nil, &llms.APIError{StatusCode: 503, Message: "primary down"}
	}
	return f.LLM.Stream(ctx, m, o...)
}

// fallbackMidLoop runs a tool loop through a FallbackChain whose primary serves
// the first step and then fails, so the secondary serves the rest with the
// primary's reasoning in the history.
func fallbackMidLoop(t *testing.T, primary, secondary llms.LLM, cfg *genai.GenerateContentConfig) []*session.Event {
	t.Helper()
	chain := resilience.NewFallbackChain([]llms.LLM{&failAfter{LLM: primary, n: 1}, secondary})
	s := &spy{LLM: chain}
	m, err := NewModel(s)
	if err != nil {
		t.Fatal(err)
	}
	a, err := llmagent.New(llmagent.Config{
		Name: "weather", Model: m, Instruction: weatherInstruction,
		Tools: []tool.Tool{weatherTool(t)}, GenerateContentConfig: cfg,
	})
	if err != nil {
		t.Fatal(err)
	}
	events := newHarness(t, a, sqliteSessions(t)).run("What is the weather in Oslo?", agent.StreamingModeNone)
	if finalText(events) == "" {
		t.Fatalf("no final answer after falling back; events: %s", describe(events))
	}
	requireToolResults(t, s.last())
	return events
}

// unstampedLLM returns reasoning without a provenance stamp, as a third-party
// llms.LLM would.
type unstampedLLM struct{ *fakeLLM }

func (u unstampedLLM) GenerateContent(_ context.Context, m []llms.Message, o ...llms.CallOption) (*llms.Response, error) {
	return u.next(m, o), nil
}

// TestFallbackChain_LeavesUnstampedReasoningUnstamped covers the bridge's
// router rule: behind a fallback chain the serving entry is unknown, so
// reasoning a provider left unstamped is not stamped with entry 0's name, which
// would send it back to the wrong provider.
func TestFallbackChain_LeavesUnstampedReasoningUnstamped(t *testing.T) {
	inner := newFake(
		&llms.Response{Reasoning: &llms.ReasoningContent{Content: "r", Signature: "s"},
			ToolCalls: []llms.ToolCall{toolCall("toolu_0123456789", "get_weather", `{"city":"Oslo"}`)}, FinishReason: llms.FinishReasonToolCalls},
		&llms.Response{Content: "ok", FinishReason: llms.FinishReasonStop},
	)
	chain := resilience.NewFallbackChain([]llms.LLM{unstampedLLM{inner}})
	h := newHarness(t, newAgent(t, chain, llmagent.Config{Tools: []tool.Tool{weatherTool(t)}}), nil)
	h.run("go", agent.StreamingModeNone)
	for _, m := range inner.lastCall().messages {
		if m.Reasoning != nil && m.Reasoning.Provider != "" {
			t.Errorf("reasoning behind a fallback chain was stamped %q", m.Reasoning.Provider)
		}
	}
}
