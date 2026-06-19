package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"
)

// A client-level OnProgress handler must receive a progress notification the
// transport surfaces, with the typed fields parsed.
func TestClient_OnProgress(t *testing.T) {
	m := newMockTransport()
	c := mustClient(t, m)

	var got ProgressNotification
	var wg sync.WaitGroup
	wg.Add(1)
	c.OnProgress(func(pn ProgressNotification) {
		got = pn
		wg.Done()
	})

	m.emit([]byte(`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"t1","progress":3,"total":10,"message":"step"}}`))
	wg.Wait()

	if got.Progress != 3 || got.Total != 10 || got.Message != "step" {
		t.Errorf("progress = %+v, want progress=3 total=10 message=step", got)
	}
	if progressTokenKey(got.ProgressToken) != "t1" {
		t.Errorf("progress token = %q, want t1", progressTokenKey(got.ProgressToken))
	}
}

// A client-level OnLog handler must receive a log notification with its level and
// structured data preserved.
func TestClient_OnLog(t *testing.T) {
	m := newMockTransport()
	c := mustClient(t, m)

	var got LogMessage
	var wg sync.WaitGroup
	wg.Add(1)
	c.OnLog(func(lm LogMessage) {
		got = lm
		wg.Done()
	})

	m.emit([]byte(`{"jsonrpc":"2.0","method":"notifications/message","params":{"level":"warning","logger":"srv","data":{"k":"v"}}}`))
	wg.Wait()

	if got.Level != "warning" || got.Logger != "srv" {
		t.Errorf("log = %+v, want level=warning logger=srv", got)
	}
	if string(got.Data) != `{"k":"v"}` {
		t.Errorf("log data = %s, want {\"k\":\"v\"}", got.Data)
	}
}

// WithProgress must inject a progress token into the tools/call request and route
// only the notifications carrying that token to the per-call handler.
func TestClient_CallToolWithProgress(t *testing.T) {
	m := newMockTransport()
	m.queue(methodToolsCall, CallToolResult{Content: []ContentBlock{{Type: "text", Text: "done"}}})
	c := mustClient(t, m)

	// The mock resolves the request synchronously, so this test asserts the wiring
	// the option installs: a _meta.progressToken on the request and a per-call
	// handler that is cleaned up on return. Live per-token routing is covered by
	// TestClient_PerCallProgressRouting.
	if _, err := c.CallToolWithProgress(context.Background(), "slow", json.RawMessage(`{}`),
		WithProgress(func(ProgressNotification) {})); err != nil {
		t.Fatalf("CallToolWithProgress: %v", err)
	}

	// The request carried a _meta.progressToken.
	call, ok := m.lastCall(methodToolsCall)
	if !ok {
		t.Fatal("no tools/call recorded")
	}
	var p callToolParams
	if err := json.Unmarshal(call.params, &p); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if p.Meta == nil || p.Meta.ProgressToken == "" {
		t.Fatalf("expected a progress token in _meta, got %+v", p.Meta)
	}

	// After the call returns, its per-call handler is unregistered: a late
	// notification with the same token must NOT reach it.
	c.notifier.mu.RLock()
	_, stillRegistered := c.notifier.callProgress[p.Meta.ProgressToken]
	c.notifier.mu.RUnlock()
	if stillRegistered {
		t.Error("per-call progress handler should be unregistered after the call returns")
	}
}

// A progress notification whose token matches a live per-call handler routes to
// that handler and not the client-level handler.
func TestClient_PerCallProgressRouting(t *testing.T) {
	m := newMockTransport()
	c := mustClient(t, m)

	var clientLevel int
	c.OnProgress(func(ProgressNotification) { clientLevel++ })

	perCallCh := make(chan ProgressNotification, 1)
	cleanup := c.registerCallProgress("tok-1", func(pn ProgressNotification) { perCallCh <- pn })
	defer cleanup()

	// Token "tok-1" -> per-call handler.
	m.emit([]byte(`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"tok-1","progress":1}}`))
	select {
	case pn := <-perCallCh:
		if pn.Progress != 1 {
			t.Errorf("per-call progress = %v, want 1", pn.Progress)
		}
	case <-time.After(time.Second):
		t.Fatal("per-call handler did not receive the matching notification")
	}
	if clientLevel != 0 {
		t.Errorf("client-level handler fired %d times for a per-call token, want 0", clientLevel)
	}

	// A token with no per-call handler falls through to the client-level handler.
	m.emit([]byte(`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"other","progress":2}}`))
	if clientLevel != 1 {
		t.Errorf("client-level handler fired %d times, want 1", clientLevel)
	}
}

// Unparseable or unknown notifications must be ignored without panicking.
func TestClient_DispatchNotificationIgnoresJunk(t *testing.T) {
	m := newMockTransport()
	c := mustClient(t, m)
	fired := false
	c.OnProgress(func(ProgressNotification) { fired = true })

	c.dispatchNotification([]byte(`not json`))
	c.dispatchNotification([]byte(`{"jsonrpc":"2.0","method":"notifications/unknown"}`))
	c.dispatchNotification([]byte(`{"jsonrpc":"2.0","method":"notifications/progress","params":"bad"}`))
	if fired {
		t.Error("handler fired for malformed/unknown notifications")
	}
}

// End-to-end over the stdio transport: a notification frame interleaved on the
// server's output stream must reach the registered client handler, while the
// id-correlated response still resolves the request.
func TestStdioTransport_NotificationDelivery(t *testing.T) {
	tr, serverReads, serverWrites := newPipeStdioTransport(t)
	defer func() { _ = tr.close() }()

	got := make(chan []byte, 1)
	tr.onNotification(func(raw []byte) { got <- raw })

	go func() {
		defer func() { _ = serverWrites.Close() }()
		br := bufio.NewReader(serverReads)
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			id, _ := messageID(line)
			// Emit a notification first, then the id-correlated response.
			_, _ = serverWrites.Write([]byte("{\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progress\":5}}\n"))
			resp, _ := json.Marshal(rpcResponse{
				JSONRPC: jsonRPCVersion,
				ID:      json.RawMessage(strconv.FormatInt(id, 10)),
				Result:  json.RawMessage(`{"ok":true}`),
			})
			_, _ = serverWrites.Write(append(resp, '\n'))
		}
		if err != nil {
			return
		}
	}()

	payload, err := encodeRequest(1, "ping", nil)
	if err != nil {
		t.Fatalf("encodeRequest: %v", err)
	}
	raw, err := tr.request(context.Background(), 1, payload)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := decodeResult(raw, &out); err != nil || !out.OK {
		t.Fatalf("decodeResult: %v ok=%v", err, out.OK)
	}

	select {
	case raw := <-got:
		var probe struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(raw, &probe)
		if probe.Method != methodNotificationsProgress {
			t.Errorf("notification method = %q, want %q", probe.Method, methodNotificationsProgress)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("notification was not delivered to the sink")
	}
}
