package cloudflare

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/decisionwire"
)

// decisionWire names the wire format a decision model speaks.
type decisionWire int

const (
	wireSystemOne decisionWire = iota + 1
	wireClairic
)

// decisionModels maps each supported decision model to its wire format. It is
// read-only after package init. Models not listed return ErrDecisionNotSupported
// rather than a guessed format. typesafe/jev is omitted: on Workers AI it bills
// through unified billing and its /ai/run shape has not been verified.
var decisionModels = map[string]decisionWire{
	"@cf/cloudflare/clef":       wireSystemOne,
	"@cf/cloudflare/clef-flash": wireSystemOne,
	"@cf/cloudflare/clef-omni":  wireSystemOne,
	"@cf/cloudflare/clairic":    wireClairic,
}

// Limits from the Clef model pages, enforced before sending. The ID rules are
// applied to Clairic too; its page states no question cap, so none is applied.
const (
	maxQuestions      = 64
	maxQuestionIDLen  = 100
	maxDecisionImages = 4
)

var systemOneKinds = []llms.DecisionKind{llms.DecisionKindNoul, llms.DecisionKindChoice, llms.DecisionKindScore}

var clairicKinds = map[llms.DecisionKind]bool{
	llms.DecisionKindText: true, llms.DecisionKindDate: true, llms.DecisionKindNumber: true,
	llms.DecisionKindList: true, llms.DecisionKindEstimate: true,
}

var clairicListItems = map[llms.DecisionKind]bool{llms.DecisionKindText: true, llms.DecisionKindNumber: true, llms.DecisionKindDate: true}

// reservedDecisionKeys are body keys owned by typed fields on either wire
// format; Extra may not set them.
var reservedDecisionKeys = map[string]bool{"model": true, "state": true, "questions": true, "fields": true, "images": true, "reference_date": true}

var imageMIMETypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true}

// Decide evaluates typed questions on a Workers AI decision model through
// POST /ai/run/{model}. The model is the per-call llms.WithDecisionModel, else
// WithDecisionModel, else DefaultDecisionModel.
//
// Clef, Clef-flash and Clef-omni take noul, choice and score questions, and
// every question needs Instructions. Clairic takes text, date,
// number, list and estimate questions over a string State, plus ReferenceDate.
// Assets must be embedded PNG, JPEG or WebP images (at most four); they are sent
// as data URIs in name order. Audio and video are not supported yet.
//
// Errors: ErrDecisionNotSupported for a model outside the supported set,
// ErrInvalidParameters for a request the model cannot serve, ErrQuotaExceeded
// for insufficient balance, and ErrModelNotFound for an unknown route. The state
// is sent verbatim and never logged or placed in SDK error text; a Workers AI
// error message is passed through as written.
func (c *Client) Decide(ctx context.Context, req llms.DecisionRequest, opts ...llms.DecisionOption) (*llms.DecisionResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	o := llms.ApplyDecisionOptions(opts...)
	model := o.Model
	if model == "" {
		model = c.options.DecisionModel
	}
	if model == "" {
		model = DefaultDecisionModel
	}
	wire, ok := decisionModels[model]
	if !ok {
		return nil, fmt.Errorf("cloudflare: decision model %q: %w", model, llms.ErrDecisionNotSupported)
	}
	if o.SessionID != "" || o.User != "" {
		return nil, fmt.Errorf("cloudflare: session ID and user are not supported on Workers AI: %w", llms.ErrInvalidParameters)
	}
	if err := decisionwire.ValidateRequest(req); err != nil {
		return nil, fmt.Errorf("cloudflare: %w", err)
	}
	images, err := dataURIs(req.Assets)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: %w", err)
	}
	var body map[string]any
	if wire == wireClairic {
		body, err = clairicBody(req, images)
	} else {
		body, err = systemOneBody(req, images)
	}
	if err != nil {
		return nil, fmt.Errorf("cloudflare: %w", err)
	}
	for k, v := range o.Extra {
		if reservedDecisionKeys[k] {
			return nil, fmt.Errorf("cloudflare: decision Extra key %q is reserved for a typed field: %w", k, llms.ErrInvalidParameters)
		}
		body[k] = v
	}
	var result json.RawMessage
	if err := c.post(ctx, runRoute(model), body, true, &result); err != nil {
		return nil, decisionwire.Classify(err)
	}
	var resp *llms.DecisionResponse
	if wire == wireClairic {
		resp, err = parseClairic(result)
	} else {
		resp, err = parseSystemOne(result, req.Questions)
	}
	if err != nil {
		return nil, err
	}
	// Workers AI reports a short name ("clef-flash"); report the full model ID so
	// it matches the pricing and catalog keys, and keep the short name.
	if resp.Metadata == nil {
		resp.Metadata = map[string]any{}
	}
	resp.Metadata["reported_model"] = resp.Model
	resp.Model = model
	return resp, nil
}

