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
		// A balanced but non-JSON bracket group (prose in brackets) must not stop
		// the scan before the real object later in the string.
		{"prose bracket before object", `Here is [the JSON] {"name":"Ada"}`, `{"name":"Ada"}`, true},
		{"prose brace before array", `note {see below} [1,2]`, `[1,2]`, true},
		{"only prose brackets", `Here is [the JSON]`, "", false},
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

// A valid but unrelated JSON value before the real object must not stop the
// parser: GenerateTyped needs the candidate that matches its schema.
func TestGenerateTyped_SkipsUnrelatedJSONValue(t *testing.T) {
	llm := &repairScriptLLM{responses: []string{`prefix [1] then {"name":"Ada","age":36}`}}
	got, _, err := GenerateTyped[person](context.Background(), llm, []Message{{Role: RoleUser, Content: "x"}})
	if err != nil {
		t.Fatalf("GenerateTyped: %v", err)
	}
	if got.Name != "Ada" || got.Age != 36 {
		t.Errorf("got %+v, want {Ada 36}", got)
	}
}

// An unrelated object must not satisfy a struct with its zero value; the later
// object that actually names the struct's fields wins.
func TestGenerateTyped_SkipsUnrelatedObject(t *testing.T) {
	llm := &repairScriptLLM{responses: []string{`prefix {"noise":1} then {"name":"Ada","age":36}`}}
	got, _, err := GenerateTyped[person](context.Background(), llm, []Message{{Role: RoleUser, Content: "x"}})
	if err != nil {
		t.Fatalf("GenerateTyped: %v", err)
	}
	if got.Name != "Ada" || got.Age != 36 {
		t.Errorf("got %+v, want {Ada 36}", got)
	}
}

// A direct object that names none of the struct's exported JSON fields is not a
// valid structured response, even though json.Unmarshal would leave the struct
// at its zero value.
func TestParseTyped_DirectObjectWithoutTargetFields(t *testing.T) {
	for _, content := range []string{`{"noise":1}`, `{}`} {
		if _, err := parseTyped[person](content); err == nil {
			t.Errorf("parseTyped(%s) succeeded, want an error", content)
		}
	}
}

// Unexported fields are invisible to encoding/json, so an object naming only
// one of them does not match the schema.
func TestParseTyped_UnexportedFieldDoesNotMatch(t *testing.T) {
	type hidden struct {
		//lint:ignore U1000 the field must stay unexported to exercise schema matching
		secret struct{ Value int }
	}
	if _, err := parseTyped[hidden](`{"Value":1}`); err == nil {
		t.Fatal("parseTyped matched an unexported-only field, want an error")
	}
}

// An unbalanced object must not yield one of its keys as a string result.
func TestParseTyped_UnbalancedObjectDoesNotYieldKey(t *testing.T) {
	if got, err := parseTyped[string](`{"name":"Ada"`); err == nil {
		t.Fatalf("parseTyped returned %q, want an error for malformed JSON", got)
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
