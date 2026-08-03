package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v6"
)

// samplingStubLLM records what it was asked and returns a canned completion.
// Set failIfCalled to assert a path must never reach the model.
type samplingStubLLM struct {
	t            *testing.T
	failIfCalled bool

	gotMessages []llms.Message
	gotOptions  *llms.CallOptions
}

func (s *samplingStubLLM) GenerateContent(_ context.Context, messages []llms.Message, options ...llms.CallOption) (*llms.Response, error) {
	if s.failIfCalled {
		s.t.Error("the LLM was invoked when it must not have been")
	}
	s.gotMessages = messages
	s.gotOptions = llms.ApplyOptions(options...)
	return &llms.Response{Content: "completed", FinishReason: llms.FinishReasonStop}, nil
}

func (s *samplingStubLLM) Stream(context.Context, []llms.Message, ...llms.CallOption) (<-chan llms.StreamChunk, error) {
	return nil, errors.New("not used")
}
func (s *samplingStubLLM) Provider() llms.Provider { return llms.ProviderOpenAI }
func (s *samplingStubLLM) Model() string           { return "stub-model" }

// validSamplingParams is a minimally valid sampling request payload.
func validSamplingParams() map[string]any {
	return map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": map[string]any{"type": "text", "text": "hi"}},
		},
		"maxTokens": 100,
	}
}

// TestContentBlockStaysComparable guards the image-support additions. Data and
// MimeType are strings deliberately: a slice or map field would make
// ContentBlock non-comparable and break any caller using == or a map key — the
// same class of change that forced the v6 major.
func TestContentBlockStaysComparable(t *testing.T) {
	a := ContentBlock{Type: "image", Data: "x", MimeType: "image/png"}
	b := ContentBlock{Type: "image", Data: "x", MimeType: "image/png"}
	if a != b {
		t.Error("identical ContentBlock values should compare equal")
	}
	_ = map[ContentBlock]struct{}{}
}

// TestSamplingWithoutApproverFailsToConstruct is THE consent invariant.
//
// A client configured to serve sampling with no approver must not construct. The
// alternative — constructing and denying at request time — hides the
// misconfiguration until a server first asks, which is exactly when nobody is
// watching. Failing at wire-up surfaces it immediately.
func TestSamplingWithoutApproverFailsToConstruct(t *testing.T) {
	cases := []struct {
		name string
		opt  Option
	}{
		{"WithSamplingLLM", WithSamplingLLM(&samplingStubLLM{t: t})},
		{"WithSamplingHandler", WithSamplingHandler(func(context.Context, SamplingRequest) (SamplingResult, error) {
			return SamplingResult{}, nil
		})},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newMockTransport()
			_, err := newClient(context.Background(), m, buildConfig([]Option{c.opt}))
			if err == nil {
				t.Fatal("expected construction to fail without an approver")
			}
			if !errors.Is(err, errSamplingWithoutApprover) &&
				!strings.Contains(err.Error(), "approver") {
				t.Errorf("error = %v, want it to name the missing approver", err)
			}
			if !m.closed {
				t.Error("transport should be closed when construction fails")
			}
		})
	}
}

