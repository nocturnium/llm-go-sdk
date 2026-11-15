package cloudflare

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// Clairic wire format, observed live on 2026-11-14:
//
//	request {"state": "...", "fields": {name: {"type", "instructions"?, "decimals"?, "unit"?,
//	         "items"?, "min"?, "max"?, "step"?}}, "images"?: [data URI], "reference_date"?}
//	answer  text   {"type","value","span":[start,end]|null,"guess","p_null","confidence"}
//	        date, number {"type","value","guess","p_null","confidence"}
//	        list   {"type","value":[...],"confidences":[...],"confidence"}
//	        estimate {"type","value","mean","median","p10","p90","p_below","p_above"}
//	        a field the state cannot answer has "value": null
//	result  {"model","answers","truncated","state_text"?,"usage":{"state_tokens","image_tokens",
//	         "schema_tokens","input_tokens","output_tokens","passes"}}
//
// input_tokens already counts every pass: passes × (state_tokens + image_tokens)
// + schema_tokens on three live calls (412 = 2×39 + 334, 254 = 2×11 + 232,
// 219 = 1×64 + 155), so cost estimates use it directly.

func clairicBody(req llms.DecisionRequest, images []string) (map[string]any, error) {
	// Clairic requires the state field; an image-only request sends "" (live
	// verified 2026-11-14), so a nil State becomes the empty string.
	state, ok := req.State.(string)
	if !ok && req.State != nil {
		return nil, fmt.Errorf("clairic takes a string state: %w", llms.ErrInvalidParameters)
	}
	if err := checkQuestionIDs(req.Questions, false); err != nil {
		return nil, err
	}
	fields := make(map[string]any, len(req.Questions))
	for name, q := range req.Questions {
		field, err := clairicField(q)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", name, err)
		}
		fields[name] = field
	}
	body := map[string]any{"state": state, "fields": fields}
	if len(images) > 0 {
		body["images"] = images
	}
	if req.ReferenceDate != "" {
		body["reference_date"] = req.ReferenceDate
	}
	return body, nil
}

func clairicField(q llms.DecisionQuestion) (map[string]any, error) {
	if !clairicKinds[q.Kind] {
		return nil, fmt.Errorf("kind %q is not a Clairic field type: %w", q.Kind, llms.ErrInvalidParameters)
	}
	if len(q.Choices) > 0 || len(q.Levels) > 0 || q.TrueGuidance != "" || q.FalseGuidance != "" {
		return nil, fmt.Errorf("choices, levels and true/false guidance do not apply to clairic fields: %w", llms.ErrInvalidParameters)
	}
	field := map[string]any{"type": string(q.Kind)}
	if q.Instructions != "" {
		field["instructions"] = q.Instructions
	}
	switch q.Kind {
	case llms.DecisionKindList:
		if !clairicListItems[q.Items] {
			return nil, fmt.Errorf("list Items must be text, number or date: %w", llms.ErrInvalidParameters)
		}
		// Element number settings have no observed wire form; refuse rather than drop them.
		if q.Decimals != nil || q.Unit != "" {
			return nil, fmt.Errorf("decimals and unit are not supported on list fields: %w", llms.ErrInvalidParameters)
		}
		field["items"] = string(q.Items)
	case llms.DecisionKindNumber:
		if q.Decimals != nil {
			field["decimals"] = *q.Decimals
		}
		if q.Unit != "" {
			field["unit"] = q.Unit
		}
	case llms.DecisionKindEstimate:
		for key, v := range map[string]*float64{"min": q.Min, "max": q.Max, "step": q.Step} {
			if v != nil {
				field[key] = *v
			}
		}
	default:
		// text and date fields take only instructions.
	}
	if q.Kind != llms.DecisionKindList && q.Items != "" {
		return nil, fmt.Errorf("items applies only to list fields: %w", llms.ErrInvalidParameters)
	}
	// A list with Decimals or Unit was already refused above.
	if q.Kind != llms.DecisionKindNumber && (q.Decimals != nil || q.Unit != "") {
		return nil, fmt.Errorf("decimals and unit apply only to number fields: %w", llms.ErrInvalidParameters)
	}
	if q.Kind != llms.DecisionKindEstimate && (q.Min != nil || q.Max != nil || q.Step != nil) {
		return nil, fmt.Errorf("min, max and step apply only to estimate fields: %w", llms.ErrInvalidParameters)
	}
	for k, v := range q.Extra {
		if clairicFieldKeys[k] {
			return nil, fmt.Errorf("field Extra key %q is reserved for a typed option: %w", k, llms.ErrInvalidParameters)
		}
		field[k] = v
	}
	return field, nil
}

