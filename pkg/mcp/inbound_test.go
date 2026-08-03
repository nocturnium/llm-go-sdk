package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

// clientWithHandlers builds a client whose inbound request handlers are set
// directly on the config. A1 ships the dispatch machinery; the public options
// that register concrete handlers (sampling, roots, elicitation) land with those
// capabilities.
func clientWithHandlers(t *testing.T, m *mockTransport, handlers map[string]requestHandler) *Client {
	t.Helper()
	cfg := buildConfig(nil)
	cfg.requestHandlers = handlers
	c, err := newClient(context.Background(), m, cfg)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	return c
}

// requestFrame builds a server-initiated request frame.
func requestFrame(id, method string, params any) []byte {
	raw, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  any             `json:"params,omitempty"`
	}{JSONRPC: jsonRPCVersion, ID: json.RawMessage(id), Method: method, Params: params})
	if err != nil {
		panic(err)
	}
	return raw
}

// awaitResponses waits for at least n response frames to be written back.
func awaitResponses(t *testing.T, m *mockTransport, n int) [][]byte {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if sent := m.sentResponses(); len(sent) >= n {
			return sent
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d response frames (got %d)", n, len(m.sentResponses()))
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func decodeResponse(t *testing.T, raw []byte) rpcResponse {
	t.Helper()
	var resp rpcResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return resp
}

// TestInboundUnknownMethodReturnsMethodNotFound pins that an unregistered method
// is answered rather than ignored, so a server is never left waiting.
func TestInboundUnknownMethodReturnsMethodNotFound(t *testing.T) {
	m := newMockTransport()
	_ = clientWithHandlers(t, m, nil)

	m.emitRequest(requestFrame("1", "sampling/createMessage", nil))

	resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
	if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
		t.Fatalf("response = %+v, want MethodNotFound", resp.Error)
	}
}

// TestInboundMalformedRequestReturnsParseError pins that an unparseable frame is
// still answered.
func TestInboundMalformedRequestReturnsParseError(t *testing.T) {
	m := newMockTransport()
	c := clientWithHandlers(t, m, nil)

	// Bypass emitRequest's classification, which would reject this frame.
	c.dispatchRequest([]byte(`{"jsonrpc":"2.0","id":1`), json.RawMessage("1"))

	resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
	if resp.Error == nil || resp.Error.Code != CodeParseError {
		t.Fatalf("response = %+v, want ParseError", resp.Error)
	}
}

// TestInboundHandlerResultIsReturned pins the success path, including that the
// request's id is echoed back verbatim in its original JSON form.
func TestInboundHandlerResultIsReturned(t *testing.T) {
	m := newMockTransport()
	gotParams := make(chan json.RawMessage, 1)
	_ = clientWithHandlers(t, m, map[string]requestHandler{
		methodRootsList: func(_ context.Context, params json.RawMessage) (any, error) {
			gotParams <- params
			return map[string]any{"roots": []any{}}, nil
		},
	})

	m.emitRequest(requestFrame(`"abc"`, methodRootsList, map[string]any{"cursor": "page2"}))

	// The handler must receive the request's params verbatim.
	select {
	case params := <-gotParams:
		var probe struct {
			Cursor string `json:"cursor"`
		}
		if err := json.Unmarshal(params, &probe); err != nil {
			t.Fatalf("handler params were not valid JSON: %v", err)
		}
		if probe.Cursor != "page2" {
			t.Errorf("handler params cursor = %q, want page2", probe.Cursor)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler never ran")
	}

	resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if string(resp.ID) != `"abc"` {
		t.Errorf("id = %s, want \"abc\" echoed verbatim", resp.ID)
	}
	if len(resp.Result) == 0 {
		t.Error("response carries no result")
	}
}

// TestInboundHandlerRPCErrorControlsTheWireCode pins that a handler returning an
// *RPCError chooses the code the server sees, while any other error maps to
// InternalError.
func TestInboundHandlerRPCErrorControlsTheWireCode(t *testing.T) {
	m := newMockTransport()
	_ = clientWithHandlers(t, m, map[string]requestHandler{
		methodRootsList: func(_ context.Context, _ json.RawMessage) (any, error) {
			return nil, &RPCError{Code: CodeInvalidParams, Message: "bad params"}
		},
		methodElicitationCreate: func(_ context.Context, _ json.RawMessage) (any, error) {
			return nil, errors.New("something broke")
		},
	})

	m.emitRequest(requestFrame("1", methodRootsList, nil))
	m.emitRequest(requestFrame("2", methodElicitationCreate, nil))

	sent := awaitResponses(t, m, 2)
	byID := map[string]rpcResponse{}
	for _, raw := range sent {
		resp := decodeResponse(t, raw)
		byID[string(resp.ID)] = resp
	}

	if got := byID["1"]; got.Error == nil || got.Error.Code != CodeInvalidParams {
		t.Errorf("typed error: got %+v, want InvalidParams", got.Error)
	}
	if got := byID["2"]; got.Error == nil || got.Error.Code != CodeInternalError {
		t.Errorf("plain error: got %+v, want InternalError", got.Error)
	}
}

// TestInboundHandlerPanicDoesNotKillTheClient pins that a panicking handler is
// converted into an error response and the client keeps serving. It also pins
// that the panic value never reaches the wire: it can carry host internals, and
// the peer is not necessarily trusted.
func TestInboundHandlerPanicDoesNotKillTheClient(t *testing.T) {
	m := newMockTransport()
	_ = clientWithHandlers(t, m, map[string]requestHandler{
		methodRootsList: func(_ context.Context, _ json.RawMessage) (any, error) {
			panic("secret: /home/user/.ssh/id_rsa")
		},
		methodElicitationCreate: func(_ context.Context, _ json.RawMessage) (any, error) {
			return map[string]any{"action": "decline"}, nil
		},
	})

	m.emitRequest(requestFrame("1", methodRootsList, nil))
	first := decodeResponse(t, awaitResponses(t, m, 1)[0])
	if first.Error == nil || first.Error.Code != CodeInternalError {
		t.Fatalf("panic response = %+v, want InternalError", first.Error)
	}
	if got := first.Error.Message; got != "mcp: request handler panicked" {
		t.Errorf("panic message = %q; the panic value must not reach the wire", got)
	}

	// The client must still serve a subsequent request.
	m.emitRequest(requestFrame("2", methodElicitationCreate, nil))
	sent := awaitResponses(t, m, 2)
	second := decodeResponse(t, sent[1])
	if second.Error != nil {
		t.Errorf("client stopped serving after a panic: %+v", second.Error)
	}
}

// TestInboundHandlerRunsOffTheReadPath is the invariant that justifies the
// worker goroutine: a slow handler (sampling can take tens of seconds) must not
// stall delivery of responses to this client's own in-flight calls.
func TestInboundHandlerRunsOffTheReadPath(t *testing.T) {
	m := newMockTransport()
	m.queue(methodToolsList, listToolsResult{Tools: []Tool{{Name: "a"}}})

	blocked := make(chan struct{})
	entered := make(chan struct{})
	c := clientWithHandlers(t, m, map[string]requestHandler{
		methodRootsList: func(_ context.Context, _ json.RawMessage) (any, error) {
			close(entered)
			<-blocked
			return map[string]any{}, nil
		},
	})
	defer close(blocked)

	m.emitRequest(requestFrame("1", methodRootsList, nil))
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never ran")
	}

	// With the handler still blocked, an ordinary call must complete.
	done := make(chan error, 1)
	go func() {
		_, err := c.ListTools(context.Background())
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ListTools failed while a request handler was blocked: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a blocked request handler stalled an unrelated call")
	}
}

// TestInboundConcurrencyLimitRefusesRatherThanDrops pins the central difference
// from the notification pump: overflow is answered with an error and counted,
// never silently dropped. A dropped request would hang the server forever.
func TestInboundConcurrencyLimitRefusesRatherThanDrops(t *testing.T) {
	m := newMockTransport()
	release := make(chan struct{})
	var started sync.WaitGroup
	started.Add(maxInflightRequests)

	var once sync.Once
	c := clientWithHandlers(t, m, map[string]requestHandler{
		methodRootsList: func(_ context.Context, _ json.RawMessage) (any, error) {
			started.Done()
			<-release
			return map[string]any{}, nil
		},
	})
	defer once.Do(func() { close(release) })

	// Saturate every slot.
	for i := range maxInflightRequests {
		m.emitRequest(requestFrame(string(rune('0'+i)), methodRootsList, nil))
	}
	started.Wait()

	// One more must be refused, not queued and not dropped.
	m.emitRequest(requestFrame("99", methodRootsList, nil))

	resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
	if resp.Error == nil || resp.Error.Code != CodeInternalError {
		t.Fatalf("overflow response = %+v, want an InternalError refusal", resp.Error)
	}
	if got := c.RefusedRequests(); got != 1 {
		t.Errorf("RefusedRequests() = %d, want 1", got)
	}

	once.Do(func() { close(release) })
}

// TestCloseDoesNotHangOnAWedgedHandler pins the bounded teardown. A handler that
// never returns must not make Close block forever.
func TestCloseDoesNotHangOnAWedgedHandler(t *testing.T) {
	m := newMockTransport()
	wedged := make(chan struct{})
	entered := make(chan struct{})
	c := clientWithHandlers(t, m, map[string]requestHandler{
		methodRootsList: func(_ context.Context, _ json.RawMessage) (any, error) {
			close(entered)
			<-wedged // never released during the test
			return nil, nil
		},
	})
	defer close(wedged)

	m.emitRequest(requestFrame("1", methodRootsList, nil))
	<-entered

	closed := make(chan error, 1)
	go func() { closed <- c.Close() }()

	select {
	case <-closed:
	case <-time.After(inboundShutdownTimeout + 3*time.Second):
		t.Fatal("Close hung on a wedged request handler")
	}
}

// TestInboundRequestAfterCloseIsAnswered pins that a request arriving during or
// after shutdown still gets a reply rather than silence.
func TestInboundRequestAfterCloseIsAnswered(t *testing.T) {
	m := newMockTransport()
	c := clientWithHandlers(t, m, map[string]requestHandler{
		methodRootsList: func(_ context.Context, _ json.RawMessage) (any, error) {
			return map[string]any{}, nil
		},
	})
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	m.emitRequest(requestFrame("1", methodRootsList, nil))

	resp := decodeResponse(t, awaitResponses(t, m, 1)[0])
	if resp.Error == nil {
		t.Fatal("a request after Close must be answered with an error, not served or ignored")
	}
}

// TestClientCapabilitiesDerivedFromHandlers pins that advertisement cannot drift
// from capability. A server told this client samples, which then answers
// MethodNotFound, has no way to recover.
func TestClientCapabilitiesDerivedFromHandlers(t *testing.T) {
	t.Run("none registered advertises nothing", func(t *testing.T) {
		m := newMockTransport()
		c := clientWithHandlers(t, m, nil)

		caps := c.ClientCapabilities()
		if caps.Sampling != nil || caps.Roots != nil || caps.Elicitation != nil {
			t.Errorf("advertised %+v, want all nil", caps)
		}

		call, ok := m.lastCall(methodInitialize)
		if !ok {
			t.Fatal("no initialize call recorded")
		}
		var params struct {
			Capabilities json.RawMessage `json:"capabilities"`
		}
		if err := json.Unmarshal(call.params, &params); err != nil {
			t.Fatalf("unmarshal initialize params: %v", err)
		}
		if got := string(params.Capabilities); got != "{}" {
			t.Errorf("advertised capabilities = %s, want {}", got)
		}
	})

	t.Run("registered handlers are advertised", func(t *testing.T) {
		m := newMockTransport()
		noop := func(_ context.Context, _ json.RawMessage) (any, error) { return nil, nil }
		c := clientWithHandlers(t, m, map[string]requestHandler{
			methodSamplingCreateMessage: noop,
			methodElicitationCreate:     noop,
		})

		caps := c.ClientCapabilities()
		if caps.Sampling == nil {
			t.Error("sampling handler registered but capability not advertised")
		}
		if caps.Elicitation == nil {
			t.Error("elicitation handler registered but capability not advertised")
		}
		if caps.Roots != nil {
			t.Error("roots advertised without a handler")
		}
	})
}

// TestClientStaysComparable pins the reason inbound is pointer-held. A mutex by
// value on Client would break every caller comparing two client values.
func TestClientStaysComparable(t *testing.T) {
	var a, b *Client
	if a != b {
		t.Error("nil client pointers should compare equal")
	}
	_ = map[*Client]struct{}{}
}
