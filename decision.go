package llms

import (
	"context"
	"encoding/json"
	"errors"
)

// DecisionKind is the type of a decision question and of its answer.
//
// Decision models evaluate a state against typed questions and return typed,
// calibrated answers instead of generated text. Providers translate these kinds
// to and from their own wire names; the constants here are the SDK's names.
type DecisionKind string

const (
	// DecisionKindNoul is a yes/no question answered with a probability of "yes".
	// Wire names such as "predicate" normalize to this kind.
	DecisionKindNoul DecisionKind = "noul"
	// DecisionKindChoice picks one option from DecisionQuestion.Choices.
	DecisionKindChoice DecisionKind = "choice"
	// DecisionKindScore places the state on the ordered DecisionQuestion.Levels.
	DecisionKindScore DecisionKind = "score"
	// DecisionKindBox locates one target in an image and answers with a DecisionBox.
	DecisionKindBox DecisionKind = "box"
	// DecisionKindText extracts an exact span of the state.
	DecisionKindText DecisionKind = "text"
	// DecisionKindDate extracts or computes a date.
	DecisionKindDate DecisionKind = "date"
	// DecisionKindNumber extracts or computes a number.
	DecisionKindNumber DecisionKind = "number"
	// DecisionKindList extracts a list whose element kind is DecisionQuestion.Items.
	DecisionKindList DecisionKind = "list"
	// DecisionKindEstimate forecasts a number and answers with a DecisionEstimate.
	DecisionKindEstimate DecisionKind = "estimate"
)

// DecisionChoice is one option of a choice question. Choices are a slice, not a
// map, because option order is visible to the model and must be caller-controlled.
type DecisionChoice struct {
	// Label is the value the model answers with when it picks this option.
	Label string `json:"label"`
	// Guidance describes when the option applies.
	Guidance string `json:"guidance,omitempty"`
}

// DecisionQuestion is one typed question. Only the fields relevant to Kind are
// read; a provider returns ErrInvalidParameters for a kind or field it cannot
// serve rather than silently dropping it.
type DecisionQuestion struct {
	// Kind selects the question type.
	Kind DecisionKind `json:"kind"`
	// Instructions is the question text.
	Instructions string `json:"instructions,omitempty"`
	// Choices lists the options of a choice question, in order.
	Choices []DecisionChoice `json:"choices,omitempty"`
	// TrueGuidance describes when a noul question is true.
	TrueGuidance string `json:"true_guidance,omitempty"`
	// FalseGuidance describes when a noul question is false.
	FalseGuidance string `json:"false_guidance,omitempty"`
	// Levels lists the levels of a score question, ordered lowest first.
	Levels []string `json:"levels,omitempty"`
	// Items is the element kind of a list question (text, number, or date).
	Items DecisionKind `json:"items,omitempty"`
	// Decimals sets the precision of a number question.
	Decimals *int `json:"decimals,omitempty"`
	// Unit names the unit of a number question.
	Unit string `json:"unit,omitempty"`
	// Min is the lower bound of an estimate question.
	Min *float64 `json:"min,omitempty"`
	// Max is the upper bound of an estimate question.
	Max *float64 `json:"max,omitempty"`
	// Step is the resolution of an estimate question.
	Step *float64 `json:"step,omitempty"`
	// Extra carries provider-specific question fields, merged last.
	Extra map[string]any `json:"extra,omitempty"`
}

// DecisionRequest is the input to Decider.Decide.
//
// State is sent to the provider verbatim. Implementations must never log it,
// trace it, or place it in error text of their own. A provider's error message
// is passed through in APIError.Message as the provider wrote it.
type DecisionRequest struct {
	// State is the content to evaluate: a string, an object, or an array.
	State any `json:"state"`
	// Questions maps answer names to questions.
	Questions map[string]DecisionQuestion `json:"questions"`
	// Assets maps names to media referenced by the state. Providers that do not
	// support a source kind (for example URL or FileID) return ErrInvalidParameters.
	Assets map[string]MediaInput `json:"assets,omitempty"`
	// ReferenceDate is an ISO date used to resolve dates written without a year.
	// Providers that do not support it return ErrInvalidParameters when it is set.
	ReferenceDate string `json:"reference_date,omitempty"`
}

// DecisionSpan is a half-open range of runes (Unicode code points, not bytes)
// into a string State. Convert before slicing a Go string by byte index.
type DecisionSpan struct {
	// Start is the first rune of the span.
	Start int `json:"start"`
	// End is one past the last rune of the span.
	End int `json:"end"`
}

// DecisionBox is a bounding box. Each edge is a fraction of the image size.
// Fields are pointers because 0 is a valid edge and must differ from absent.
type DecisionBox struct {
	// Top is the top edge.
	Top *float64 `json:"top"`
	// Bottom is the bottom edge.
	Bottom *float64 `json:"bottom"`
	// Left is the left edge.
	Left *float64 `json:"left"`
	// Right is the right edge.
	Right *float64 `json:"right"`
	// Confidence is the provider's confidence in the box.
	Confidence *float64 `json:"confidence,omitempty"`
}

// DecisionEstimate is the distribution of an estimate answer. Every field is a
// pointer because 0 is a valid probability. The point estimate itself is in
// DecisionAnswer.Value.
type DecisionEstimate struct {
	// Mean is the expected value.
	Mean *float64 `json:"mean,omitempty"`
	// Median is the 50th percentile.
	Median *float64 `json:"median,omitempty"`
	// P10 is the 10th percentile.
	P10 *float64 `json:"p10,omitempty"`
	// P90 is the 90th percentile.
	P90 *float64 `json:"p90,omitempty"`
	// PBelow is the probability the true value is below the estimate range.
	PBelow *float64 `json:"p_below,omitempty"`
	// PAbove is the probability the true value is above the estimate range.
	PAbove *float64 `json:"p_above,omitempty"`
}

