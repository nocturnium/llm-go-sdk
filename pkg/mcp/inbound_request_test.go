package mcp

import (
	"bufio"
	"encoding/json"
	"io"
	"testing"
	"time"
)

// TestClassifyFrame pins the three-way classification that inbound routing
// depends on. The frameRequest cases are the ones that regressed: a
// server-initiated request carries BOTH a method and an id, so any classifier
// keyed only on the presence of an id treats it as a response.
func TestClassifyFrame(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		want   frameKind
		wantID string // "" when no id is expected
	}{
		{
			name: "response with result",
			raw:  `{"jsonrpc":"2.0","id":1,"result":{}}`,
			want: frameResponse,
		},
		{
			name: "response with error",
			raw:  `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"nope"}}`,
			want: frameResponse,
		},
		{
			name: "notification has method and no id",
			raw:  `{"jsonrpc":"2.0","method":"notifications/progress","params":{}}`,
			want: frameNotification,
		},
		{
			name: "null id is not an id",
			raw:  `{"jsonrpc":"2.0","id":null,"method":"notifications/message"}`,
			want: frameNotification,
		},
		{
			name:   "request has method and numeric id",
			raw:    `{"jsonrpc":"2.0","id":7,"method":"sampling/createMessage","params":{}}`,
			want:   frameRequest,
			wantID: "7",
		},
		{
			name:   "request with numeric string id preserves the string form",
			raw:    `{"jsonrpc":"2.0","id":"7","method":"roots/list"}`,
			want:   frameRequest,
			wantID: `"7"`,
		},
		{
			// messageID parses string ids with ParseInt, so a non-numeric string id
			// yields (0,false) there. Classification must not depend on that.
			name:   "request with non-numeric string id",
			raw:    `{"jsonrpc":"2.0","id":"abc","method":"roots/list"}`,
			want:   frameRequest,
			wantID: `"abc"`,
		},
		{
			name: "malformed json is treated as a response",
			raw:  `{not json`,
			want: frameResponse,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, id := classifyFrame([]byte(c.raw))
			if got != c.want {
				t.Errorf("classifyFrame kind = %v, want %v", got, c.want)
			}
			if string(id) != c.wantID {
				t.Errorf("classifyFrame id = %q, want %q", string(id), c.wantID)
			}
		})
	}
}

// TestIsNotificationFrameStillRejectsRequests guards the wrapper that
// extractFrames relies on: a server-initiated request must never be surfaced as
// a notification, or the HTTP path would hand it to notification handlers.
func TestIsNotificationFrameStillRejectsRequests(t *testing.T) {
	if isNotificationFrame([]byte(`{"jsonrpc":"2.0","id":1,"method":"sampling/createMessage"}`)) {
		t.Error("a frame with both method and id must not classify as a notification")
	}
	if !isNotificationFrame([]byte(`{"jsonrpc":"2.0","method":"notifications/progress"}`)) {
		t.Error("a frame with a method and no id must classify as a notification")
	}
}

// TestExtractFramesIgnoresInboundRequest pins that the HTTP framing path drops
// server-initiated requests rather than returning one as the POST's response.
// Returning it would resolve the caller with a frame carrying no result, i.e. a
// silent empty success.
func TestExtractFramesIgnoresInboundRequest(t *testing.T) {
	body := []byte("data: {\"jsonrpc\":\"2.0\",\"id\":9,\"method\":\"sampling/createMessage\"}\n\n" +
		"data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{}}\n\n" +
		"data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n")

	response, notifications := extractFrames(body)

	var probe struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(response, &probe); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(probe.Result) == 0 {
		t.Fatalf("response frame carries no result; got %s", response)
	}
	if len(notifications) != 1 {
		t.Fatalf("got %d notifications, want 1 (the request must not be surfaced as one)", len(notifications))
	}
}

