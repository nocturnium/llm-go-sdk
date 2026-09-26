package llmadk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/artifact"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/database"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/adk/v2/tool/loadartifactstool"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

const (
	e2eApp     = "llmadk-e2e"
	e2eUser    = "user"
	e2eSession = "session"
)

// harness runs an ADK agent over a session service.
type harness struct {
	t         *testing.T
	sessions  session.Service
	artifacts artifact.Service
	agent     agent.Agent
}

// withArtifacts gives the harness an artifact service holding one file.
func (h *harness) withArtifacts(name string, part *genai.Part) *harness {
	h.t.Helper()
	h.artifacts = artifact.InMemoryService()
	if _, err := h.artifacts.Save(context.Background(), &artifact.SaveRequest{
		AppName: e2eApp, UserID: e2eUser, SessionID: e2eSession, FileName: name, Part: part,
	}); err != nil {
		h.t.Fatal(err)
	}
	return h
}

func newHarness(t *testing.T, a agent.Agent, sessions session.Service) *harness {
	t.Helper()
	if sessions == nil {
		sessions = session.InMemoryService()
	}
	if _, err := sessions.Create(context.Background(), &session.CreateRequest{AppName: e2eApp, UserID: e2eUser, SessionID: e2eSession}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return &harness{t: t, sessions: sessions, agent: a}
}

// run sends one user message and returns the events of the turn. A fresh
// runner is built each time, so state reaches the model only through the
// session service, which for sqlite means a store and reload.
func (h *harness) run(message string, mode agent.StreamingMode) []*session.Event {
	h.t.Helper()
	return h.runContent(genai.NewContentFromText(message, genai.RoleUser), mode)
}

// runContent sends one user turn and returns the events of the turn.
func (h *harness) runContent(content *genai.Content, mode agent.StreamingMode) []*session.Event {
	h.t.Helper()
	r, err := runner.New(runner.Config{AppName: e2eApp, Agent: h.agent, SessionService: h.sessions, ArtifactService: h.artifacts})
	if err != nil {
		h.t.Fatalf("runner: %v", err)
	}
	var events []*session.Event
	for ev, err := range r.Run(context.Background(), e2eUser, e2eSession, content, agent.RunConfig{StreamingMode: mode}) {
		if err != nil {
			h.t.Fatalf("run: %v", err)
		}
		events = append(events, ev)
	}
	return events
}

func sqliteSessions(t *testing.T) session.Service {
	t.Helper()
	svc, err := database.NewSessionService(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())),
		&gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(svc); err != nil {
		t.Fatal(err)
	}
	return svc
}

type weatherArgs struct {
	City string `json:"city"`
}

