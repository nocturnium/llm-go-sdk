package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// SamplingMessage is one turn of the conversation a server asks the host to
// complete. Role is "user" or "assistant".
type SamplingMessage struct {
	Role    string       `json:"role"`
	Content ContentBlock `json:"content"`
}

// ModelHint is a server's suggested model name. It is advisory only.
type ModelHint struct {
	Name string `json:"name,omitempty"`
}

// ModelPreferences is the server's advisory guidance on model selection.
//
// It is deliberately NOT honored: model choice belongs to the host, which is the
// party paying for the tokens. A server that could steer the host onto an
// arbitrary model could also steer it onto an expensive one. Override the model
// per request with the llms.CallOption values passed to [WithSamplingLLM].
type ModelPreferences struct {
	Hints                []ModelHint `json:"hints,omitempty"`
	CostPriority         float64     `json:"costPriority,omitempty"`
	SpeedPriority        float64     `json:"speedPriority,omitempty"`
	IntelligencePriority float64     `json:"intelligencePriority,omitempty"`
}

// SamplingRequest is a server's ask for the host to run an LLM completion.
// Serving it spends the host's tokens, which is why it is gated on approval —
// see [SamplingApprover].
type SamplingRequest struct {
	Messages         []SamplingMessage `json:"messages"`
	ModelPreferences *ModelPreferences `json:"modelPreferences,omitempty"`
	SystemPrompt     string            `json:"systemPrompt,omitempty"`
	// IncludeContext is accepted and IGNORED. Honoring "allServers" would splice
	// other MCP servers' conversation into a request made by this one, leaking
	// context across trust boundaries.
	IncludeContext string          `json:"includeContext,omitempty"`
	Temperature    float64         `json:"temperature,omitempty"`
	MaxTokens      int             `json:"maxTokens"`
	StopSequences  []string        `json:"stopSequences,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
}

// SamplingResult is the completion returned to the server.
type SamplingResult struct {
	Role       string       `json:"role"`
	Content    ContentBlock `json:"content"`
	Model      string       `json:"model"`
	StopReason string       `json:"stopReason,omitempty"`
}

// SamplingHandler serves a sampling request. Use it to plug in a completion
// source other than an [llms.LLM]; for the common case use [WithSamplingLLM].
//
// A handler is still gated by the approver: registering one without
// [WithSamplingApprover] is a construction error.
type SamplingHandler func(ctx context.Context, req SamplingRequest) (SamplingResult, error)

// SamplingApproval is the host's decision on a sampling request. The zero value
// is a denial, so a partially-filled or forgotten return refuses rather than
// approves.
type SamplingApproval struct {
	// Approved gates the call. When false, no LLM is invoked at all.
	Approved bool
	// Reason is returned to the server on denial. Keep it free of host internals.
	Reason string
	// MaxTokens, when greater than zero, caps the completion below the server's
	// requested MaxTokens. It can only lower the cap, never raise it.
	MaxTokens int
}

// SamplingApprover decides whether a server's sampling request may spend the
// host's tokens.
//
// It is called BEFORE any LLM invocation, on the inbound-request worker rather
// than the transport read path, so it may block on real human input without
// stalling the connection. Its context is canceled when the client closes.
//
// There is no default approver. A client configured to serve sampling without
// one fails to construct: an MCP server must never be able to spend the host's
// tokens without the host having said yes. Use [ApproveAllSampling] to opt out
// explicitly.
type SamplingApprover func(ctx context.Context, req SamplingRequest) SamplingApproval

// ApproveAllSampling approves every sampling request.
//
// It exists so non-interactive hosts — tests, or servers you fully control — can
// opt out of consent EXPLICITLY and greppably. Do not use it with a server you do
// not control: it hands that server your token budget.
func ApproveAllSampling() SamplingApprover {
	return func(_ context.Context, _ SamplingRequest) SamplingApproval {
		return SamplingApproval{Approved: true}
	}
}

// WithSamplingApprover installs the consent gate for sampling requests. It is
// required whenever sampling is served; see [SamplingApprover].
func WithSamplingApprover(a SamplingApprover) Option {
	return func(c *config) {
		c.samplingApprover = a
	}
}

// WithSamplingHandler serves sampling/createMessage with a custom handler.
//
// An approver is still required. For the common case of answering from a model
// this SDK already drives, use [WithSamplingLLM].
//
// Transport boundary: server-initiated requests are delivered over the stdio
// transport only. The Streamable HTTP transport has no standalone SSE listener
// to receive them and no path to send a response frame back, so a handler
// registered on an HTTP client is never invoked and the capability is not
// advertised.
func WithSamplingHandler(h SamplingHandler) Option {
	return func(c *config) {
		c.samplingHandler = h
	}
}

// WithSamplingLLM serves sampling/createMessage from an [llms.LLM].
//
// This is the capability that makes an MCP host useful to a server: the server
// asks for a completion and the host answers with a model it already owns,
// under its own credentials and its own budget. Which is exactly why it is
// gated — see [SamplingApprover].
//
// The opts are applied AFTER the server's requested parameters, so the host
// always wins: pass llms.WithModel to force a cheaper model regardless of what
// the server asked for.
//
// An approver is required; constructing a client with this option and no
// [WithSamplingApprover] returns an error.
func WithSamplingLLM(llm llms.LLM, opts ...llms.CallOption) Option {
	return func(c *config) {
		c.samplingLLM = llm
		c.samplingLLMOptions = opts
	}
}

// errSamplingWithoutApprover explains the one wiring mistake that would let a
// server spend the host's tokens unattended.
var errSamplingWithoutApprover = fmt.Errorf(
	"mcp: sampling is configured without an approver; pass mcp.WithSamplingApprover " +
		"(or mcp.ApproveAllSampling() to explicitly opt out of consent)")

// buildSamplingHandler resolves the configured sampling source into a request
// handler, applying the consent gate. It returns (nil, nil) when sampling is not
// configured at all.
func buildSamplingHandler(cfg config) (requestHandler, error) {
	handler := cfg.samplingHandler
	if handler == nil && cfg.samplingLLM != nil {
		handler = samplingFromLLM(cfg.samplingLLM, cfg.samplingLLMOptions)
	}
	if handler == nil {
		if cfg.samplingApprover != nil {
			// An approver with nothing to approve is a wiring mistake in the safe
			// direction, so it is not fatal — but it means sampling is not served.
			return nil, nil
		}
		return nil, nil
	}
	// Fail at wire-up, not at request time. A misconfiguration should surface when
	// the client is built, not at 3am when a server first asks to sample.
	if cfg.samplingApprover == nil {
		return nil, errSamplingWithoutApprover
	}
	return gateSampling(handler, cfg.samplingApprover), nil
}

// gateSampling wraps a sampling handler in the approval check.
func gateSampling(handler SamplingHandler, approve SamplingApprover) requestHandler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var req SamplingRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "mcp: invalid sampling params"}
		}
		// Validate before consulting the approver so a human is never asked to
		// approve a nonsensical request.
		if len(req.Messages) == 0 {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "mcp: sampling request has no messages"}
		}
		if req.MaxTokens <= 0 {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "mcp: sampling request must set maxTokens"}
		}

		approval := approve(ctx, req)
		if !approval.Approved {
			message := "mcp: sampling request was not approved"
			if approval.Reason != "" {
				message += ": " + approval.Reason
			}
			// CodeInvalidRequest, not InternalError: this is a deliberate refusal,
			// and the server should not retry it.
			return nil, &RPCError{Code: CodeInvalidRequest, Message: message}
		}
		// An approval may only tighten the budget the server asked for.
		if approval.MaxTokens > 0 && approval.MaxTokens < req.MaxTokens {
			req.MaxTokens = approval.MaxTokens
		}

		result, err := handler(ctx, req)
		if err != nil {
			return nil, err
		}
		return result, nil
	}
}

// samplingFromLLM adapts an llms.LLM into a SamplingHandler.
func samplingFromLLM(llm llms.LLM, extra []llms.CallOption) SamplingHandler {
	return func(ctx context.Context, req SamplingRequest) (SamplingResult, error) {
		messages, err := samplingMessages(req)
		if err != nil {
			return SamplingResult{}, err
		}

		// Server-requested parameters first, host options last, so a host option
		// always overrides what the server asked for.
		options := []llms.CallOption{llms.WithMaxTokens(req.MaxTokens)}
		if req.Temperature > 0 {
			options = append(options, llms.WithTemperature(req.Temperature))
		}
		if len(req.StopSequences) > 0 {
			options = append(options, llms.WithStopWords(req.StopSequences))
		}
		options = append(options, extra...)

		resp, err := llm.GenerateContent(ctx, messages, options...)
		if err != nil {
			return SamplingResult{}, fmt.Errorf("mcp: sampling completion failed: %w", err)
		}
		return SamplingResult{
			Role:       "assistant",
			Content:    ContentBlock{Type: "text", Text: resp.Content},
			Model:      llm.Model(),
			StopReason: string(resp.FinishReason),
		}, nil
	}
}

// samplingMessages converts a sampling request's turns into llms messages,
// prepending the system prompt when present.
func samplingMessages(req SamplingRequest) ([]llms.Message, error) {
	messages := make([]llms.Message, 0, len(req.Messages)+1)
	if req.SystemPrompt != "" {
		messages = append(messages, llms.Message{Role: llms.RoleSystem, Content: req.SystemPrompt})
	}
	for _, m := range req.Messages {
		role, err := samplingRole(m.Role)
		if err != nil {
			return nil, err
		}
		msg := llms.Message{Role: role}
		switch m.Content.Type {
		case "text":
			msg.Content = m.Content.Text
		case "image":
			if m.Content.Data == "" || m.Content.MimeType == "" {
				return nil, &RPCError{Code: CodeInvalidParams, Message: "mcp: image content requires data and mimeType"}
			}
			msg.Parts = []llms.ContentPart{{
				Type: llms.PartTypeImage,
				Image: &llms.ImageContent{
					Source:    llms.ImageSourceBase64,
					MediaType: m.Content.MimeType,
					Data:      m.Content.Data,
				},
			}}
		default:
			return nil, &RPCError{
				Code:    CodeInvalidParams,
				Message: "mcp: unsupported sampling content type: " + m.Content.Type,
			}
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

// samplingRole maps an MCP role onto an llms role, rejecting anything else
// rather than silently coercing it.
func samplingRole(role string) (llms.Role, error) {
	switch role {
	case "user":
		return llms.RoleUser, nil
	case "assistant":
		return llms.RoleAssistant, nil
	default:
		return "", &RPCError{Code: CodeInvalidParams, Message: "mcp: unsupported sampling role: " + role}
	}
}
