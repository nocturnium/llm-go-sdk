// Package decisionwire converts between the llms decision types and the
// "System One" decision wire format shared by TypeSafe Jev on OpenRouter and the
// Cloudflare Clef models. Shapes were observed live on 2026-11-14:
//
//	request  {"model", "state", "questions": {id: {"type", "instructions", "criteria"}}}
//	answer   noul   {"type":"noul","noul":0.96}
//	         choice {"type":"choice","choice":"billing","probabilities":{...},"confidence":0.94}
//	         score  {"type":"score","score":2,"legend":{"0":"calm"},"probabilities":{"0":0},"confidence":1}
//	usage    {"input_tokens", "output_tokens", "cost"?, "input_tokens_details"?: {"cached_tokens"}}
//
// Box and refusal answers come from the OpenRouter API reference, not a live call.
// Box answers are kept in Raw until one is observed on the wire.
package decisionwire

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// Canonical llms.APIError codes. They mirror unexported constants in the llms
// package, which classifies an APIError by Code before falling back to status.
const (
	codeInsufficientQuota     = "insufficient_quota"
	codeModelNotFound         = "model_not_found"
	codeContextLengthExceeded = "context_length_exceeded"
	typeInvalidRequest        = "invalid_request_error"
)

// CodeInsufficientQuota and CodeModelNotFound are the canonical codes a
// provider's transport error code mapper returns for its own codes.
const (
	CodeInsufficientQuota = codeInsufficientQuota
	CodeModelNotFound     = codeModelNotFound
)

// ValidateRequest checks what every decision endpoint requires: a state (or
// assets) and at least one named question. Error text never includes state or question content.
func ValidateRequest(req llms.DecisionRequest) error {
	// A state may be empty only when assets carry the content (an image-only
	// request); providers that take no assets reject those separately.
	if len(req.Assets) == 0 {
		if req.State == nil {
			return fmt.Errorf("decision state is required: %w", llms.ErrInvalidParameters)
		}
		if s, ok := req.State.(string); ok && strings.TrimSpace(s) == "" {
			return fmt.Errorf("decision state is empty: %w", llms.ErrInvalidParameters)
		}
	}
	if len(req.Questions) == 0 {
		return fmt.Errorf("at least one decision question is required: %w", llms.ErrInvalidParameters)
	}
	for name := range req.Questions {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("decision question names must not be blank: %w", llms.ErrInvalidParameters)
		}
	}
	return nil
}

// BuildQuestions converts questions to System One wire questions. allowed lists
// the kinds the target endpoint serves; any other kind, or a field that does not
// apply to the question's kind, returns ErrInvalidParameters instead of being
// dropped. Extra keys are merged last; type, instructions and criteria are
// reserved for the typed fields.
func BuildQuestions(qs map[string]llms.DecisionQuestion, allowed ...llms.DecisionKind) (map[string]map[string]any, error) {
	out := make(map[string]map[string]any, len(qs))
	for _, name := range sortedKeys(qs) {
		q := qs[name]
		if !containsKind(allowed, q.Kind) {
			return nil, fmt.Errorf("question %q: kind %q is not supported by this endpoint: %w", name, q.Kind, llms.ErrInvalidParameters)
		}
		if err := checkFields(q); err != nil {
			return nil, fmt.Errorf("question %q: %w", name, err)
		}
		wire := map[string]any{"type": string(q.Kind)}
		if q.Instructions != "" {
			wire["instructions"] = q.Instructions
		}
		switch q.Kind {
		case llms.DecisionKindNoul:
			if criteria := noulCriteria(q); criteria != nil {
				wire["criteria"] = criteria
			}
		case llms.DecisionKindChoice:
			if err := checkChoices(q.Choices); err != nil {
				return nil, fmt.Errorf("question %q: %w", name, err)
			}
			wire["criteria"] = orderedChoices(q.Choices)
		case llms.DecisionKindScore:
			if len(q.Levels) == 0 {
				return nil, fmt.Errorf("question %q: score needs at least one level: %w", name, llms.ErrInvalidParameters)
			}
			wire["criteria"] = q.Levels
		case llms.DecisionKindBox:
			if strings.TrimSpace(q.Instructions) == "" {
				return nil, fmt.Errorf("question %q: box needs instructions naming the target: %w", name, llms.ErrInvalidParameters)
			}
		default:
			// Unreachable: containsKind rejected every kind outside allowed.
		}
		for k, v := range q.Extra {
			if k == "type" || k == "instructions" || k == "criteria" {
				return nil, fmt.Errorf("question %q: Extra key %q is reserved for a typed field: %w", name, k, llms.ErrInvalidParameters)
			}
			wire[k] = v
		}
		out[name] = wire
	}
	return out, nil
}