func weatherTool(t *testing.T) tool.Tool {
	t.Helper()
	w, err := functiontool.New(functiontool.Config{Name: "get_weather", Description: "Weather for a city."},
		func(_ agent.Context, a weatherArgs) (map[string]any, error) {
			return map[string]any{"city": a.City, "temp": len(a.City)}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func newAgent(t *testing.T, llm llms.LLM, cfg llmagent.Config) agent.Agent {
	t.Helper()
	m, err := NewModel(llm)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name == "" {
		cfg.Name = "assistant"
	}
	cfg.Model = m
	a, err := llmagent.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func finalText(events []*session.Event) string {
	var out string
	for _, ev := range events {
		if ev.Partial || ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p.Text != "" && !p.Thought {
				out = p.Text
			}
		}
	}
	return out
}

// checkToolPairs asserts that every tool call in messages is answered by the
// tool message right after it, and that the answer is for that call's city:
// the property ADK's ID-keyed pairing breaks when IDs repeat.
func checkToolPairs(t *testing.T, messages []llms.Message) {
	t.Helper()
	for i, msg := range messages {
		if msg.Role != llms.RoleAssistant {
			continue
		}
		for j, tc := range msg.ToolCalls {
			if i+1+j >= len(messages) {
				t.Fatalf("call %s has no result", tc.ID)
			}
			res := messages[i+1+j]
			if res.Role != llms.RoleTool || res.ToolCallID != tc.ID {
				t.Fatalf("call %s answered by %+v", tc.ID, res)
			}
			var args, result map[string]any
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			if err := json.Unmarshal([]byte(res.Content), &result); err != nil {
				t.Fatalf("result %q: %v", res.Content, err)
			}
			if args["city"] != result["city"] {
				t.Errorf("call %s for %v paired with the result for %v", tc.ID, args["city"], result["city"])
			}
		}
	}
}

func TestE2E_ToolLoop(t *testing.T) {
	for _, mode := range []agent.StreamingMode{agent.StreamingModeNone, agent.StreamingModeSSE} {
		t.Run(string(mode), func(t *testing.T) {
			fake := newFake(
				&llms.Response{ToolCalls: []llms.ToolCall{toolCall("", "get_weather", `{"city":"Paris"}`)}, FinishReason: llms.FinishReasonToolCalls},
				&llms.Response{Content: "It is mild in Paris.", FinishReason: llms.FinishReasonStop},
			)
			h := newHarness(t, newAgent(t, fake, llmagent.Config{Instruction: "Be helpful.", Tools: []tool.Tool{weatherTool(t)}}), nil)
			events := h.run("Weather in Paris?", mode)
			if got := finalText(events); got != "It is mild in Paris." {
				t.Errorf("final text = %q", got)
			}
			if fake.callCount() != 2 {
				t.Fatalf("model calls = %d", fake.callCount())
			}
			second := fake.lastCall()
			if second.messages[0].Role != llms.RoleSystem || !strings.Contains(second.messages[0].Content, "Be helpful.") {
				t.Errorf("system = %+v", second.messages[0])
			}
			checkToolPairs(t, second.messages)
			if len(second.opts.Tools) != 1 || second.opts.Tools[0].Function.Name != "get_weather" {
				t.Errorf("tools = %+v", second.opts.Tools)
			}
			if mode == agent.StreamingModeSSE {
				partial := 0
				for _, ev := range events {
					if ev.Partial {
						partial++
					}
				}
				if partial == 0 {
					t.Error("no partial events in SSE mode")
				}
			}
		})
	}
}

// TestE2E_SameToolAcrossTurns runs the scenario ADK's whole-session pairing
// breaks on when IDs repeat: the same tool called in parallel and in three
// consecutive turns, by a provider that returns no IDs, with the history stored
// in sqlite and reloaded for every turn.
func TestE2E_SameToolAcrossTurns(t *testing.T) {
	// The provider returns no IDs (Gemini before 3), or the same long ID every
	// turn (an OpenAI-compatible server counting from zero per response).
	for name, providerID := range map[string]func(i int) string{
		"no IDs":       func(int) string { return "" },
		"repeated IDs": func(i int) string { return fmt.Sprintf("call_%08d", i) },
	} {
		t.Run(name, func(t *testing.T) { sameToolAcrossTurns(t, providerID) })
	}
}

func sameToolAcrossTurns(t *testing.T, providerID func(i int) string) {
	cities := [][]string{{"Oslo", "Rome"}, {"Lima"}, {"Kyiv"}}
	var script []*llms.Response
	for _, turn := range cities {
		var calls []llms.ToolCall
		for i, c := range turn {
			calls = append(calls, toolCall(providerID(i), "get_weather", fmt.Sprintf(`{"city":%q}`, c)))
		}
		script = append(script,
			&llms.Response{ToolCalls: calls, FinishReason: llms.FinishReasonToolCalls},
			&llms.Response{Content: "done " + strings.Join(turn, ","), FinishReason: llms.FinishReasonStop})
	}
	script = append(script, &llms.Response{Content: "summary", FinishReason: llms.FinishReasonStop})
	fake := newFake(script...)
	h := newHarness(t, newAgent(t, fake, llmagent.Config{Tools: []tool.Tool{weatherTool(t)}}), sqliteSessions(t))
	for i := range cities {
		h.run(fmt.Sprintf("turn %d", i), agent.StreamingModeNone)
	}
	h.run("summarize", agent.StreamingModeNone)

	last := fake.lastCall().messages
	checkToolPairs(t, last)
	ids := map[string]bool{}
	calls := 0
	for _, msg := range last {
		for _, tc := range msg.ToolCalls {
			calls++
			if ids[tc.ID] || strings.HasPrefix(tc.ID, "adk-") {
				t.Errorf("history repeats or carries an ADK ID: %q", tc.ID)
			}
			ids[tc.ID] = true
		}
	}
	if calls != 4 {
		t.Errorf("history holds %d calls, want 4", calls)
	}
}

// TestE2E_ReasoningSurvivesSQLite checks that a provider's reasoning, hidden
// from the events, comes back to the same provider on the next step intact:
// text, signature, metadata and stamp, through a sqlite store and reload.
func TestE2E_ReasoningSurvivesSQLite(t *testing.T) {
	rc := &llms.ReasoningContent{
		Content: "private thought", Signature: "sig-123",
		Metadata: map[string]any{"redacted_thinking": []any{"blob"}},
	}
	fake := newFake(
		&llms.Response{Reasoning: rc, ToolCalls: []llms.ToolCall{toolCall("toolu_0123456789", "get_weather", `{"city":"Bern"}`)}, FinishReason: llms.FinishReasonToolCalls},
		&llms.Response{Content: "ok", FinishReason: llms.FinishReasonStop},
	)
	h := newHarness(t, newAgent(t, fake, llmagent.Config{Tools: []tool.Tool{weatherTool(t)}}), sqliteSessions(t))
	events := h.run("go", agent.StreamingModeNone)
	for _, ev := range events {
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if strings.Contains(p.Text, "private thought") {
				t.Error("hidden thought text shown in an event")
			}
		}
	}
	var replayed *llms.ReasoningContent
	for _, msg := range fake.lastCall().messages {
		if msg.Reasoning != nil {
			replayed = msg.Reasoning
		}
	}
	if replayed == nil || replayed.Content != "private thought" || replayed.Signature != "sig-123" ||
		replayed.Provider != llms.ProviderAnthropic || len(redacted(replayed)) != 1 {
		t.Errorf("replayed reasoning = %+v", replayed)
	}
}

func redacted(rc *llms.ReasoningContent) []any {
	v, _ := rc.Metadata["redacted_thinking"].([]any)
	return v
}

// TestE2E_StructuredOutputWithTools covers an agent with both an output
// schema and tools, which ADK sends together for non-Gemini models.
func TestE2E_StructuredOutputWithTools(t *testing.T) {
	fake := newFake(
		&llms.Response{ToolCalls: []llms.ToolCall{toolCall("", "get_weather", `{"city":"Nice"}`)}, FinishReason: llms.FinishReasonToolCalls},
		&llms.Response{ToolCalls: []llms.ToolCall{toolCall("", setModelResponseName, `{"summary":"warm"}`)}, FinishReason: llms.FinishReasonToolCalls},
	)
	schema := &genai.Schema{Type: genai.TypeObject, Properties: map[string]*genai.Schema{"summary": {Type: genai.TypeString}}, Required: []string{"summary"}}
	h := newHarness(t, newAgent(t, fake, llmagent.Config{Tools: []tool.Tool{weatherTool(t)}, OutputSchema: schema, OutputKey: "report"}), nil)
	events := h.run("Report on Nice", agent.StreamingModeNone)
	if got := finalText(events); got != `{"summary":"warm"}` {
		t.Errorf("final text = %q", got)
	}
	first := fake.calls[0]
	if first.opts.ResponseFormat != nil {
		t.Error("the schema was passed down alongside the tools")
	}
	var names []string
	for _, tool := range first.opts.Tools {
		names = append(names, tool.Function.Name)
	}
	if strings.Join(names, ",") != "get_weather,"+setModelResponseName {
		t.Errorf("tools offered = %v", names)
	}
	sess, err := h.sessions.Get(context.Background(), &session.GetRequest{AppName: e2eApp, UserID: e2eUser, SessionID: e2eSession})
	if err != nil {
		t.Fatal(err)
	}
	if v, err := sess.Session.State().Get("report"); err != nil || v == nil {
		t.Errorf("output key report = %v (%v)", v, err)
	}
}

// TestE2E_AgentTransfer covers a root agent handing off to a sub-agent served
// by a different provider, with the root's thought in the shared history.
func TestE2E_AgentTransfer(t *testing.T) {
	rootLLM := newFake(&llms.Response{
		Reasoning:    &llms.ReasoningContent{Content: "route it", Signature: "root-sig"},
		ToolCalls:    []llms.ToolCall{toolCall("", "transfer_to_agent", `{"agent_name":"expert"}`)},
		FinishReason: llms.FinishReasonToolCalls,
	})
	expertLLM := newFake(&llms.Response{Content: "expert answer", FinishReason: llms.FinishReasonStop})
	expertLLM.provider = llms.ProviderOpenAI
	expert := newAgent(t, expertLLM, llmagent.Config{Name: "expert", Description: "Answers hard questions."})
	root := newAgent(t, rootLLM, llmagent.Config{Name: "root", Instruction: "Route questions.", SubAgents: []agent.Agent{expert}})
	events := newHarness(t, root, nil).run("hard question", agent.StreamingModeNone)
	if got := finalText(events); got != "expert answer" {
		t.Errorf("final text = %q", got)
	}
	for _, msg := range expertLLM.lastCall().messages {
		if msg.Reasoning != nil && msg.Reasoning.Signature == "root-sig" {
			t.Error("the root provider's signed reasoning reached the expert's request as replayable reasoning")
		}
	}
}

// TestE2E_ToolsBridge runs registry tools inside ADK, including a failing one.
func TestE2E_ToolsBridge(t *testing.T) {
	reg := llms.NewToolRegistry()
	reg.Register(llms.NewFunctionTool("add", "Adds.", map[string]any{
		"type": "object", "properties": map[string]any{"a": map[string]any{"type": "number"}, "b": map[string]any{"type": "number"}},
	}), func(_ context.Context, raw json.RawMessage) (any, error) {
		var in struct{ A, B float64 }
		if err := json.Unmarshal(raw, &in); err != nil {
			return nil, err
		}
		return in.A + in.B, nil
	})
	reg.Register(llms.NewFunctionTool("fail", "Fails.", map[string]any{"type": "object"}),
		func(context.Context, json.RawMessage) (any, error) { return nil, errors.New("boom") })
	tools, err := Tools(reg)
	if err != nil {
		t.Fatal(err)
	}
	fake := newFake(
		&llms.Response{ToolCalls: []llms.ToolCall{toolCall("", "add", `{"a":2,"b":3}`), toolCall("", "fail", `{}`)}, FinishReason: llms.FinishReasonToolCalls},
		&llms.Response{Content: "5", FinishReason: llms.FinishReasonStop},
	)
	newHarness(t, newAgent(t, fake, llmagent.Config{Tools: tools}), nil).run("add", agent.StreamingModeNone)
	var results []string
	for _, msg := range fake.lastCall().messages {
		if msg.Role == llms.RoleTool {
			results = append(results, msg.Content)
		}
	}
	if len(results) != 2 || results[0] != `{"result":5}` || !strings.Contains(results[1], "boom") {
		t.Errorf("tool results = %v", results)
	}
}

// TestE2E_LongRunningToolLeavesCallPending covers a long-running tool, for
// which ADK records no result until the operation finishes. The next request
// still answers the call, with the pending placeholder, which OpenAI- and
// Anthropic-style APIs require.
func TestE2E_LongRunningToolLeavesCallPending(t *testing.T) {
	job, err := functiontool.New(functiontool.Config{Name: "start_job", Description: "Starts a job.", IsLongRunning: true},
		func(agent.Context, map[string]any) (map[string]any, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	fake := newFake(
		&llms.Response{ToolCalls: []llms.ToolCall{toolCall("", "start_job", `{}`)}, FinishReason: llms.FinishReasonToolCalls},
		&llms.Response{Content: "started", FinishReason: llms.FinishReasonStop},
		&llms.Response{Content: "still running", FinishReason: llms.FinishReasonStop},
	)
	h := newHarness(t, newAgent(t, fake, llmagent.Config{Tools: []tool.Tool{job}}), nil)
	h.run("start the job", agent.StreamingModeNone)
	h.run("is it done?", agent.StreamingModeNone)
	last := fake.lastCall().messages
	var call string
	for i, m := range last {
		if len(m.ToolCalls) > 0 {
			call = m.ToolCalls[0].ID
			if i+1 >= len(last) || last[i+1].Role != llms.RoleTool || last[i+1].ToolCallID != call {
				t.Fatalf("the long-running call is not answered: %+v", last)
			}
			if last[i+1].Content != pendingResult {
				t.Errorf("result = %q, want the pending placeholder", last[i+1].Content)
			}
		}
	}
	if call == "" {
		t.Fatal("the long-running call is missing from the history")
	}
}

// TestE2E_LoadArtifactsText covers ADK's load_artifacts tool, which attaches a
// stored file to the request as inline data; text files reach the model as text.
func TestE2E_LoadArtifactsText(t *testing.T) {
	fake := newFake(
		&llms.Response{ToolCalls: []llms.ToolCall{toolCall("", "load_artifacts", `{"artifact_names":["notes.txt"]}`)}, FinishReason: llms.FinishReasonToolCalls},
		&llms.Response{Content: "read it", FinishReason: llms.FinishReasonStop},
	)
	h := newHarness(t, newAgent(t, fake, llmagent.Config{Tools: []tool.Tool{loadartifactstool.New()}}), nil).
		withArtifacts("notes.txt", &genai.Part{InlineData: &genai.Blob{MIMEType: "text/plain", Data: []byte("the launch code is 42")}})
	h.run("read my notes", agent.StreamingModeNone)
	found := false
	for _, m := range fake.lastCall().messages {
		if m.Role == llms.RoleUser && strings.Contains(m.Content+fmt.Sprint(m.Parts), "the launch code is 42") {
			found = true
		}
	}
	if !found {
		t.Errorf("artifact text not in the request: %+v", fake.lastCall().messages)
	}
}

// TestE2E_ToolConfirmation covers ADK's human-in-the-loop flow: the tool asks
// for confirmation, the user confirms in a later turn, and the model then sees
// the tool's real result, not a pending one, and none of ADK's confirmation
// bookkeeping.
func TestE2E_ToolConfirmation(t *testing.T) {
	transfer, err := functiontool.New(functiontool.Config{Name: "transfer", Description: "Moves money.", RequireConfirmation: true},
		func(agent.Context, map[string]any) (map[string]any, error) {
			return map[string]any{"status": "sent"}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	fake := newFake(
		&llms.Response{ToolCalls: []llms.ToolCall{toolCall("", "transfer", `{"amount":5}`)}, FinishReason: llms.FinishReasonToolCalls},
		&llms.Response{Content: "transferred", FinishReason: llms.FinishReasonStop},
	)
	h := newHarness(t, newAgent(t, fake, llmagent.Config{Tools: []tool.Tool{transfer}}), nil)
	var confirmID string
	for _, ev := range h.run("send 5", agent.StreamingModeNone) {
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p.FunctionCall != nil && p.FunctionCall.Name == toolconfirmation.FunctionCallName {
				confirmID = p.FunctionCall.ID
			}
		}
	}
	if confirmID == "" {
		t.Fatal("ADK did not ask for confirmation")
	}
	events := h.runContent(&genai.Content{Role: string(genai.RoleUser), Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
		Name: toolconfirmation.FunctionCallName, ID: confirmID, Response: map[string]any{"confirmed": true},
	}}}}, agent.StreamingModeNone)
	if got := finalText(events); got != "transferred" {
		t.Fatalf("final text = %q; events: %s", got, describe(events))
	}
	last := fake.lastCall().messages
	for i, m := range last {
		for _, tc := range m.ToolCalls {
			if tc.Function.Name == toolconfirmation.FunctionCallName {
				t.Errorf("confirmation bookkeeping reached the model: %+v", tc)
			}
			if tc.Function.Name == "transfer" && (i+1 >= len(last) || last[i+1].Content != `{"status":"sent"}`) {
				t.Errorf("transfer call answered with %+v, want its real result", last[i+1:])
			}
		}
	}
}

// TestE2E_LongRunningToolCompletes covers a long-running tool whose result the
// application reports later: that result replaces the pending placeholder.
func TestE2E_LongRunningToolCompletes(t *testing.T) {
	job, err := functiontool.New(functiontool.Config{Name: "start_job", Description: "Starts a job.", IsLongRunning: true},
		func(agent.Context, map[string]any) (map[string]any, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	fake := newFake(
		&llms.Response{ToolCalls: []llms.ToolCall{toolCall("", "start_job", `{}`)}, FinishReason: llms.FinishReasonToolCalls},
		&llms.Response{Content: "started", FinishReason: llms.FinishReasonStop},
		&llms.Response{Content: "it finished", FinishReason: llms.FinishReasonStop},
	)
	h := newHarness(t, newAgent(t, fake, llmagent.Config{Tools: []tool.Tool{job}}), nil)
	var callID string
	for _, ev := range h.run("start the job", agent.StreamingModeNone) {
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p.FunctionCall != nil && p.FunctionCall.Name == "start_job" {
				callID = p.FunctionCall.ID
			}
		}
	}
	h.runContent(&genai.Content{Role: string(genai.RoleUser), Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
		Name: "start_job", ID: callID, Response: map[string]any{"status": "done"},
	}}}}, agent.StreamingModeNone)
	last := fake.lastCall().messages
	for i, m := range last {
		if len(m.ToolCalls) > 0 && m.ToolCalls[0].ID == callID {
			if i+1 >= len(last) || last[i+1].Content != `{"status":"done"}` {
				t.Errorf("completed job answered with %+v, want its result", last[i+1:])
			}
			return
		}
	}
	t.Errorf("job call %q missing from the history: %+v", callID, last)
}

// TestE2E_ThoughtOnlyTurnCallsOnce covers a turn that is only hidden thought
// (thinking that ran out of tokens): ADK re-calls the model after a thought-only
// response, so the bridge must not emit one.
func TestE2E_ThoughtOnlyTurnCallsOnce(t *testing.T) {
	fake := newFake(&llms.Response{Reasoning: &llms.ReasoningContent{Content: "thinking...", Signature: "s"}, FinishReason: llms.FinishReasonLength})
	newHarness(t, newAgent(t, fake, llmagent.Config{}), nil).run("think hard", agent.StreamingModeNone)
	if fake.callCount() != 1 {
		t.Errorf("model calls = %d, want 1", fake.callCount())
	}
}