// DecisionAnswer is the answer to one question. Only the fields relevant to Kind
// are set. Raw always holds the provider's answer object.
//
// A populated answer never sets Null. A null answer can still carry Span, Guess
// and PNull. A refusal sets both Refused and Null, with Value and Probability nil.
type DecisionAnswer struct {
	// Kind is the answer type.
	Kind DecisionKind `json:"kind"`
	// Null reports that the provider determined no value or refused.
	Null bool `json:"null,omitempty"`
	// Refused reports that the provider declined to answer.
	Refused bool `json:"refused,omitempty"`
	// Value is the answer for text, date, number, list, and estimate kinds.
	// Numbers decode as float64 (JSON's own precision, exact to 2^53).
	Value any `json:"value,omitempty"`
	// Guess is the provider's best-effort value, set even when Null is true.
	Guess any `json:"guess,omitempty"`
	// Choice is the picked option label of a choice answer.
	Choice string `json:"choice,omitempty"`
	// Probability is the probability of "yes" for a noul answer.
	Probability *float64 `json:"probability,omitempty"`
	// Score is the probability-weighted level index of a score answer.
	Score *float64 `json:"score,omitempty"`
	// Legend maps score level indexes to level labels.
	Legend map[string]string `json:"legend,omitempty"`
	// Probabilities maps options or level indexes to probabilities.
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	// Confidence is the provider's confidence in the answer.
	Confidence *float64 `json:"confidence,omitempty"`
	// Confidences holds per-element confidence for a list answer.
	Confidences []float64 `json:"confidences,omitempty"`
	// PNull is the probability that no value exists.
	PNull *float64 `json:"p_null,omitempty"`
	// Span locates a text answer in the state.
	Span *DecisionSpan `json:"span,omitempty"`
	// Box is the bounding box of a box answer.
	Box *DecisionBox `json:"box,omitempty"`
	// Estimate is the distribution of an estimate answer.
	Estimate *DecisionEstimate `json:"estimate,omitempty"`
	// Raw is the provider's answer object. It can echo state content; never log it.
	Raw json.RawMessage `json:"raw,omitempty"`
}

// DecisionResponse is the result of Decider.Decide.
type DecisionResponse struct {
	// Model identifies the model that answered.
	Model string `json:"model"`
	// Answers maps question names to answers.
	Answers map[string]DecisionAnswer `json:"answers"`
	// Usage contains token counts and any provider-reported cost.
	Usage Usage `json:"usage"`
	// Truncated reports that the provider shortened the state to fit.
	Truncated bool `json:"truncated,omitempty"`
	// Passes is the number of billing passes the provider reported, if any.
	Passes int `json:"passes,omitempty"`
	// Breakdown holds provider token subtotals such as state, image, and schema tokens.
	Breakdown map[string]int `json:"breakdown,omitempty"`
	// Metadata contains provider-specific response information. A state echo, when
	// a provider returns one, is sensitive; never log it.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// DecisionOptions contains per-call options. Zero values leave provider defaults in effect.
type DecisionOptions struct {
	// Model selects the decision model.
	Model string
	// SessionID groups related requests for providers that support it.
	SessionID string
	// User identifies the end user for providers that support it.
	User string
	// Extra carries provider-specific request fields, merged last.
	Extra map[string]any
}

// DecisionOption modifies DecisionOptions.
type DecisionOption func(*DecisionOptions)

// ApplyDecisionOptions applies options in order and returns the resulting options.
func ApplyDecisionOptions(options ...DecisionOption) *DecisionOptions {
	opts := &DecisionOptions{}
	for _, opt := range options {
		if opt == nil {
			continue
		}
		opt(opts)
	}
	return opts
}

// WithDecisionModel sets Model for the request.
func WithDecisionModel(value string) DecisionOption {
	return func(o *DecisionOptions) { o.Model = value }
}

// WithDecisionSessionID sets SessionID for the request.
func WithDecisionSessionID(value string) DecisionOption {
	return func(o *DecisionOptions) { o.SessionID = value }
}

// WithDecisionUser sets User for the request.
func WithDecisionUser(value string) DecisionOption {
	return func(o *DecisionOptions) { o.User = value }
}

// WithDecisionExtra sets Extra for the request.
func WithDecisionExtra(value map[string]any) DecisionOption {
	return func(o *DecisionOptions) { o.Extra = value }
}

// ErrDecisionNotSupported indicates an absent decision capability, or a model the
// provider cannot route to a known decision wire format.
var ErrDecisionNotSupported = errors.New("decisions not supported by this provider")

// Decider evaluates typed questions against a state, independently of the LLM
// chat interface.
//
// Example:
//
//	if decider, ok := llms.AsDecider(client); ok {
//	    resp, err := decider.Decide(ctx, llms.DecisionRequest{
//	        State: "Help! My payouts have been failing for 3 days.",
//	        Questions: map[string]llms.DecisionQuestion{
//	            "urgent": {Kind: llms.DecisionKindNoul, Instructions: "Is this urgent?"},
//	        },
//	    })
//	    // resp.Answers["urgent"].Probability
//	}
type Decider interface {
	// Decide returns typed answers or a validation/provider error.
	Decide(ctx context.Context, req DecisionRequest, opts ...DecisionOption) (*DecisionResponse, error)
}

// SupportsDecision reports whether v implements Decider.
func SupportsDecision(v any) bool { _, ok := v.(Decider); return ok }

// AsDecider returns the Decider and true, or nil and false.
func AsDecider(v any) (Decider, bool) { d, ok := v.(Decider); return d, ok }
