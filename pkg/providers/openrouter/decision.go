package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"unicode/utf8"

	llms "github.com/nocturnium/llm-go-sdk/v6"
	"github.com/nocturnium/llm-go-sdk/v6/internal/decisionwire"
	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
	"github.com/nocturnium/llm-go-sdk/v6/pkg/openaicompat"
)

// decisionKinds are the kinds Decide sends. Box is in the OpenRouter schema but
// needs an image asset, and the in-text asset reference syntax is undocumented,
// so box and Assets stay refused until a live box call confirms both.
var decisionKinds = []llms.DecisionKind{llms.DecisionKindNoul, llms.DecisionKindChoice, llms.DecisionKindScore}

// maxDecisionIDLength bounds session_id and user, per the OpenRouter schema.
const maxDecisionIDLength = 256

// reservedDecisionKeys are body keys owned by typed fields and options; Extra
// may not set them.
var reservedDecisionKeys = map[string]bool{"model": true, "state": true, "questions": true, "assets": true, "session_id": true, "user": true}

type decisionResponse struct {
	ID          string                     `json:"id"`
	Model       string                     `json:"model"`
	Provider    string                     `json:"provider"`
	Answers     map[string]json.RawMessage `json:"answers"`
	Annotations json.RawMessage            `json:"annotations"`
	Usage       decisionwire.Usage         `json:"usage"`
}

// Decide evaluates typed questions against req.State on OpenRouter's
// Decisions API (POST /api/alpha/decisions, schema read 2026-11-14). The model is
// the per-call llms.WithDecisionModel, else WithDecisionModel, else
// DefaultDecisionModel. Noul, choice and score questions are supported; box
// questions, Assets and ReferenceDate return ErrInvalidParameters. Usage.Cost is
// OpenRouter's reported charge in USD.
//
// Errors: ErrInvalidParameters for a request the endpoint cannot serve,
// ErrQuotaExceeded for insufficient credits (402), ErrModelNotFound for an
// unknown model, ErrContextLengthExceeded (413), and the retryable
// ErrRateLimited, ErrTimeout (524) and ErrServiceUnavailable. The state is sent
// verbatim and never logged or placed in SDK error text; an OpenRouter error
// message is passed through as written.
func (c *Client) Decide(ctx context.Context, req llms.DecisionRequest, opts ...llms.DecisionOption) (*llms.DecisionResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	body, questions, err := c.buildDecisionBody(req, llms.ApplyDecisionOptions(opts...))
	if err != nil {
		return nil, fmt.Errorf("openrouter: %w", err)
	}
	var resp decisionResponse
	err = c.transport.DoJSON(ctx, httpclient.Request{
		Method:    http.MethodPost,
		URL:       endpointURL(c.alphaURL, "decisions", nil),
		Headers:   c.headers,
		Body:      body,
		Sensitive: true,
	}, &resp)
	if err != nil {
		// WrapError returns the *llms.APIError itself, which Classify finds with
		// errors.As; TestDecide_ErrorMapping pins 402, 413 and 400 through this path.
		return nil, decisionwire.Classify(openaicompat.WrapError(c.Provider(), "decisions", err))
	}
	return convertDecisionResponse(&resp, questions)
}

func (c *Client) buildDecisionBody(req llms.DecisionRequest, o *llms.DecisionOptions) (map[string]any, map[string]llms.DecisionQuestion, error) {
	if err := decisionwire.ValidateRequest(req); err != nil {
		return nil, nil, err
	}
	if len(req.Assets) > 0 {
		return nil, nil, fmt.Errorf("decision assets are not supported until the asset reference syntax is confirmed: %w", llms.ErrInvalidParameters)
	}
	if req.ReferenceDate != "" {
		return nil, nil, fmt.Errorf("ReferenceDate is not supported by the OpenRouter Decisions API: %w", llms.ErrInvalidParameters)
	}
	questions, err := decisionwire.BuildQuestions(req.Questions, decisionKinds...)
	if err != nil {
		return nil, nil, err
	}
	model := o.Model
	if model == "" {
		model = c.options.DecisionModel
	}
	if model == "" {
		model = DefaultDecisionModel
	}
	body := map[string]any{"model": model, "state": req.State, "questions": questions}
	if o.SessionID != "" {
		if utf8.RuneCountInString(o.SessionID) > maxDecisionIDLength {
			return nil, nil, fmt.Errorf("session ID exceeds %d characters: %w", maxDecisionIDLength, llms.ErrInvalidParameters)
		}
		body["session_id"] = o.SessionID
	}
	if o.User != "" {
		if utf8.RuneCountInString(o.User) > maxDecisionIDLength {
			return nil, nil, fmt.Errorf("user exceeds %d characters: %w", maxDecisionIDLength, llms.ErrInvalidParameters)
		}
		body["user"] = o.User
	}
	for k, v := range o.Extra {
		if reservedDecisionKeys[k] {
			return nil, nil, fmt.Errorf("decision Extra key %q is reserved for a typed field: %w", k, llms.ErrInvalidParameters)
		}
		body[k] = v
	}
	return body, req.Questions, nil
}

func convertDecisionResponse(resp *decisionResponse, questions map[string]llms.DecisionQuestion) (*llms.DecisionResponse, error) {
	if resp.Answers == nil {
		return nil, fmt.Errorf("openrouter: decision response has no answers: %w", llms.ErrIncompleteResponse)
	}
	answers, err := decisionwire.ParseAnswers(resp.Answers, questions)
	if err != nil {
		return nil, fmt.Errorf("openrouter: %w: %w", llms.ErrIncompleteResponse, err)
	}
	metadata := map[string]any{}
	if resp.ID != "" {
		metadata["id"] = resp.ID
	}
	if resp.Provider != "" {
		metadata["provider"] = resp.Provider
	}
	if len(resp.Annotations) > 0 && string(resp.Annotations) != "null" {
		metadata["annotations"] = resp.Annotations
	}
	if resp.Usage.IsBYOK != nil {
		metadata["is_byok"] = *resp.Usage.IsBYOK
	}
	return &llms.DecisionResponse{Model: resp.Model, Answers: answers, Usage: resp.Usage.Core(), Metadata: metadata}, nil
}

var _ llms.Decider = (*Client)(nil)
