//go:build integration

package llmadk

import (
	"context"
	"iter"
	"net/http"
	"sync"
	"testing"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// TestLive runs every scenario against the real provider APIs, skipping
// families without a key. It is the release gate for llmadk: run it with
// make integration, and with LLMADK_RECORD=1 to refresh the fixtures
// TestFixtures replays.
func TestLive(t *testing.T) {
	runScenarios(t, liveClient)
}

// TestLive_EnvelopeReachesNativeGemini checks that an agent that ran on the
// bridge can switch to ADK's own Gemini model mid-session: that model sends
// every stored ThoughtSignature to the Gemini API as it is, so the session must
// hold only Gemini's own signatures and Google's placeholder there, with other
// providers' reasoning in PartMetadata.
func TestLive_EnvelopeReachesNativeGemini(t *testing.T) {
	for _, first := range []string{"gemini", "anthropic"} {
		t.Run("from "+first, func(t *testing.T) { envelopeReachesNativeGemini(t, first) })
	}
}

func envelopeReachesNativeGemini(t *testing.T, first string) {
	if !hasKey("GEMINI_API_KEY") || !hasKey(familyKeys[first]...) {
		t.Skip("missing an API key")
	}
	ours, err := families[first].build(families[first].model, "", http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	bridged, err := NewModel(ours, WithCallOptions(llms.WithReasoningBudget(1024)))
	if err != nil {
		t.Fatal(err)
	}
	native, err := gemini.NewModel(context.Background(), families["gemini"].model, &genai.ClientConfig{APIKey: key("GEMINI_API_KEY"), Backend: genai.BackendGeminiAPI})
	if err != nil {
		t.Fatal(err)
	}
	sw := &modelSwitcher{first: bridged, rest: native}
	a, err := llmagent.New(llmagent.Config{
		Name: "weather", Model: sw, Instruction: weatherInstruction, Tools: []tool.Tool{weatherTool(t)},
		GenerateContentConfig: &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{IncludeThoughts: true, ThinkingLevel: genai.ThinkingLevelLow}},
	})
	if err != nil {
		t.Fatal(err)
	}
	events := newHarness(t, a, nil).run("What is the weather in Oslo?", agent.StreamingModeNone)
	if finalText(events) == "" {
		t.Fatalf("no answer after switching to ADK's Gemini model; events: %s", describe(events))
	}
}

// modelSwitcher serves the first call from first and the rest from rest.
type modelSwitcher struct {
	mu    sync.Mutex
	calls int
	first model.LLM
	rest  model.LLM
}

func (s *modelSwitcher) Name() string { return s.rest.Name() }

func (s *modelSwitcher) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	s.mu.Lock()
	s.calls++
	m := s.rest
	if s.calls == 1 {
		m = s.first
	}
	s.mu.Unlock()
	// ADK fills Model from the switcher's Name; each model gets its own.
	routed := *req
	routed.Model = m.Name()
	return m.GenerateContent(ctx, &routed, stream)
}
