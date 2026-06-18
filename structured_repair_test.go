package llms

import (
	"context"
	"errors"
	"testing"
)

// repairScriptLLM returns a queued sequence of responses (and optional per-call
// errors), one per GenerateContent call, so repair loops can be exercised.
type repairScriptLLM struct {
	responses []string
	errs      []error
	calls     int
	lastConvo []Message
}

func (s *repairScriptLLM) GenerateContent(_ context.Context, messages []Message, _ ...CallOption) (*Response, error) {
	i := s.calls
	s.calls++
	s.lastConvo = messages
	if i < len(s.errs) && s.errs[i] != nil {
		return nil, s.errs[i]
	}
	idx := i
	if idx >= len(s.responses) {
		idx = len(s.responses) - 1
	}
	return &Response{Content: s.responses[idx]}, nil
}

func (s *repairScriptLLM) Stream(context.Context, []Message, ...CallOption) (<-chan StreamChunk, error) {
	return nil, errors.New("not implemented")
}
func (s *repairScriptLLM) Provider() Provider { return ProviderOpenAI }
func (s *repairScriptLLM) Model() string      { return "scripted" }

type person struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"plain object", `{"name":"a","age":1}`, `{"name":"a","age":1}`, true},
		{"fenced", "```json\n{\"name\":\"a\",\"age\":1}\n```", `{"name":"a","age":1}`, true},
		{"prose around", "Sure! Here you go:\n{\"name\":\"a\"} \nHope that helps.", `{"name":"a"}`, true},
		{"array", `prefix [1,2,3] suffix`, `[1,2,3]`, true},
		{"nested", `{"a":{"b":[1,2]},"c":3}`, `{"a":{"b":[1,2]},"c":3}`, true},
		{"brace in string", `{"k":"} not the end"}`, `{"k":"} not the end"}`, true},
		{"escaped quote in string", `{"k":"a\"} still in"}`, `{"k":"a\"} still in"}`, true},
		{"no json", `just some prose`, "", false},
		{"unbalanced", `{"k": "v"`, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := extractJSON(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Errorf("extractJSON(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// GenerateTyped should recover JSON wrapped in markdown fences via local
// extraction, in a single model call.
func TestGenerateTyped_LocalExtraction(t *testing.T) {
	llm := &repairScriptLLM{responses: []string{"```json\n{\"name\":\"Ada\",\"age\":36}\n```"}}
	got, _, err := GenerateTyped[person](context.Background(), llm, []Message{{Role: RoleUser, Content: "x"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "Ada" || got.Age != 36 {
		t.Errorf("got %+v, want {Ada 36}", got)
	}
	if llm.calls != 1 {
		t.Errorf("calls = %d, want 1 (local extraction, no model retry)", llm.calls)
	}
}

// GenerateTypedWithRepair should retry on unparseable output and succeed once the
// model returns valid JSON.
func TestGenerateTypedWithRepair_Succeeds(t *testing.T) {
	llm := &repairScriptLLM{responses: []string{
		"I cannot do that.",     // attempt 1: no JSON at all
		`{"name":"Bo","age":7}`, // attempt 2: valid
	}}
	got, _, err := GenerateTypedWithRepair[person](context.Background(), llm, []Message{{Role: RoleUser, Content: "x"}}, RepairOptions{MaxRepairAttempts: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "Bo" || got.Age != 7 {
		t.Errorf("got %+v, want {Bo 7}", got)
	}
	if llm.calls != 2 {
		t.Errorf("calls = %d, want 2", llm.calls)
	}
	// The repair turn must have fed the prior bad output + a correction request back.
	if len(llm.lastConvo) < 3 {
		t.Errorf("expected repair turn appended to conversation, got %d messages", len(llm.lastConvo))
	}
}

// When every attempt fails, the error reports the attempt count and stops after
// MaxRepairAttempts+1 calls.
func TestGenerateTypedWithRepair_Exhausted(t *testing.T) {
	llm := &repairScriptLLM{responses: []string{"nope"}}
	_, _, err := GenerateTypedWithRepair[person](context.Background(), llm, []Message{{Role: RoleUser, Content: "x"}}, RepairOptions{MaxRepairAttempts: 2})
	if err == nil {
		t.Fatal("expected error after exhausting repair attempts")
	}
	if llm.calls != 3 {
		t.Errorf("calls = %d, want 3 (1 + 2 repairs)", llm.calls)
	}
}

// A transport error must surface immediately without consuming repair attempts.
func TestGenerateTypedWithRepair_TransportErrorNoRetry(t *testing.T) {
	sentinel := errors.New("boom")
	llm := &repairScriptLLM{responses: []string{"unused"}, errs: []error{sentinel}}
	_, _, err := GenerateTypedWithRepair[person](context.Background(), llm, []Message{{Role: RoleUser, Content: "x"}}, RepairOptions{MaxRepairAttempts: 3})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want transport sentinel", err)
	}
	if llm.calls != 1 {
		t.Errorf("calls = %d, want 1 (no repair on transport error)", llm.calls)
	}
}
