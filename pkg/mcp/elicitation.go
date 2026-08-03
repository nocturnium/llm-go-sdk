package mcp

import (
	"context"
	"encoding/json"
)

// Elicitation actions a client may return. Anything else is rejected rather than
// coerced, so a server is never told "accepted" because of a typo.
const (
	// ElicitationAccept means the user supplied the requested data, carried in
	// ElicitationResult.Content.
	ElicitationAccept = "accept"
	// ElicitationDecline means the user was asked and said no.
	ElicitationDecline = "decline"
	// ElicitationCancel means the user dismissed the request without deciding.
	ElicitationCancel = "cancel"
)

// ElicitationRequest is a server's ask for structured input from the user.
type ElicitationRequest struct {
	// Message is what to show the user.
	Message string `json:"message"`
	// RequestedSchema is a JSON Schema describing the shape of the data the
	// server wants back. It is server-supplied and therefore untrusted: render it
	// as a form, do not execute or trust it as a contract.
	RequestedSchema json.RawMessage `json:"requestedSchema,omitempty"`
}

// ElicitationResult is the user's answer.
type ElicitationResult struct {
	// Action is one of [ElicitationAccept], [ElicitationDecline] or
	// [ElicitationCancel].
	Action string `json:"action"`
	// Content carries the supplied data, and is meaningful only when Action is
	// accept.
	Content json.RawMessage `json:"content,omitempty"`
}

// ElicitationHandler prompts the user and returns their answer.
//
// It runs off the transport read path, so it may block on real human input. Its
// context is canceled when the client closes, so a handler waiting on a person
// should honor it rather than block forever.
//
// Unlike sampling, elicitation needs no separate approver: declining is
// expressible in the result, and the handler IS the human-in-the-loop. Never
// auto-accept a schema you did not render to a user — a server can ask for
// anything, including credentials.
type ElicitationHandler func(ctx context.Context, req ElicitationRequest) (ElicitationResult, error)

// WithElicitationHandler serves elicitation/create.
//
// With no handler registered the capability is not advertised and such requests
// are answered with MethodNotFound, so a server cannot prompt a host that has no
// way to ask anyone.
//
// Transport boundary: server-initiated requests are delivered over stdio only,
// so a handler registered on an HTTP client is never invoked.
func WithElicitationHandler(h ElicitationHandler) Option {
	return func(c *config) {
		c.elicitationHandler = h
	}
}

// buildElicitationHandler resolves the configured handler into a request
// handler, returning nil when elicitation is not configured.
//
// Unlike sampling (which requires an approver) and roots (whose URIs are
// validated), elicitation has no construction-time failure mode, so this returns
// no error rather than an always-nil one.
func buildElicitationHandler(cfg config) requestHandler {
	if cfg.elicitationHandler == nil {
		return nil
	}
	handler := cfg.elicitationHandler
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var req ElicitationRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "mcp: invalid elicitation params"}
		}
		if req.Message == "" {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "mcp: elicitation request has no message"}
		}

		result, err := handler(ctx, req)
		if err != nil {
			return nil, err
		}

		switch result.Action {
		case ElicitationAccept, ElicitationDecline, ElicitationCancel:
		default:
			// A handler that returns an unrecognized action is a bug in the host.
			// Reporting it as an internal error is safer than passing it through:
			// a server seeing an unknown action may fall back to assuming success.
			return nil, &RPCError{
				Code:    CodeInternalError,
				Message: "mcp: elicitation handler returned an unsupported action",
			}
		}
		// Content is meaningful only on accept. Dropping it otherwise stops a
		// handler from leaking partially-filled form data on a decline or cancel.
		if result.Action != ElicitationAccept {
			result.Content = nil
		}
		return result, nil
	}
}
