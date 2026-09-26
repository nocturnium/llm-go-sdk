package llmadk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/providers/openai"
)

// The scenarios below run against every provider family they list, live
// (live_test.go) and from recorded fixtures (TestFixtures). Each checks what
// holds for any correct run: the turn finishes, every tool call in the history
// the bridge sends is answered by its own result, and reasoning the provider
// needs back is replayed.

const weatherInstruction = "You answer weather questions. Always call get_weather once for every city " +
	"the user names, all in the same turn, then answer in one short sentence."

var scenarios = []scenario{
	{name: "tool_loop", families: []string{"openai-chat", "openai-responses", "anthropic", "anthropic-sonnet-5", "gemini", "openrouter", "zai"},
		run: func(t *testing.T, clients func(string) llms.LLM) {
			toolLoop(t, clients(onlyFamily(t)), agent.StreamingModeNone, nil)
		}},
	{name: "tool_loop_stream", families: []string{"openai-chat", "openai-responses", "anthropic", "gemini", "openrouter", "zai"},
		run: func(t *testing.T, clients func(string) llms.LLM) {
			toolLoop(t, clients(onlyFamily(t)), agent.StreamingModeSSE, nil)
		}},
	{name: "thinking_tools", families: []string{"openai-responses", "anthropic", "gemini"},
		run: func(t *testing.T, clients func(string) llms.LLM) {
			fam := onlyFamily(t)
			var opts []Option
			cfg := &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{IncludeThoughts: true}}
			switch fam {
			case "openai-responses":
				opts = append(opts, WithCallOptions(openai.WithReasoningRoundTrip(), llms.WithReasoningEffort(llms.ReasoningEffortLow)))
			case "anthropic":
				budget := int32(1024)
				cfg.ThinkingConfig.ThinkingBudget = &budget
			case "gemini":
				cfg.ThinkingConfig.ThinkingLevel = genai.ThinkingLevelLow
			}
			s := toolLoop(t, clients(fam), agent.StreamingModeNone, cfg, opts...)
			checkReplayedReasoning(t, s.last(), fam)
		}},
	{name: "structured_tools", families: []string{"openai-chat", "anthropic", "gemini"},
		run: func(t *testing.T, clients func(string) llms.LLM) {
			structuredTools(t, clients(onlyFamily(t)))
		}},
	{name: "switch_into_claude", families: []string{"openai-chat", "anthropic"}, multi: true,
		run: func(t *testing.T, clients func(string) llms.LLM) {
			budget := int32(1024)
			cfg := &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{ThinkingBudget: &budget}}
			events := switchMidLoop(t, clients("openai-chat"), clients("anthropic"), cfg)
			requireAdjustment(t, events, "anthropic.thinking_suspended")
		}},
	// Adaptive-thinking and always-on models are exempt from the thinking
	// suspension: these rows check that Anthropic accepts a foreign tool turn
	// for them as the bridge sends it. The first step runs on the Responses API
	// because chat completions refuse reasoning together with tools.
	{name: "switch_into_claude_adaptive", families: []string{"openai-responses", "anthropic-adaptive"}, multi: true,
		run: func(t *testing.T, clients func(string) llms.LLM) {
			cfg := &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow}}
			events := switchMidLoop(t, clients("openai-responses"), clients("anthropic-adaptive"), cfg)
			rejectAdjustment(t, events, "anthropic.thinking_suspended")
		}},
	{name: "switch_into_claude_always_on", families: []string{"openai-responses", "anthropic-always-on"}, multi: true,
		run: func(t *testing.T, clients func(string) llms.LLM) {
			cfg := &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{ThinkingLevel: genai.ThinkingLevelLow}}
			events := switchMidLoop(t, clients("openai-responses"), clients("anthropic-always-on"), cfg)
			rejectAdjustment(t, events, "anthropic.thinking_suspended")
		}},
	{name: "web_search", families: []string{"zai"},
		run: func(t *testing.T, clients func(string) llms.LLM) {
			m, err := NewModel(clients("zai"), WithWebSearch())
			if err != nil {
				t.Fatal(err)
			}
			a, err := llmagent.New(llmagent.Config{Name: "searcher", Model: m, Instruction: "Answer using web search.",
				GenerateContentConfig: &genai.GenerateContentConfig{Tools: []*genai.Tool{{GoogleSearch: &genai.GoogleSearch{}}}}})
			if err != nil {
				t.Fatal(err)
			}
			events := newHarness(t, a, nil).run("Name one recent release of the Go programming language.", agent.StreamingModeNone)
			if finalText(events) == "" {
				t.Fatalf("no answer; events: %s", describe(events))
			}
			grounded := false
			for _, ev := range events {
				if ev.GroundingMetadata != nil && len(ev.GroundingMetadata.GroundingChunks) > 0 {
					grounded = true
				}
			}
			if !grounded {
				t.Error("no event carries the search results as grounding metadata")
			}
		}},
	// A real fallback chain inside the bridge, in both directions: the
	// secondary receives the primary's reasoning in the history and must drop
	// it, and a Claude secondary with manual thinking must suspend it.
	{name: "fallback_into_claude", families: []string{"openai-responses", "anthropic"}, multi: true,
		run: func(t *testing.T, clients func(string) llms.LLM) {
			budget := int32(1024)
			cfg := &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{ThinkingBudget: &budget}}
			events := fallbackMidLoop(t, clients("openai-responses"), clients("anthropic"), cfg)
			requireAdjustment(t, events, "anthropic.thinking_suspended")
		}},
	{name: "fallback_into_openai", families: []string{"anthropic", "openai-responses"}, multi: true,
		run: func(t *testing.T, clients func(string) llms.LLM) {
			budget := int32(1024)
			cfg := &genai.GenerateContentConfig{ThinkingConfig: &genai.ThinkingConfig{ThinkingBudget: &budget}}
			fallbackMidLoop(t, clients("anthropic"), clients("openai-responses"), cfg)
		}},
	{name: "switch_into_gemini3", families: []string{"openai-chat", "gemini"}, multi: true,
		run: func(t *testing.T, clients func(string) llms.LLM) {
			// The bridge marks the foreign call with Google's placeholder itself,
			// so the Gemini provider receives it as a Gemini signature.
			s := switchMidLoopSpy(t, clients("openai-chat"), clients("gemini"), nil)
			var first *llms.ToolCall
			for _, m := range s.last() {
				if len(m.ToolCalls) > 0 {
					first = &m.ToolCalls[0]
					break
				}
			}
			want := base64.StdEncoding.EncodeToString([]byte(geminiSkipValidator))
			if first == nil || first.Signature != want || first.SignatureProvider != llms.ProviderGemini {
				t.Errorf("foreign call sent to Gemini as %+v, want the placeholder", first)
			}
		}},
}