// checkFields rejects fields that only apply to other kinds. The System One
// format has no list, number or estimate questions, so Items, Decimals, Unit,
// Min, Max and Step never apply to it and are rejected for every kind; those
// fields belong to Clairic, which has its own builder.
func checkFields(q llms.DecisionQuestion) error {
	bad := func(field string) error {
		return fmt.Errorf("field %s does not apply to kind %q: %w", field, q.Kind, llms.ErrInvalidParameters)
	}
	switch {
	case q.Kind != llms.DecisionKindChoice && len(q.Choices) > 0:
		return bad("Choices")
	case q.Kind != llms.DecisionKindNoul && (q.TrueGuidance != "" || q.FalseGuidance != ""):
		return bad("TrueGuidance/FalseGuidance")
	case q.Kind != llms.DecisionKindScore && len(q.Levels) > 0:
		return bad("Levels")
	case q.Items != "":
		return bad("Items")
	case q.Decimals != nil || q.Unit != "":
		return bad("Decimals/Unit")
	case q.Min != nil || q.Max != nil || q.Step != nil:
		return bad("Min/Max/Step")
	}
	return nil
}

func checkChoices(choices []llms.DecisionChoice) error {
	if len(choices) == 0 {
		return fmt.Errorf("choice needs at least one option: %w", llms.ErrInvalidParameters)
	}
	seen := make(map[string]bool, len(choices))
	for _, c := range choices {
		if strings.TrimSpace(c.Label) == "" {
			return fmt.Errorf("choice labels must not be blank: %w", llms.ErrInvalidParameters)
		}
		if seen[c.Label] {
			return fmt.Errorf("choice label %q is repeated: %w", c.Label, llms.ErrInvalidParameters)
		}
		seen[c.Label] = true
	}
	return nil
}

func noulCriteria(q llms.DecisionQuestion) map[string]string {
	if q.TrueGuidance == "" && q.FalseGuidance == "" {
		return nil
	}
	criteria := map[string]string{}
	if q.TrueGuidance != "" {
		criteria["true"] = q.TrueGuidance
	}
	if q.FalseGuidance != "" {
		criteria["false"] = q.FalseGuidance
	}
	return criteria
}

// orderedChoices encodes choices as a JSON object whose keys keep slice order.
// A Go map would sort them, and option order is visible to the model.
type orderedChoices []llms.DecisionChoice

