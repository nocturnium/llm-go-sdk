package decisionwire

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// liveJev is the answers object of a live typesafe/jev-1.14 call on OpenRouter, 2026-11-14.
const liveJev = `{
 "urgent": {"type": "noul", "noul": 0.96},
 "dept": {"type": "choice", "choice": "billing", "probabilities": {"billing": 0.97, "tech": 0.03}, "confidence": 0.94},
 "anger": {"type": "score", "score": 2, "legend": {"0": "calm", "1": "annoyed", "2": "furious"}, "probabilities": {"0": 0, "1": 0, "2": 1}, "confidence": 1}
}`

var allKinds = []llms.DecisionKind{llms.DecisionKindNoul, llms.DecisionKindChoice, llms.DecisionKindScore, llms.DecisionKindBox}

func parse(t *testing.T, answers string, qs map[string]llms.DecisionQuestion) map[string]llms.DecisionAnswer {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(answers), &raw); err != nil {
		t.Fatal(err)
	}
	out, err := ParseAnswers(raw, qs)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSystemOneMapping_Answers(t *testing.T) {
	got := parse(t, liveJev, nil)

	noul := got["urgent"]
	if noul.Kind != llms.DecisionKindNoul || noul.Probability == nil || *noul.Probability != 0.96 || noul.Null {
		t.Errorf("noul: %+v", noul)
	}
	choice := got["dept"]
	if choice.Kind != llms.DecisionKindChoice || choice.Choice != "billing" || choice.Probabilities["tech"] != 0.03 || *choice.Confidence != 0.94 {
		t.Errorf("choice: %+v", choice)
	}
	score := got["anger"]
	if score.Kind != llms.DecisionKindScore || *score.Score != 2 || score.Legend["2"] != "furious" || score.Probabilities["2"] != 1 {
		t.Errorf("score: %+v", score)
	}
	// A probability of exactly 0 must survive, not read as absent.
	if p, ok := score.Probabilities["0"]; !ok || p != 0 {
		t.Errorf("zero probability lost: %+v", score.Probabilities)
	}
	for name, a := range got {
		if len(a.Raw) == 0 || !json.Valid(a.Raw) {
			t.Errorf("%s: Raw not kept", name)
		}
	}
}

func TestSystemOneMapping_Predicate(t *testing.T) {
	got := parse(t, `{"q":{"type":"predicate","noul":0.5}}`, nil)
	if got["q"].Kind != llms.DecisionKindNoul || *got["q"].Probability != 0.5 {
		t.Fatalf("predicate not normalised to noul: %+v", got["q"])
	}
}

// TestSystemOneMapping_Refusal: the spec's refusal carries only "type". Setting
// Null with Refused, and taking Kind from the question, is the SDK's mapping.
func TestSystemOneMapping_Refusal(t *testing.T) {
	qs := map[string]llms.DecisionQuestion{"q": {Kind: llms.DecisionKindChoice}}
	got := parse(t, `{"q":{"type":"refusal"},"extra":{"type":"refusal"}}`, qs)
	if q := got["q"]; !q.Refused || !q.Null || q.Kind != llms.DecisionKindChoice || q.Value != nil || q.Probability != nil {
		t.Fatalf("refusal: %+v", q)
	}
	// A refusal for a name the request did not ask keeps a non-empty kind.
	if e := got["extra"]; !e.Refused || e.Kind != "refusal" {
		t.Fatalf("unasked refusal: %+v", e)
	}
}

// TestSystemOneMapping_BoxInRaw: box answers are spec-sourced, so the fields stay
// in Raw and Box is not filled until a live box answer is observed.
func TestSystemOneMapping_BoxInRaw(t *testing.T) {
	got := parse(t, `{"b":{"type":"box","box":{"top":0,"bottom":0.5,"left":0.1,"right":0.9},"confidence":0.8}}`, nil)["b"]
	if got.Kind != llms.DecisionKindBox || got.Box != nil || *got.Confidence != 0.8 || !strings.Contains(string(got.Raw), `"bottom":0.5`) {
		t.Fatalf("box: %+v", got)
	}
}