// onlyFamily is the family of a single-family scenario run, set by the runner
// through the test name.
func onlyFamily(t *testing.T) string {
	t.Helper()
	parts := strings.Split(t.Name(), "/")
	return parts[len(parts)-1]
}

// toolLoop asks for the weather in two cities and checks the turn.
func toolLoop(t *testing.T, llm llms.LLM, mode agent.StreamingMode, cfg *genai.GenerateContentConfig, opts ...Option) *spy {
	t.Helper()
	s := &spy{LLM: llm}
	m, err := NewModel(s, opts...)
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
	events := newHarness(t, a, sqliteSessions(t)).run("What is the weather in Oslo and in Rome?", mode)
	if finalText(events) == "" {
		t.Fatalf("no final answer; events: %s", describe(events))
	}
	requireToolResults(t, s.last())
	return s
}

// requireToolResults checks that the last request carried tool results and
// that each answers its own call.
func requireToolResults(t *testing.T, messages []llms.Message) {
	t.Helper()
	results := 0
	for _, m := range messages {
		if m.Role == llms.RoleTool {
			results++
		}
	}
	if results == 0 {
		t.Fatal("the model never called get_weather")
	}
	checkToolPairs(t, messages)
}

// checkReplayedReasoning checks that the reasoning a provider needs back was
// in the request that followed the tool call.
func checkReplayedReasoning(t *testing.T, messages []llms.Message, fam string) {
	t.Helper()
	for _, m := range messages {
		if m.Role != llms.RoleAssistant || len(m.ToolCalls) == 0 {
			continue
		}
		switch fam {
		case "anthropic":
			if m.Reasoning == nil || m.Reasoning.Signature == "" {
				t.Errorf("Anthropic thinking was not replayed: %+v", m.Reasoning)
			}
		case "openai-responses":
			if m.Reasoning == nil || len(m.Reasoning.Metadata) == 0 {
				t.Errorf("OpenAI encrypted reasoning was not replayed: %+v", m.Reasoning)
			}
		case "gemini":
			if m.ToolCalls[0].Signature == "" {
				t.Errorf("Gemini's function-call signature was not replayed: %+v", m.ToolCalls[0])
			}
		}
		return
	}
	t.Error("no assistant tool call in the replayed history")
}