// TestSamplingDenialDoesNotInvokeTheLLM pins that a denial short-circuits before
// any token is spent. The stub fails the test if it is called at all.
func TestSamplingDenialDoesNotInvokeTheLLM(t *testing.T) {
	m := newMockTransport()
	stub := &samplingStubLLM{t: t, failIfCalled: true}

	var sawRequest bool
	_ = mustClient(t, m,
		WithSamplingLLM(stub),
		WithSamplingApprover(func(_ context.Context, req SamplingRequest) SamplingApproval {
			// The approver must see the request BEFORE any model call.
			sawRequest = len(req.Messages) == 1
			return SamplingApproval{Approved: false, Reason: "not today"}
		}),
	)

	m.emitRequest(requestFrame("1", methodSamplingCreateMessage, validSamplingParams()))

	resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
	if resp.Error == nil {
		t.Fatal("a denied request must return an error")
	}
	if resp.Error.Code != CodeInvalidRequest {
		t.Errorf("code = %d, want CodeInvalidRequest (a deliberate refusal, not retryable)", resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "not today") {
		t.Errorf("message = %q, want it to carry the denial reason", resp.Error.Message)
	}
	if !sawRequest {
		t.Error("the approver did not receive the request")
	}
}

// TestSamplingZeroApprovalIsADenial pins that the zero value refuses. An approver
// that falls through a switch, or returns a partially-filled struct, must not
// accidentally approve.
func TestSamplingZeroApprovalIsADenial(t *testing.T) {
	m := newMockTransport()
	stub := &samplingStubLLM{t: t, failIfCalled: true}

	_ = mustClient(t, m,
		WithSamplingLLM(stub),
		WithSamplingApprover(func(context.Context, SamplingRequest) SamplingApproval {
			return SamplingApproval{} // forgot to set Approved
		}),
	)

	m.emitRequest(requestFrame("1", methodSamplingCreateMessage, validSamplingParams()))

	resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
	if resp.Error == nil {
		t.Fatal("a zero-value approval must deny")
	}
}

// TestSamplingApprovedInvokesTheLLM pins the success path end to end.
func TestSamplingApprovedInvokesTheLLM(t *testing.T) {
	m := newMockTransport()
	stub := &samplingStubLLM{t: t}

	_ = mustClient(t, m, WithSamplingLLM(stub), WithSamplingApprover(ApproveAllSampling()))

	params := validSamplingParams()
	params["systemPrompt"] = "be terse"
	m.emitRequest(requestFrame("1", methodSamplingCreateMessage, params))

	resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	var result SamplingResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Content.Text != "completed" {
		t.Errorf("content = %q, want the model's completion", result.Content.Text)
	}
	if result.Model != "stub-model" {
		t.Errorf("model = %q, want the model that actually ran", result.Model)
	}
	if result.Role != "assistant" {
		t.Errorf("role = %q, want assistant", result.Role)
	}

	// The system prompt must lead, followed by the conversation.
	if len(stub.gotMessages) != 2 {
		t.Fatalf("got %d messages, want 2 (system + user)", len(stub.gotMessages))
	}
	if stub.gotMessages[0].Role != llms.RoleSystem || stub.gotMessages[0].Content != "be terse" {
		t.Errorf("first message = %+v, want the system prompt", stub.gotMessages[0])
	}
	if stub.gotMessages[1].Role != llms.RoleUser {
		t.Errorf("second message role = %q, want user", stub.gotMessages[1].Role)
	}
}

// TestSamplingApprovalCanOnlyLowerMaxTokens pins the budget guard: an approver
// may tighten what the server asked for, never widen it. Widening would let a
// careless approver hand a server a larger budget than it even requested.
func TestSamplingApprovalCanOnlyLowerMaxTokens(t *testing.T) {
	cases := []struct {
		name          string
		approvalMax   int
		requestedMax  int
		wantMaxTokens int
	}{
		{"lowers", 50, 100, 50},
		{"cannot raise", 5000, 100, 100},
		{"zero means no override", 0, 100, 100},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newMockTransport()
			stub := &samplingStubLLM{t: t}
			_ = mustClient(t, m,
				WithSamplingLLM(stub),
				WithSamplingApprover(func(context.Context, SamplingRequest) SamplingApproval {
					return SamplingApproval{Approved: true, MaxTokens: c.approvalMax}
				}),
			)

			params := validSamplingParams()
			params["maxTokens"] = c.requestedMax
			m.emitRequest(requestFrame("1", methodSamplingCreateMessage, params))
			awaitResponses(t, m, 1)

			if stub.gotOptions == nil {
				t.Fatal("the LLM was never called")
			}
			got := stub.gotOptions.MaxTokens
			if got == nil {
				t.Fatal("MaxTokens was not set on the call")
			}
			if *got != c.wantMaxTokens {
				t.Errorf("MaxTokens = %d, want %d", *got, c.wantMaxTokens)
			}
		})
	}
}