// drainFrames continuously reads newline-delimited frames off the server side of
// the pipe and forwards them on a channel.
//
// A continuous drain is required, not a convenience: newPipeStdioTransport uses
// io.Pipe, whose writes block until a reader consumes them. The transport now
// writes replies to inbound requests from its read goroutine, so a test that
// reads only at specific points deadlocks the moment two frames are in flight.
func drainFrames(t *testing.T, r io.Reader) <-chan []byte {
	t.Helper()
	frames := make(chan []byte, 16)
	go func() {
		defer close(frames)
		reader := bufio.NewReader(r)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				frames <- line
			}
			if err != nil {
				return
			}
		}
	}()
	return frames
}

// nextFrame returns the next frame from drainFrames, failing on timeout.
func nextFrame(t *testing.T, frames <-chan []byte) []byte {
	t.Helper()
	select {
	case f, ok := <-frames:
		if !ok {
			t.Fatal("frame stream closed")
		}
		return f
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a frame")
		return nil
	}
}

// TestStdioInboundRequestDoesNotResolvePendingCall is the regression test for
// the misrouting bug.
//
// Before the fix, dispatchOne routed on the id alone, so a server-initiated
// request whose id collided with an in-flight client call was delivered to that
// call's waiter. decodeResult then found neither a result nor an error and
// returned a nil-result success — the caller got an empty answer with no error.
// Server ids are server-chosen and client ids start at 1, so collision is the
// likely case rather than an exotic one.
func TestStdioInboundRequestDoesNotResolvePendingCall(t *testing.T) {
	tr, serverReads, serverWrites := newPipeStdioTransport(t)
	// Close the server's writer first: stdioTransport.close waits for the read
	// loop to exit, and the read loop only sees EOF once this end of the pipe is
	// closed.
	defer func() { _ = serverWrites.Close(); _ = tr.close() }()

	type callResult struct {
		raw []byte
		err error
	}
	done := make(chan callResult, 1)
	go func() {
		payload, _ := encodeRequest(1, methodToolsCall, nil)
		raw, err := tr.request(t.Context(), 1, payload)
		done <- callResult{raw: raw, err: err}
	}()

	// Wait for the client's request to hit the wire before answering.
	frames := drainFrames(t, serverReads)
	nextFrame(t, frames)

	// The server interleaves its OWN request carrying the SAME id, then the real
	// response. The request must not satisfy the pending call.
	if _, err := serverWrites.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"sampling/createMessage","params":{}}` + "\n")); err != nil {
		t.Fatalf("write inbound request: %v", err)
	}
	writeStdioResult(serverWrites, json.RawMessage("1"), map[string]any{"content": []any{}, "isError": false})

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("request returned error: %v", got.err)
		}
		var probe struct {
			Result json.RawMessage `json:"result"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(got.raw, &probe); err != nil {
			t.Fatalf("unmarshal delivered frame: %v", err)
		}
		if probe.Method != "" {
			t.Fatalf("the server's REQUEST was delivered as the response to a pending call: %s", got.raw)
		}
		if len(probe.Result) == 0 {
			t.Fatalf("delivered frame carries no result (silent empty success): %s", got.raw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending call never resolved")
	}
}

// TestStdioUnhandledInboundRequestRepliesMethodNotFound pins that a client with
// no request handler answers rather than ignoring. Dropping the frame would leave
// the server blocked forever waiting for a response.
func TestStdioUnhandledInboundRequestRepliesMethodNotFound(t *testing.T) {
	tr, serverReads, serverWrites := newPipeStdioTransport(t)
	// Close the server's writer first: stdioTransport.close waits for the read
	// loop to exit, and the read loop only sees EOF once this end of the pipe is
	// closed.
	defer func() { _ = serverWrites.Close(); _ = tr.close() }()

	frames := drainFrames(t, serverReads)
	if _, err := serverWrites.Write([]byte(`{"jsonrpc":"2.0","id":"abc","method":"sampling/createMessage"}` + "\n")); err != nil {
		t.Fatalf("write inbound request: %v", err)
	}
	line := nextFrame(t, frames)

	var resp rpcResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		t.Fatalf("unmarshal reply: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
		t.Fatalf("reply = %s, want a MethodNotFound error", line)
	}
	// The id must be echoed verbatim, including its string form.
	if string(resp.ID) != `"abc"` {
		t.Errorf("reply id = %s, want \"abc\" echoed verbatim", resp.ID)
	}
}