// structuredTools runs an agent with an output schema and a tool.
func structuredTools(t *testing.T, llm llms.LLM) {
	t.Helper()
	m, err := NewModel(llm)
	if err != nil {
		t.Fatal(err)
	}
	a, err := llmagent.New(llmagent.Config{
		Name: "reporter", Model: m, Instruction: weatherInstruction, Tools: []tool.Tool{weatherTool(t)},
		OutputSchema: &genai.Schema{Type: genai.TypeObject, Required: []string{"summary"},
			Properties: map[string]*genai.Schema{"summary": {Type: genai.TypeString}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	events := newHarness(t, a, nil).run("Report the weather in Oslo.", agent.StreamingModeNone)
	var out map[string]any
	final := finalText(events)
	if err := json.Unmarshal([]byte(final), &out); err != nil || out["summary"] == nil {
		t.Errorf("final answer %q is not the structured output: %v", final, err)
	}
}

// switcher serves the first call from before and every later one from after,
// the way an agent framework or a fallback moves a conversation between
// providers in the middle of a tool loop.
type switcher struct {
	mu     sync.Mutex
	calls  int
	before llms.LLM
	after  llms.LLM
}

func (s *switcher) pick() llms.LLM {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.calls == 1 {
		return s.before
	}
	return s.after
}

func (s *switcher) GenerateContent(ctx context.Context, m []llms.Message, o ...llms.CallOption) (*llms.Response, error) {
	return s.pick().GenerateContent(ctx, m, o...)
}

func (s *switcher) Stream(ctx context.Context, m []llms.Message, o ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	return s.pick().Stream(ctx, m, o...)
}

func (s *switcher) Provider() llms.Provider { return s.after.Provider() }
func (s *switcher) Model() string           { return s.after.Model() }

// switchMidLoop runs a tool loop whose first step is served by before and
// whose later steps are served by after.
func switchMidLoop(t *testing.T, before, after llms.LLM, cfg *genai.GenerateContentConfig) []*session.Event {
	t.Helper()
	events, _ := switchMidLoopRun(t, before, after, cfg)
	return events
}

// switchMidLoopSpy is switchMidLoop returning the spy on the requests.
func switchMidLoopSpy(t *testing.T, before, after llms.LLM, cfg *genai.GenerateContentConfig) *spy {
	t.Helper()
	_, s := switchMidLoopRun(t, before, after, cfg)
	return s
}

func switchMidLoopRun(t *testing.T, before, after llms.LLM, cfg *genai.GenerateContentConfig) ([]*session.Event, *spy) {
	t.Helper()
	sw := &switcher{before: before, after: after}
	s := &spy{LLM: sw}
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
	events := newHarness(t, a, nil).run("What is the weather in Oslo?", agent.StreamingModeNone)
	if finalText(events) == "" {
		t.Fatalf("no final answer after the switch; events: %s", describe(events))
	}
	requireToolResults(t, s.last())
	return events, s
}

// requireAdjustment checks that some event reports the named adjustment.
func requireAdjustment(t *testing.T, events []*session.Event, name string) {
	t.Helper()
	for _, ev := range events {
		if adj, ok := ev.CustomMetadata[MetadataAdjustments].([]string); ok {
			for _, a := range adj {
				if a == name {
					return
				}
			}
		}
		if adj, ok := ev.CustomMetadata[MetadataAdjustments].([]any); ok {
			for _, a := range adj {
				if a == name {
					return
				}
			}
		}
	}
	t.Errorf("no event reports adjustment %s", name)
}

// rejectAdjustment checks that no event reports the named adjustment.
func rejectAdjustment(t *testing.T, events []*session.Event, name string) {
	t.Helper()
	for _, ev := range events {
		if strings.Contains(fmt.Sprint(ev.CustomMetadata[MetadataAdjustments]), name) {
			t.Errorf("an event reports adjustment %s", name)
		}
	}
}

// describe renders events for a failure message.
func describe(events []*session.Event) string {
	var b strings.Builder
	for _, ev := range events {
		b.WriteString("[")
		if ev.ErrorCode != "" {
			b.WriteString("error " + ev.ErrorCode + ": " + ev.ErrorMessage + " ")
		}
		if ev.Content != nil {
			for _, p := range ev.Content.Parts {
				switch {
				case p.FunctionCall != nil:
					b.WriteString("call " + p.FunctionCall.Name + " ")
				case p.FunctionResponse != nil:
					b.WriteString("result " + p.FunctionResponse.Name + " ")
				case p.Text != "":
					b.WriteString(p.Text + " ")
				}
			}
		}
		b.WriteString("] ")
	}
	return b.String()
}