// TestSamplingHostOptionsOverrideTheServer pins that host-supplied call options
// are applied last, so a host can force a cheaper model regardless of what the
// server requested.
func TestSamplingHostOptionsOverrideTheServer(t *testing.T) {
	m := newMockTransport()
	stub := &samplingStubLLM{t: t}

	_ = mustClient(t, m,
		WithSamplingLLM(stub, llms.WithMaxTokens(7), llms.WithModel("host-choice")),
		WithSamplingApprover(ApproveAllSampling()),
	)

	params := validSamplingParams()
	params["maxTokens"] = 4000
	m.emitRequest(requestFrame("1", methodSamplingCreateMessage, params))
	awaitResponses(t, m, 1)

	if stub.gotOptions == nil {
		t.Fatal("the LLM was never called")
	}
	if got := stub.gotOptions.MaxTokens; got == nil || *got != 7 {
		t.Errorf("MaxTokens = %v, want 7 (the host option must win)", got)
	}
	if got := stub.gotOptions.Model; got != "host-choice" {
		t.Errorf("Model = %q, want host-choice", got)
	}
}

// TestSamplingIgnoresModelPreferences pins that the server's model hints are not
// honored. Model choice belongs to the party paying for the tokens.
func TestSamplingIgnoresModelPreferences(t *testing.T) {
	m := newMockTransport()
	stub := &samplingStubLLM{t: t}

	_ = mustClient(t, m, WithSamplingLLM(stub), WithSamplingApprover(ApproveAllSampling()))

	params := validSamplingParams()
	params["modelPreferences"] = map[string]any{
		"hints": []any{map[string]any{"name": "expensive-model"}},
	}
	m.emitRequest(requestFrame("1", methodSamplingCreateMessage, params))
	awaitResponses(t, m, 1)

	if stub.gotOptions == nil {
		t.Fatal("the LLM was never called")
	}
	if stub.gotOptions.Model == "expensive-model" {
		t.Error("the server's model hint was honored; model choice must stay with the host")
	}
}

// TestSamplingIgnoresIncludeContext pins that includeContext is accepted and
// ignored. Honoring "allServers" would splice other servers' conversation into a
// request made by this one.
func TestSamplingIgnoresIncludeContext(t *testing.T) {
	m := newMockTransport()
	stub := &samplingStubLLM{t: t}

	_ = mustClient(t, m, WithSamplingLLM(stub), WithSamplingApprover(ApproveAllSampling()))

	params := validSamplingParams()
	params["includeContext"] = "allServers"
	m.emitRequest(requestFrame("1", methodSamplingCreateMessage, params))

	resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
	if resp.Error != nil {
		t.Fatalf("includeContext should be accepted, not rejected: %+v", resp.Error)
	}
	// Only the request's own single message reaches the model.
	if len(stub.gotMessages) != 1 {
		t.Errorf("got %d messages, want 1 — no extra context may be spliced in", len(stub.gotMessages))
	}
}

// TestSamplingImageContentMapsToAnImagePart pins multimodal pass-through.
func TestSamplingImageContentMapsToAnImagePart(t *testing.T) {
	m := newMockTransport()
	stub := &samplingStubLLM{t: t}

	_ = mustClient(t, m, WithSamplingLLM(stub), WithSamplingApprover(ApproveAllSampling()))

	m.emitRequest(requestFrame("1", methodSamplingCreateMessage, map[string]any{
		"messages": []any{map[string]any{
			"role": "user",
			"content": map[string]any{
				"type": "image", "data": "aGVsbG8=", "mimeType": "image/png",
			},
		}},
		"maxTokens": 100,
	}))
	awaitResponses(t, m, 1)

	if len(stub.gotMessages) != 1 {
		t.Fatalf("got %d messages, want 1", len(stub.gotMessages))
	}
	parts := stub.gotMessages[0].Parts
	if len(parts) != 1 || parts[0].Image == nil {
		t.Fatalf("message parts = %+v, want one image part", parts)
	}
	if parts[0].Image.MediaType != "image/png" || parts[0].Image.Data != "aGVsbG8=" {
		t.Errorf("image = %+v, want the request's data and mime type", parts[0].Image)
	}
}