// clairicFieldKeys are field keys owned by typed DecisionQuestion fields.
var clairicFieldKeys = map[string]bool{"type": true, "instructions": true, "decimals": true, "unit": true, "items": true, "min": true, "max": true, "step": true}

type clairicAnswer struct {
	Type        string          `json:"type"`
	Value       json.RawMessage `json:"value"`
	Guess       json.RawMessage `json:"guess"`
	PNull       *float64        `json:"p_null"`
	Span        []int           `json:"span"`
	Confidence  *float64        `json:"confidence"`
	Confidences []float64       `json:"confidences"`
	Mean        *float64        `json:"mean"`
	Median      *float64        `json:"median"`
	P10         *float64        `json:"p10"`
	P90         *float64        `json:"p90"`
	PBelow      *float64        `json:"p_below"`
	PAbove      *float64        `json:"p_above"`
}

type clairicResult struct {
	Model     string                     `json:"model"`
	Answers   map[string]json.RawMessage `json:"answers"`
	Truncated bool                       `json:"truncated"`
	StateText *string                    `json:"state_text"`
	Usage     struct {
		StateTokens  int `json:"state_tokens"`
		ImageTokens  int `json:"image_tokens"`
		SchemaTokens int `json:"schema_tokens"`
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		Passes       int `json:"passes"`
	} `json:"usage"`
}

func parseClairic(result json.RawMessage) (*llms.DecisionResponse, error) {
	var r clairicResult
	if err := json.Unmarshal(result, &r); err != nil {
		return nil, fmt.Errorf("cloudflare: decode Clairic result: %w: %w", llms.ErrIncompleteResponse, err)
	}
	if r.Answers == nil {
		return nil, fmt.Errorf("cloudflare: Clairic result has no answers: %w", llms.ErrIncompleteResponse)
	}
	answers := make(map[string]llms.DecisionAnswer, len(r.Answers))
	names := make([]string, 0, len(r.Answers))
	for name := range r.Answers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		a, err := convertClairicAnswer(r.Answers[name])
		if err != nil {
			return nil, fmt.Errorf("cloudflare: answer %q: %w: %w", name, llms.ErrIncompleteResponse, err)
		}
		answers[name] = a
	}
	resp := &llms.DecisionResponse{
		Model:     r.Model,
		Answers:   answers,
		Truncated: r.Truncated,
		Passes:    r.Usage.Passes,
		Usage: llms.Usage{
			PromptTokens:     r.Usage.InputTokens,
			CompletionTokens: r.Usage.OutputTokens,
			TotalTokens:      r.Usage.InputTokens + r.Usage.OutputTokens,
		},
		Breakdown: map[string]int{
			"state_tokens":  r.Usage.StateTokens,
			"image_tokens":  r.Usage.ImageTokens,
			"schema_tokens": r.Usage.SchemaTokens,
		},
	}
	if r.StateText != nil {
		// state_text echoes the caller's state. It is returned to the caller, who
		// owns it; the SDK itself never logs it or puts it in an error.
		resp.Metadata = map[string]any{"state_text": *r.StateText}
	}
	return resp, nil
}

func convertClairicAnswer(data json.RawMessage) (llms.DecisionAnswer, error) {
	var w clairicAnswer
	if err := json.Unmarshal(data, &w); err != nil {
		return llms.DecisionAnswer{}, err
	}
	a := llms.DecisionAnswer{
		Kind:        llms.DecisionKind(w.Type),
		PNull:       w.PNull,
		Confidence:  w.Confidence,
		Confidences: w.Confidences,
		Raw:         append(json.RawMessage(nil), data...),
	}
	value, err := decodeOptional(w.Value)
	if err != nil {
		return a, err
	}
	if value == nil {
		a.Null = true
	} else {
		a.Value = value
	}
	if a.Guess, err = decodeOptional(w.Guess); err != nil {
		return a, err
	}
	if len(w.Span) == 2 {
		a.Span = &llms.DecisionSpan{Start: w.Span[0], End: w.Span[1]}
	}
	if w.Type == string(llms.DecisionKindEstimate) {
		a.Estimate = &llms.DecisionEstimate{Mean: w.Mean, Median: w.Median, P10: w.P10, P90: w.P90, PBelow: w.PBelow, PAbove: w.PAbove}
	}
	return a, nil
}

// decodeOptional decodes a JSON value, returning nil for an absent or null value.
func decodeOptional(raw json.RawMessage) (any, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return v, nil
}