func systemOneBody(req llms.DecisionRequest, images []string) (map[string]any, error) {
	if req.ReferenceDate != "" {
		return nil, fmt.Errorf("ReferenceDate is only supported by Clairic: %w", llms.ErrInvalidParameters)
	}
	if err := checkQuestionIDs(req.Questions, true); err != nil {
		return nil, err
	}
	for name, q := range req.Questions {
		// Workers AI rejects a question without instructions (live 5006, 2026-11-14).
		if strings.TrimSpace(q.Instructions) == "" {
			return nil, fmt.Errorf("question %q: Instructions are required on Workers AI: %w", name, llms.ErrInvalidParameters)
		}
	}
	questions, err := decisionwire.BuildQuestions(req.Questions, systemOneKinds...)
	if err != nil {
		return nil, err
	}
	// System One takes a string, object or array state (Clef model pages); only
	// Clairic is string-only. An image-only request sends "".
	state := req.State
	if state == nil {
		state = ""
	}
	body := map[string]any{"state": state, "questions": questions}
	if len(images) > 0 {
		body["images"] = images
	}
	return body, nil
}

// checkQuestionIDs enforces the Clef ID charset and length on every wire, and the
// 64-question cap only where the model page states it (capCount).
func checkQuestionIDs(qs map[string]llms.DecisionQuestion, capCount bool) error {
	if capCount && len(qs) > maxQuestions {
		return fmt.Errorf("at most %d questions per request: %w", maxQuestions, llms.ErrInvalidParameters)
	}
	for name := range qs {
		if name == "" || len(name) > maxQuestionIDLen || strings.IndexFunc(name, func(r rune) bool { return !isIDRune(r) }) >= 0 {
			return fmt.Errorf("question IDs must be 1-%d letters, digits, '_', '.' or '-': %w", maxQuestionIDLen, llms.ErrInvalidParameters)
		}
	}
	return nil
}

func isIDRune(r rune) bool {
	return r == '_' || r == '.' || r == '-' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// dataURIs encodes assets as data URIs in name order. Only embedded images are
// accepted: Workers AI rejects remote URLs, and audio/video have not been probed.
// The four-image cap is from the Clef-omni and Clairic model pages; PNG, JPEG
// and WebP were each accepted live by clef-flash on 2026-11-14.
func dataURIs(assets map[string]llms.MediaInput) ([]string, error) {
	if len(assets) == 0 {
		return nil, nil
	}
	if len(assets) > maxDecisionImages {
		return nil, fmt.Errorf("at most %d images per request: %w", maxDecisionImages, llms.ErrInvalidParameters)
	}
	names := make([]string, 0, len(assets))
	for name := range assets {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, name := range names {
		a := assets[name]
		if err := a.Validate(); err != nil {
			return nil, fmt.Errorf("asset %q: %w", name, err)
		}
		if len(a.Data) == 0 {
			return nil, fmt.Errorf("asset %q: Workers AI takes embedded data only, not URL or FileID: %w", name, llms.ErrInvalidParameters)
		}
		if !imageMIMETypes[a.MIMEType] {
			return nil, fmt.Errorf("asset %q: only image/png, image/jpeg and image/webp are supported: %w", name, llms.ErrInvalidParameters)
		}
		out = append(out, "data:"+a.MIMEType+";base64,"+base64.StdEncoding.EncodeToString(a.Data))
	}
	return out, nil
}

func parseSystemOne(result json.RawMessage, questions map[string]llms.DecisionQuestion) (*llms.DecisionResponse, error) {
	var r struct {
		Model   string                     `json:"model"`
		Answers map[string]json.RawMessage `json:"answers"`
		Usage   decisionwire.Usage         `json:"usage"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		return nil, fmt.Errorf("cloudflare: decode decision result: %w: %w", llms.ErrIncompleteResponse, err)
	}
	if r.Answers == nil {
		return nil, fmt.Errorf("cloudflare: decision result has no answers: %w", llms.ErrIncompleteResponse)
	}
	answers, err := decisionwire.ParseAnswers(r.Answers, questions)
	if err != nil {
		return nil, fmt.Errorf("cloudflare: %w: %w", llms.ErrIncompleteResponse, err)
	}
	return &llms.DecisionResponse{Model: r.Model, Answers: answers, Usage: r.Usage.Core()}, nil
}