// MarshalJSON writes {"label": "guidance", ...} in slice order. An empty
// guidance is sent as null, which the OpenRouter schema documents as valid.
func (o orderedChoices) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, c := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(c.Label)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		if c.Guidance == "" {
			buf.WriteString("null")
			continue
		}
		value, err := json.Marshal(c.Guidance)
		if err != nil {
			return nil, err
		}
		buf.Write(value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// wireAnswer is the union of System One answer fields.
type wireAnswer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul"`
	Choice        string             `json:"choice"`
	Score         *float64           `json:"score"`
	Legend        map[string]string  `json:"legend"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    *float64           `json:"confidence"`
}

// ParseAnswers converts System One answers. questions supplies the kind of a
// refusal, whose wire form carries no kind. Every answer keeps its wire object
// in Raw. Unknown answer types are kept with Kind set to the wire type.
func ParseAnswers(raw map[string]json.RawMessage, questions map[string]llms.DecisionQuestion) (map[string]llms.DecisionAnswer, error) {
	out := make(map[string]llms.DecisionAnswer, len(raw))
	for name, data := range raw {
		var w wireAnswer
		if err := json.Unmarshal(data, &w); err != nil {
			return nil, fmt.Errorf("decode answer %q: %w", name, err)
		}
		a := llms.DecisionAnswer{Raw: append(json.RawMessage(nil), data...)}
		switch w.Type {
		case "noul", "predicate":
			a.Kind = llms.DecisionKindNoul
			a.Probability = w.Noul
		case "choice":
			a.Kind = llms.DecisionKindChoice
			a.Choice, a.Probabilities, a.Confidence = w.Choice, w.Probabilities, w.Confidence
		case "score":
			a.Kind = llms.DecisionKindScore
			a.Score, a.Legend, a.Probabilities, a.Confidence = w.Score, w.Legend, w.Probabilities, w.Confidence
		case "box":
			// Spec-sourced only; Box is filled once a live box answer is observed.
			a.Kind = llms.DecisionKindBox
			a.Confidence = w.Confidence
		case "refusal":
			// The wire refusal carries no kind, so take it from the question. An
			// answer for a name the request did not ask keeps the wire type.
			a.Kind = llms.DecisionKind(w.Type)
			if q, ok := questions[name]; ok {
				a.Kind = q.Kind
			}
			a.Refused, a.Null = true, true
		default:
			a.Kind = llms.DecisionKind(w.Type)
		}
		out[name] = a
	}
	return out, nil
}

// Usage is the System One usage object.
type Usage struct {
	InputTokens        int      `json:"input_tokens"`
	OutputTokens       int      `json:"output_tokens"`
	Cost               *float64 `json:"cost"`
	IsBYOK             *bool    `json:"is_byok"`
	InputTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}

// Core converts the usage. Cost is provider-reported USD and nil when absent.
func (u Usage) Core() llms.Usage {
	return llms.Usage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.InputTokens + u.OutputTokens,
		CacheReadTokens:  u.InputTokensDetails.CachedTokens,
		Cost:             u.Cost,
	}
}

// Classify maps decision-endpoint failures onto SDK sentinels by setting the
// canonical code or type on an *llms.APIError. Provider-specific codes (for
// example Cloudflare "2021") are mapped earlier, by the transport's error code
// mapper; Classify then only fills in what the status implies when the code
// carries no meaning of its own (empty, or a bare number such as an echoed HTTP
// status):
//
//   - 402 is a billing failure: ErrQuotaExceeded, not ErrPlanRequired.
//   - 413 is ErrContextLengthExceeded.
//   - 400 or 422 is ErrModelNotFound when the message says the model does not
//     exist (OpenRouter's wording for an unknown model, returned as 400, not
//     404), and ErrInvalidParameters otherwise. Cloudflare's unknown-model
//     wording ("No route for that URI", code 7000) is mapped earlier by the
//     Cloudflare transport.
//
// A named code (such as "insufficient_quota") is left alone. Other errors pass
// through unchanged.
func Classify(err error) error {
	var apiErr *llms.APIError
	if !errors.As(err, &apiErr) || apiErr == nil || !overridable(apiErr.Code) {
		return err
	}
	switch apiErr.StatusCode {
	case 402:
		apiErr.Code = codeInsufficientQuota
	case 413:
		apiErr.Code = codeContextLengthExceeded
	case 400, 422:
		if modelMissing.MatchString(apiErr.Message) {
			apiErr.Code = codeModelNotFound
		} else {
			apiErr.Type = typeInvalidRequest
		}
	}
	return err
}

// modelMissing matches OpenRouter's unknown-model message, "Model X does not
// exist" (live 2026-11-14), and not other messages that mention existence.
var modelMissing = regexp.MustCompile(`(?i)^model \S+ does not exist`)

// overridable reports whether an APIError code carries no meaning of its own:
// empty, or a bare number such as an echoed HTTP status.
func overridable(code string) bool {
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func containsKind(kinds []llms.DecisionKind, k llms.DecisionKind) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
