package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func elicitParams(message string) map[string]any {
	return map[string]any{
		"message":         message,
		"requestedSchema": map[string]any{"type": "object"},
	}
}

// TestElicitationAcceptReturnsContent pins the success path.
func TestElicitationAcceptReturnsContent(t *testing.T) {
	m := newMockTransport()
	var gotMessage string
	_ = mustClient(t, m, WithElicitationHandler(func(_ context.Context, req ElicitationRequest) (ElicitationResult, error) {
		gotMessage = req.Message
		return ElicitationResult{
			Action:  ElicitationAccept,
			Content: json.RawMessage(`{"name":"ada"}`),
		}, nil
	}))

	m.emitRequest(requestFrame("1", methodElicitationCreate, elicitParams("What is your name?")))

	resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if gotMessage != "What is your name?" {
		t.Errorf("handler saw message %q, want the server's prompt", gotMessage)
	}

	var result ElicitationResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Action != ElicitationAccept {
		t.Errorf("action = %q, want accept", result.Action)
	}
	if string(result.Content) != `{"name":"ada"}` {
		t.Errorf("content = %s, want the supplied data", result.Content)
	}
}

// TestElicitationNonAcceptDropsContent pins that partially-filled form data is
// not leaked when the user declines or cancels. A handler that returns both a
// decline and whatever the user had typed so far must not send the latter.
func TestElicitationNonAcceptDropsContent(t *testing.T) {
	for _, action := range []string{ElicitationDecline, ElicitationCancel} {
		t.Run(action, func(t *testing.T) {
			m := newMockTransport()
			_ = mustClient(t, m, WithElicitationHandler(func(context.Context, ElicitationRequest) (ElicitationResult, error) {
				return ElicitationResult{
					Action:  action,
					Content: json.RawMessage(`{"partial":"secret"}`),
				}, nil
			}))

			m.emitRequest(requestFrame("1", methodElicitationCreate, elicitParams("hi")))

			resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
			if resp.Error != nil {
				t.Fatalf("unexpected error: %+v", resp.Error)
			}
			var result ElicitationResult
			if err := json.Unmarshal(resp.Result, &result); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if result.Action != action {
				t.Errorf("action = %q, want %q", result.Action, action)
			}
			if len(result.Content) != 0 {
				t.Errorf("content = %s, want it dropped on a non-accept action", result.Content)
			}
		})
	}
}

// TestElicitationUnsupportedActionIsRejected pins that an unrecognized action is
// reported as an error rather than passed through. A server seeing an unknown
// action might fall back to assuming success.
func TestElicitationUnsupportedActionIsRejected(t *testing.T) {
	m := newMockTransport()
	_ = mustClient(t, m, WithElicitationHandler(func(context.Context, ElicitationRequest) (ElicitationResult, error) {
		return ElicitationResult{Action: "maybe"}, nil
	}))

	m.emitRequest(requestFrame("1", methodElicitationCreate, elicitParams("hi")))

	resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
	if resp.Error == nil || resp.Error.Code != CodeInternalError {
		t.Fatalf("response = %+v, want an InternalError for an unsupported action", resp.Error)
	}
}

// TestElicitationHandlerErrorSurfaces pins that a handler failure reaches the
// server rather than being reported as a decline.
func TestElicitationHandlerErrorSurfaces(t *testing.T) {
	m := newMockTransport()
	_ = mustClient(t, m, WithElicitationHandler(func(context.Context, ElicitationRequest) (ElicitationResult, error) {
		return ElicitationResult{}, errors.New("no UI available")
	}))

	m.emitRequest(requestFrame("1", methodElicitationCreate, elicitParams("hi")))

	resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
	if resp.Error == nil {
		t.Fatal("a handler error must surface to the server")
	}
}

// TestElicitationRejectsRequestWithoutMessage pins input validation: a prompt
// with nothing to show the user is not answerable.
func TestElicitationRejectsRequestWithoutMessage(t *testing.T) {
	m := newMockTransport()
	handlerRan := false
	_ = mustClient(t, m, WithElicitationHandler(func(context.Context, ElicitationRequest) (ElicitationResult, error) {
		handlerRan = true
		return ElicitationResult{Action: ElicitationAccept}, nil
	}))

	m.emitRequest(requestFrame("1", methodElicitationCreate, map[string]any{"message": ""}))

	resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
	if resp.Error == nil || resp.Error.Code != CodeInvalidParams {
		t.Fatalf("response = %+v, want InvalidParams", resp.Error)
	}
	if handlerRan {
		t.Error("the handler was asked to prompt with no message to show")
	}
}

// TestElicitationNotAdvertisedWhenUnconfigured pins the default-deny posture: a
// host with no way to ask a human must not be advertised as able to.
func TestElicitationNotAdvertisedWhenUnconfigured(t *testing.T) {
	m := newMockTransport()
	c := mustClient(t, m)
	if c.ClientCapabilities().Elicitation != nil {
		t.Error("elicitation advertised without a handler")
	}

	m.emitRequest(requestFrame("1", methodElicitationCreate, elicitParams("hi")))
	resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
	if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
		t.Errorf("response = %+v, want MethodNotFound", resp.Error)
	}
}

// TestElicitationAdvertisedWhenConfigured pins the positive case.
func TestElicitationAdvertisedWhenConfigured(t *testing.T) {
	m := newMockTransport()
	c := mustClient(t, m, WithElicitationHandler(func(context.Context, ElicitationRequest) (ElicitationResult, error) {
		return ElicitationResult{Action: ElicitationDecline}, nil
	}))
	if c.ClientCapabilities().Elicitation == nil {
		t.Error("elicitation handler registered but not advertised")
	}
}

// TestElicitationNotAdvertisedOnTransportsThatCannotServeIt mirrors sampling and
// roots.
func TestElicitationNotAdvertisedOnTransportsThatCannotServeIt(t *testing.T) {
	m := newMockTransport()
	m.inboundUnsupported = true

	c := mustClient(t, m, WithElicitationHandler(func(context.Context, ElicitationRequest) (ElicitationResult, error) {
		return ElicitationResult{Action: ElicitationDecline}, nil
	}))
	if c.ClientCapabilities().Elicitation != nil {
		t.Error("elicitation advertised on a transport that cannot deliver the request")
	}
}