// TestStdioServesResponsesAfterInboundRequest pins that handling an inbound
// request does not wedge the read loop for subsequent traffic.
func TestStdioServesResponsesAfterInboundRequest(t *testing.T) {
	tr, serverReads, serverWrites := newPipeStdioTransport(t)
	// Close the server's writer first: stdioTransport.close waits for the read
	// loop to exit, and the read loop only sees EOF once this end of the pipe is
	// closed.
	defer func() { _ = serverWrites.Close(); _ = tr.close() }()

	frames := drainFrames(t, serverReads)
	if _, err := serverWrites.Write([]byte(`{"jsonrpc":"2.0","id":99,"method":"roots/list"}` + "\n")); err != nil {
		t.Fatalf("write inbound request: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		payload, _ := encodeRequest(1, methodToolsList, nil)
		_, err := tr.request(t.Context(), 1, payload)
		done <- err
	}()

	// One frame is the MethodNotFound reply to the inbound request; the other is
	// the client's own request. Ordering between them is not guaranteed.
	nextFrame(t, frames)
	nextFrame(t, frames)
	writeStdioResult(serverWrites, json.RawMessage("1"), map[string]any{"tools": []any{}})

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("call after inbound request failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("read loop stopped serving responses after an inbound request")
	}
}

// TestEncodeResponseEmitsEmptyResultObject pins that a nil result serializes as
// an explicit {} rather than being omitted. rpcResponse.Result is omitempty, and
// a frame with neither result nor error is the malformed shape a peer cannot
// interpret — the same shape that made the misrouting bug silent.
func TestEncodeResponseEmitsEmptyResultObject(t *testing.T) {
	payload, err := encodeResponse(json.RawMessage("1"), nil)
	if err != nil {
		t.Fatalf("encodeResponse: %v", err)
	}
	var probe struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(probe.Result) != "{}" {
		t.Errorf("result = %q, want {}", string(probe.Result))
	}
}

// TestEncodeResponsePreservesIDForm pins that ids round-trip in their original
// JSON form. JSON-RPC requires the id be echoed unchanged, so answering the
// string "7" with the number 7 is a protocol violation.
func TestEncodeResponsePreservesIDForm(t *testing.T) {
	for _, id := range []string{"7", `"7"`, `"abc"`} {
		payload, err := encodeResponse(json.RawMessage(id), map[string]any{"ok": true})
		if err != nil {
			t.Fatalf("encodeResponse(%s): %v", id, err)
		}
		var resp rpcResponse
		if err := json.Unmarshal(payload, &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if string(resp.ID) != id {
			t.Errorf("id = %s, want %s", resp.ID, id)
		}
	}
}

// TestClientAnswersUnhandledInboundRequest pins the same default-answer
// behavior at the client level, over the mock transport: a client with no
// request handler must answer MethodNotFound rather than leave the server
// waiting. It also exercises the mock's inbound-request plumbing that later
// handler work builds on.
func TestClientAnswersUnhandledInboundRequest(t *testing.T) {
	m := newMockTransport()
	_ = mustClient(t, m)

	m.emitRequest([]byte(`{"jsonrpc":"2.0","id":42,"method":"sampling/createMessage","params":{}}`))

	sent := m.sentResponses()
	if len(sent) != 1 {
		t.Fatalf("got %d response frames, want 1", len(sent))
	}
	var resp rpcResponse
	if err := json.Unmarshal(sent[0], &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
		t.Fatalf("response = %s, want MethodNotFound", sent[0])
	}
	if string(resp.ID) != "42" {
		t.Errorf("response id = %s, want 42", resp.ID)
	}
}

// TestHTTPTransportCannotRespond pins the documented transport boundary: the
// HTTP transport has no path to send a frame outside a request/response
// exchange, and says so explicitly rather than silently discarding.
func TestHTTPTransportCannotRespond(t *testing.T) {
	tr := &httpTransport{}
	if err := tr.respond(t.Context(), []byte(`{}`)); err == nil {
		t.Fatal("expected respond to report that the transport cannot send unsolicited frames")
	}
}
