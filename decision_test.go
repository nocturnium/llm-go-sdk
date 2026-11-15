package llms

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func f64(v float64) *float64 { return &v }

type testDecider struct{ testPlainLLM }

func (testDecider) Decide(context.Context, DecisionRequest, ...DecisionOption) (*DecisionResponse, error) {
	return &DecisionResponse{}, nil
}

func TestApplyDecisionOptions(t *testing.T) {
	extra := map[string]any{"k": "v"}
	o := ApplyDecisionOptions(nil, WithDecisionModel("m"), WithDecisionSessionID("s"), WithDecisionUser("u"), WithDecisionExtra(extra))
	if o.Model != "m" || o.SessionID != "s" || o.User != "u" || o.Extra["k"] != "v" {
		t.Fatalf("options not applied: %+v", o)
	}
	if got := ApplyDecisionOptions(); !reflect.DeepEqual(*got, DecisionOptions{}) {
		t.Fatalf("zero options changed: %+v", got)
	}
}

func TestAsDecider(t *testing.T) {
	if d, ok := AsDecider(testDecider{}); !ok || d == nil || !SupportsDecision(testDecider{}) {
		t.Fatal("decider not detected")
	}
	if d, ok := AsDecider(testPlainLLM{}); ok || d != nil || SupportsDecision(testPlainLLM{}) {
		t.Fatal("plain LLM reported as decider")
	}
	if HasDecision(testDecider{}) {
		t.Fatal("interface implementation confused with capability flag")
	}
}

func TestErrDecisionNotSupported(t *testing.T) {
	wrapped := errors.Join(errors.New("cloudflare"), ErrDecisionNotSupported)
	if !errors.Is(wrapped, ErrDecisionNotSupported) {
		t.Fatal("sentinel does not survive wrapping")
	}
}

// TestDecisionAnswer_JSONRoundTrip pins the SDK serialization of every answer
// shape the plan names. These are core JSON shapes, not provider wire shapes;
// provider adapters have their own wire-to-core fixture tests.
func TestDecisionAnswer_JSONRoundTrip(t *testing.T) {
	zero := 0.0
	cases := map[string]DecisionAnswer{
		// clef-omni live 2026-11-14.
		"noul":   {Kind: DecisionKindNoul, Probability: f64(0.9838), Raw: json.RawMessage(`{"type":"noul","noul":0.9838}`)},
		"choice": {Kind: DecisionKindChoice, Choice: "billing", Probabilities: map[string]float64{"billing": 0.8699, "tech": 0.1301}, Confidence: f64(0.5472)},
		"score":  {Kind: DecisionKindScore, Score: f64(0.986), Legend: map[string]string{"0": "low", "1": "high"}, Probabilities: map[string]float64{"0": 0.014, "1": 0.986}, Confidence: f64(0.9448)},
		// clairic live 2026-11-14.
		"text":     {Kind: DecisionKindText, Value: "lattes", Guess: "lattes", PNull: f64(0.0031), Span: &DecisionSpan{Start: 14, End: 20}, Confidence: f64(0.9977)},
		"date":     {Kind: DecisionKindDate, Value: "2026-04-01", Guess: "2026-04-01", PNull: f64(0.0000064), Confidence: f64(0.9989)},
		"number":   {Kind: DecisionKindNumber, Value: 1250.0, Guess: 1250.0, PNull: f64(0.0000224), Confidence: f64(0.9999)},
		"list":     {Kind: DecisionKindList, Value: []any{4.5}, Confidences: []float64{0.9994}, Confidence: f64(0.9947)},
		"estimate": {Kind: DecisionKindEstimate, Value: 3.0, Estimate: &DecisionEstimate{Mean: f64(2.84), Median: f64(3), P10: f64(3), P90: f64(3), PBelow: &zero, PAbove: f64(2.4e-7)}},
		"null":     {Kind: DecisionKindText, Null: true, Guess: "next steps", PNull: f64(0.9999), Span: &DecisionSpan{Start: 45, End: 55}, Confidence: f64(0.408)},
		// OpenRouter spec-sourced (DecisionsBoxAnswer, DecisionsRefusalAnswer); the
		// OpenRouter provider does not fill Box until a live box answer is observed.
		"box":     {Kind: DecisionKindBox, Box: &DecisionBox{Top: &zero, Bottom: f64(0.5), Left: f64(0.1), Right: f64(0.9), Confidence: f64(0.8)}},
		"refusal": {Kind: DecisionKindChoice, Refused: true, Null: true},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			var got DecisionAnswer
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			// Value decodes numbers as float64 and lists as []any, matching the inputs.
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("round trip changed answer\n got %#v\nwant %#v", got, want)
			}
		})
	}
}

func TestDecisionPointerZeroIsPreserved(t *testing.T) {
	zero := 0.0
	data, err := json.Marshal(DecisionEstimate{PBelow: &zero})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"p_below":0}` {
		t.Fatalf("zero probability dropped or absent fields emitted: %s", data)
	}
	data, err = json.Marshal(DecisionBox{Top: &zero})
	if err != nil {
		t.Fatal(err)
	}
	var box DecisionBox
	if err := json.Unmarshal(data, &box); err != nil {
		t.Fatal(err)
	}
	if box.Top == nil || *box.Top != 0 || box.Bottom != nil {
		t.Fatalf("box edges: %s", data)
	}
}

func TestDecisionRequest_JSON(t *testing.T) {
	req := DecisionRequest{
		State: map[string]any{"ticket": "x"},
		Questions: map[string]DecisionQuestion{
			"dept": {Kind: DecisionKindChoice, Choices: []DecisionChoice{{Label: "b", Guidance: "second"}, {Label: "a"}}},
		},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var got DecisionRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if c := got.Questions["dept"].Choices; len(c) != 2 || c[0].Label != "b" || c[1].Label != "a" {
		t.Fatalf("choice order not preserved: %+v", c)
	}
}