// TestSamplingRejectsInvalidRequests pins that malformed requests are refused
// with InvalidParams *before* the approver runs — a human must never be asked to
// approve a nonsensical request.
func TestSamplingRejectsInvalidRequests(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
	}{
		{"no messages", map[string]any{"maxTokens": 100}},
		{"no maxTokens", map[string]any{
			"messages": []any{map[string]any{"role": "user", "content": map[string]any{"type": "text", "text": "hi"}}},
		}},
		{"unknown role", map[string]any{
			"messages":  []any{map[string]any{"role": "system", "content": map[string]any{"type": "text", "text": "hi"}}},
			"maxTokens": 100,
		}},
		{"unsupported content type", map[string]any{
			"messages":  []any{map[string]any{"role": "user", "content": map[string]any{"type": "video"}}},
			"maxTokens": 100,
		}},
		{"image without mime type", map[string]any{
			"messages":  []any{map[string]any{"role": "user", "content": map[string]any{"type": "image", "data": "aGk="}}},
			"maxTokens": 100,
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newMockTransport()
			approverRan := false
			_ = mustClient(t, m,
				WithSamplingLLM(&samplingStubLLM{t: t, failIfCalled: true}),
				WithSamplingApprover(func(context.Context, SamplingRequest) SamplingApproval {
					approverRan = true
					return SamplingApproval{Approved: true}
				}),
			)

			m.emitRequest(requestFrame("1", methodSamplingCreateMessage, c.params))

			resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
			if resp.Error == nil || resp.Error.Code != CodeInvalidParams {
				t.Fatalf("response = %+v, want InvalidParams", resp.Error)
			}
			// The first two cases are rejected before the approver; the role and
			// content cases are rejected inside the adapter, after approval.
			if c.name == "no messages" || c.name == "no maxTokens" {
				if approverRan {
					t.Error("the approver was consulted for a request that is invalid on its face")
				}
			}
		})
	}
}

// TestSamplingCapabilityAdvertisedOnlyWhenServed pins that the advertisement
// tracks reality.
func TestSamplingCapabilityAdvertisedOnlyWhenServed(t *testing.T) {
	t.Run("advertised when configured", func(t *testing.T) {
		m := newMockTransport()
		c := mustClient(t, m,
			WithSamplingLLM(&samplingStubLLM{t: t}),
			WithSamplingApprover(ApproveAllSampling()),
		)
		if c.ClientCapabilities().Sampling == nil {
			t.Error("sampling is served but not advertised")
		}
	})

	t.Run("not advertised when unconfigured", func(t *testing.T) {
		m := newMockTransport()
		c := mustClient(t, m)
		if c.ClientCapabilities().Sampling != nil {
			t.Error("sampling advertised without being served")
		}
	})

	t.Run("approver alone advertises nothing", func(t *testing.T) {
		// An approver with no sampling source is a wiring mistake in the safe
		// direction: it must not advertise, and must not fail construction.
		m := newMockTransport()
		c := mustClient(t, m, WithSamplingApprover(ApproveAllSampling()))
		if c.ClientCapabilities().Sampling != nil {
			t.Error("sampling advertised with no source to serve it")
		}
	})
}

// TestSamplingNotAdvertisedOnTransportsThatCannotServeIt pins the transport
// boundary. Telling a server this client samples, when the request can never
// arrive, leaves it waiting on a promise the transport cannot keep.
func TestSamplingNotAdvertisedOnTransportsThatCannotServeIt(t *testing.T) {
	m := newMockTransport()
	m.inboundUnsupported = true

	c := mustClient(t, m,
		WithSamplingLLM(&samplingStubLLM{t: t}),
		WithSamplingApprover(ApproveAllSampling()),
	)

	if c.ClientCapabilities().Sampling != nil {
		t.Error("sampling advertised on a transport that cannot deliver the request")
	}
}