func TestSystemOneMapping_UnknownTypeKept(t *testing.T) {
	got := parse(t, `{"x":{"type":"novel","foo":1}}`, nil)["x"]
	if got.Kind != "novel" || !strings.Contains(string(got.Raw), `"foo":1`) {
		t.Fatalf("unknown type: %+v", got)
	}
}

func TestSystemOneMapping_MalformedAnswer(t *testing.T) {
	raw := map[string]json.RawMessage{"x": json.RawMessage(`{"type":1}`)}
	if _, err := ParseAnswers(raw, nil); err == nil {
		t.Fatal("malformed answer accepted")
	}
}

func TestSystemOneMapping_Questions(t *testing.T) {
	qs := map[string]llms.DecisionQuestion{
		"urgent": {Kind: llms.DecisionKindNoul, Instructions: "Urgent?", TrueGuidance: "time-sensitive"},
		"dept":   {Kind: llms.DecisionKindChoice, Choices: []llms.DecisionChoice{{Label: "tech", Guidance: "bugs"}, {Label: "billing"}}},
		"anger":  {Kind: llms.DecisionKindScore, Instructions: "How angry?", Levels: []string{"calm", "furious"}, Extra: map[string]any{"note": "passthrough"}},
	}
	wire, err := BuildQuestions(qs, allKinds...)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		`"urgent":{"criteria":{"true":"time-sensitive"},"instructions":"Urgent?","type":"noul"}`,
		// Choice order is the caller's, not sorted: tech before billing.
		`"criteria":{"tech":"bugs","billing":null}`,
		`"anger":{"criteria":["calm","furious"],"instructions":"How angry?","note":"passthrough","type":"score"}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("wire missing %s\n got %s", want, got)
		}
	}
	if strings.Contains(got, `"criteria":{}`) {
		t.Errorf("noul without guidance must omit criteria: %s", got)
	}
}

func TestBuildQuestions_Rejections(t *testing.T) {
	one := 1.0
	two := 2
	for name, q := range map[string]llms.DecisionQuestion{
		"unsupported kind":   {Kind: llms.DecisionKindText},
		"empty choice":       {Kind: llms.DecisionKindChoice},
		"blank label":        {Kind: llms.DecisionKindChoice, Choices: []llms.DecisionChoice{{Label: " "}}},
		"repeated label":     {Kind: llms.DecisionKindChoice, Choices: []llms.DecisionChoice{{Label: "a"}, {Label: "a"}}},
		"score no levels":    {Kind: llms.DecisionKindScore},
		"box no target":      {Kind: llms.DecisionKindBox},
		"choices on noul":    {Kind: llms.DecisionKindNoul, Choices: []llms.DecisionChoice{{Label: "a"}}},
		"guidance on choice": {Kind: llms.DecisionKindChoice, Choices: []llms.DecisionChoice{{Label: "a"}}, TrueGuidance: "x"},
		"levels on noul":     {Kind: llms.DecisionKindNoul, Levels: []string{"a"}},
		"items":              {Kind: llms.DecisionKindNoul, Items: llms.DecisionKindText},
		"decimals":           {Kind: llms.DecisionKindNoul, Decimals: &two},
		"min":                {Kind: llms.DecisionKindNoul, Min: &one},
		"reserved extra":     {Kind: llms.DecisionKindNoul, Extra: map[string]any{"type": "choice"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := BuildQuestions(map[string]llms.DecisionQuestion{"q": q}, allKinds[:3]...)
			if name == "box no target" {
				_, err = BuildQuestions(map[string]llms.DecisionQuestion{"q": q}, allKinds...)
			}
			if !errors.Is(err, llms.ErrInvalidParameters) {
				t.Fatalf("err = %v, want ErrInvalidParameters", err)
			}
		})
	}
}

func TestValidateRequest(t *testing.T) {
	ok := map[string]llms.DecisionQuestion{"q": {Kind: llms.DecisionKindNoul}}
	for name, req := range map[string]llms.DecisionRequest{
		"nil state":    {Questions: ok},
		"blank state":  {State: "  ", Questions: ok},
		"no questions": {State: "x"},
		"blank name":   {State: "x", Questions: map[string]llms.DecisionQuestion{" ": {Kind: llms.DecisionKindNoul}}},
	} {
		if err := ValidateRequest(req); !errors.Is(err, llms.ErrInvalidParameters) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := ValidateRequest(llms.DecisionRequest{State: map[string]any{"a": 1}, Questions: ok}); err != nil {
		t.Fatalf("object state rejected: %v", err)
	}
}

func TestValidateRequest_ErrorOmitsState(t *testing.T) {
	const secret = "account 4471 is failing"
	err := ValidateRequest(llms.DecisionRequest{State: secret})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("err = %v", err)
	}
}

func TestUsageCore(t *testing.T) {
	var u Usage
	// Live OpenRouter usage, 2026-11-14, plus the spec's cached_tokens field.
	if err := json.Unmarshal([]byte(`{"input_tokens":389,"output_tokens":61,"cost":1.6338e-05,"is_byok":false,"input_tokens_details":{"cached_tokens":12}}`), &u); err != nil {
		t.Fatal(err)
	}
	core := u.Core()
	if core.PromptTokens != 389 || core.CompletionTokens != 61 || core.TotalTokens != 450 || core.CacheReadTokens != 12 || core.Cost == nil || *core.Cost != 1.6338e-05 {
		t.Fatalf("usage: %+v", core)
	}
	if (Usage{InputTokens: 1}).Core().Cost != nil {
		t.Fatal("absent cost became non-nil")
	}
}

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   *llms.APIError
		want  error
		retry bool
	}{
		{"openrouter 402", &llms.APIError{StatusCode: 402, Code: "402"}, llms.ErrQuotaExceeded, false},
		{"mapped quota code kept", &llms.APIError{StatusCode: 402, Code: CodeInsufficientQuota}, llms.ErrQuotaExceeded, false},
		{"mapped model code kept", &llms.APIError{StatusCode: 400, Code: CodeModelNotFound, Message: "No route for that URI"}, llms.ErrModelNotFound, false},
		{"openrouter unknown model", &llms.APIError{StatusCode: 400, Code: "400", Message: "Model typesafe/nope does not exist"}, llms.ErrModelNotFound, false},
		{"bad input", &llms.APIError{StatusCode: 400, Code: "5006", Message: "Bad input"}, llms.ErrInvalidParameters, false},
		{"bad input with other type", &llms.APIError{StatusCode: 400, Code: "400", Type: "zod_error", Message: "Bad input"}, llms.ErrInvalidParameters, false},
		{"validation 422", &llms.APIError{StatusCode: 422, Code: "3030", Message: "images.0 invalid"}, llms.ErrInvalidParameters, false},
		{"413", &llms.APIError{StatusCode: 413}, llms.ErrContextLengthExceeded, false},
		{"524", &llms.APIError{StatusCode: 524}, llms.ErrTimeout, true},
		{"429", &llms.APIError{StatusCode: 429}, llms.ErrRateLimited, true},
		{"named code kept", &llms.APIError{StatusCode: 402, Code: "plan_required"}, llms.ErrPlanRequired, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Classify(tc.err)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if tc.err.IsRetryable() != tc.retry {
				t.Fatalf("retryable = %v, want %v", tc.err.IsRetryable(), tc.retry)
			}
		})
	}
	plain := errors.New("x")
	if !errors.Is(Classify(plain), plain) {
		t.Fatal("non-API error changed")
	}
}
