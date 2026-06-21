package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
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
	defer func() { _ = c.Close() }()

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

func TestClient_DroppedNotificationsZeroWhenNoDrops(t *testing.T) {
	m := newMockTransport()
	c := mustClient(t, m)
	defer func() { _ = c.Close() }()

	if got := c.DroppedNotifications(); got != 0 {
		t.Fatalf("DroppedNotifications() = %d, want 0", got)
	}

	delivered := make(chan struct{})
	c.OnProgress(func(ProgressNotification) { close(delivered) })
	m.emit([]byte(`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progress":1}}`))
	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("notification was not delivered")
	}

	if got := c.DroppedNotifications(); got != 0 {
		t.Fatalf("DroppedNotifications() after delivered notification = %d, want 0", got)
	}
}

func TestClient_DroppedNotificationsReportsOverflow(t *testing.T) {
	m := newMockTransport()
	c := mustClient(t, m)

	firstStarted := make(chan struct{})
	releaseHandlers := make(chan struct{})
	var startOnce sync.Once
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(releaseHandlers) })
		_ = c.Close()
	})

	c.OnProgress(func(ProgressNotification) {
		startOnce.Do(func() { close(firstStarted) })
		<-releaseHandlers
	})

	const total = notifyQueueSize * 4
	m.emit([]byte(`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progress":0}}`))
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first notification handler did not start")
	}

	for i := 1; i < total; i++ {
		m.emit([]byte(`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progress":1}}`))
	}

	want := uint64(total - (notifyQueueSize + 1))
	if got := c.DroppedNotifications(); got != want {
		t.Fatalf("DroppedNotifications() = %d, want %d", got, want)
	}
	if got := c.notifier.dropped.Load(); got != c.DroppedNotifications() {
		t.Fatalf("notifier dropped counter = %d, accessor = %d", got, c.DroppedNotifications())
	}

	releaseOnce.Do(func() { close(releaseHandlers) })
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
	defer func() { _ = c.Close() }()

	// Handlers run on the notifier pump (off the read path), so synchronize on
	// channels rather than reading shared counters directly.
	clientLevelCh := make(chan ProgressNotification, 4)
	c.OnProgress(func(pn ProgressNotification) { clientLevelCh <- pn })

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

	// A token with no per-call handler falls through to the client-level handler.
	m.emit([]byte(`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"other","progress":2}}`))
	select {
	case pn := <-clientLevelCh:
		if pn.Progress != 2 {
			t.Errorf("client-level progress = %v, want 2", pn.Progress)
		}
	case <-time.After(time.Second):
		t.Fatal("client-level handler did not receive the fall-through notification")
	}

	// The per-call token must NOT have reached the client-level handler. The pump is
	// serial, so by the time the fall-through notification has been delivered above,
	// any client-level delivery for "tok-1" would already be queued ahead of it.
	select {
	case pn := <-clientLevelCh:
		t.Errorf("client-level handler unexpectedly fired again: %+v", pn)
	default:
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

func TestNotifier_OverflowDoesNotInvokeHandlersConcurrently(t *testing.T) {
	n := newNotifier()
	defer n.stop()

	const total = notifyQueueSize * 4

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	deliveredEnough := make(chan struct{})
	var deliveredOnce sync.Once
	var mu sync.Mutex
	delivered := make([]int, 0, notifyQueueSize+1)
	var inFlight atomic.Int32
	var overlapped atomic.Bool

	handler := func(seq int) func() {
		return func() {
			if inFlight.Add(1) != 1 {
				overlapped.Store(true)
			}
			defer inFlight.Add(-1)

			mu.Lock()
			delivered = append(delivered, seq)
			if len(delivered) == notifyQueueSize+1 {
				deliveredOnce.Do(func() { close(deliveredEnough) })
			}
			mu.Unlock()

			if seq == 0 {
				close(firstStarted)
				<-releaseFirst
			}
		}
	}

	n.enqueue(handler(0))
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first handler did not start")
	}

	for i := 1; i < total; i++ {
		n.enqueue(handler(i))
	}
	if got, want := n.dropped.Load(), uint64(total-(notifyQueueSize+1)); got != want {
		t.Fatalf("dropped = %d, want %d", got, want)
	}

	close(releaseFirst)
	select {
	case <-deliveredEnough:
	case <-time.After(2 * time.Second):
		t.Fatal("queued notifications were not drained")
	}

	mu.Lock()
	got := append([]int(nil), delivered...)
	mu.Unlock()
	if len(got) != notifyQueueSize+1 {
		t.Fatalf("delivered %d notifications, want %d", len(got), notifyQueueSize+1)
	}
	for i, seq := range got {
		if seq != i {
			t.Fatalf("delivered order[%d] = %d, want %d; full order: %v", i, seq, i, got)
		}
	}
	if overlapped.Load() {
		t.Fatal("notification handlers overlapped")
	}
}

func TestNotifier_OverflowLoggingIsBounded(t *testing.T) {
	var records atomic.Int64
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(countingSlogHandler{records: &records}))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })

	n := newNotifier()
	defer n.stop()

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	n.enqueue(func() {
		close(firstStarted)
		<-releaseFirst
	})
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first handler did not start")
	}

	const total = notifyQueueSize * 8
	for i := 1; i < total; i++ {
		n.enqueue(func() {})
	}

	if got := n.dropped.Load(); got == 0 {
		t.Fatal("expected notification overflow to drop work")
	}
	if got := records.Load(); got > 1 {
		t.Fatalf("overflow emitted %d log records, want at most 1 bounded record", got)
	}

	close(releaseFirst)
}

func TestNotifier_EnqueueAfterStopIsNoop(t *testing.T) {
	n := newNotifier()
	n.stop()

	fired := make(chan struct{})
	n.enqueue(func() { close(fired) })

	select {
	case <-fired:
		t.Fatal("handler fired after notifier was stopped")
	case <-time.After(50 * time.Millisecond):
	}
}

type countingSlogHandler struct {
	records *atomic.Int64
}

func (h countingSlogHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h countingSlogHandler) Handle(context.Context, slog.Record) error {
	h.records.Add(1)
	return nil
}

func (h countingSlogHandler) WithAttrs([]slog.Attr) slog.Handler {
	return h
}

func (h countingSlogHandler) WithGroup(string) slog.Handler {
	return h
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
