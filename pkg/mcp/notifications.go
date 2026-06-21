package mcp

import (
	"encoding/json"
)

// notifyQueueSize bounds the notification pump's hand-off buffer. When the
// buffer fills behind a persistently slow handler, later notifications are
// dropped instead of blocking the transport read path or invoking handlers
// concurrently.
const notifyQueueSize = 64

// newNotifier constructs a notifier with its serial pump goroutine running. The
// pump invokes handlers off the transport read path so a slow or re-entrant
// handler cannot stall response delivery.
func newNotifier() *notifier {
	n := &notifier{
		queue: make(chan func(), notifyQueueSize),
		done:  make(chan struct{}),
	}
	go n.run()
	return n
}

// run drains queued handler invocations serially, preserving FIFO order for
// delivered notifications. It exits when stop closes done.
func (n *notifier) run() {
	for {
		select {
		case fn := <-n.queue:
			fn()
		case <-n.done:
			return
		}
	}
}

// enqueue hands a handler invocation to the pump without ever blocking the
// caller (the transport read goroutine). If the pump's buffer is full, the work
// is dropped and counted so a wedged handler cannot apply backpressure to
// response delivery; if the notifier is stopped, the work is dropped. The
// dropped counter is the source of truth for overflow accounting.
func (n *notifier) enqueue(fn func()) {
	select {
	case <-n.done:
		return
	default:
	}
	select {
	case n.queue <- fn:
	case <-n.done:
	default:
		n.dropped.Add(1)
	}
}

// stop shuts down the pump goroutine. It is idempotent and safe to call from
// Client.Close.
func (n *notifier) stop() {
	n.closeOnce.Do(func() { close(n.done) })
}

// OnProgress registers a handler for server-initiated progress notifications
// (notifications/progress). It returns the client for chaining. Pass nil to clear
// the handler. Handlers run on a single serial pump goroutine off the transport
// read path, so a slow handler does not stall in-flight RPCs and a handler may
// safely call back into the Client; handlers are not invoked concurrently with
// one another. If a handler blocks forever, later notifications fill the bounded
// hand-off queue and are then dropped rather than stalling RPC responses.
//
// Transport boundary: stdio reads progress notifications the server sends and
// offers them to this bounded pump. The Streamable HTTP transport only surfaces
// notifications that arrive
// interleaved on a POST response's SSE stream (e.g. progress emitted by the
// server while a tools/call is in flight); it has no standalone GET SSE listener,
// so out-of-band server pushes are not delivered.
func (c *Client) OnProgress(fn func(ProgressNotification)) *Client {
	c.notifier.mu.Lock()
	c.notifier.progressFn = fn
	c.notifier.mu.Unlock()
	return c
}

// OnLog registers a handler for server-initiated log notifications
// (notifications/message). It returns the client for chaining. Pass nil to clear
// the handler. The same transport boundary as [Client.OnProgress] applies.
func (c *Client) OnLog(fn func(LogMessage)) *Client {
	c.notifier.mu.Lock()
	c.notifier.logFn = fn
	c.notifier.mu.Unlock()
	return c
}

// DroppedNotifications returns the cumulative number of server notifications
// dropped because the client's bounded notification queue was full.
func (c *Client) DroppedNotifications() uint64 {
	if c == nil || c.notifier == nil {
		return 0
	}
	return c.notifier.dropped.Load()
}

// dispatchNotification parses a raw server-initiated JSON-RPC notification frame
// and routes it to the registered typed handler. It is the sink installed on the
// transport; unknown or unparseable notifications are ignored.
func (c *Client) dispatchNotification(raw []byte) {
	var probe struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return
	}
	switch probe.Method {
	case methodNotificationsProgress:
		var pn ProgressNotification
		if err := json.Unmarshal(probe.Params, &pn); err != nil {
			return
		}
		// A progress notification carrying a token registered by a CallTool option
		// is routed to that call's handler; otherwise it falls through to the
		// client-level handler. The handler is captured under the lock here (on the
		// read path) but invoked off it by the pump, so a blocking handler — or one
		// that calls back into the Client — cannot stall response delivery.
		c.notifier.mu.RLock()
		perCall := c.notifier.callProgress[progressTokenKey(pn.ProgressToken)]
		clientFn := c.notifier.progressFn
		c.notifier.mu.RUnlock()
		switch {
		case perCall != nil:
			c.notifier.enqueue(func() { perCall(pn) })
		case clientFn != nil:
			c.notifier.enqueue(func() { clientFn(pn) })
		}
	case methodNotificationsMessage:
		c.notifier.mu.RLock()
		fn := c.notifier.logFn
		c.notifier.mu.RUnlock()
		if fn == nil {
			return
		}
		var lm LogMessage
		if err := json.Unmarshal(probe.Params, &lm); err != nil {
			return
		}
		c.notifier.enqueue(func() { fn(lm) })
	default:
		// list_changed / resource updated notifications are recognized by name but
		// not yet surfaced through a typed handler; ignore them.
	}
}

// progressTokenKey normalizes a progress token (string or number on the wire)
// into a stable map key. An empty token yields "".
func progressTokenKey(token json.RawMessage) string {
	if len(token) == 0 {
		return ""
	}
	// Unquote a string token so the key matches the value sent in the request's
	// _meta.progressToken (which the client always sends as a string).
	var s string
	if err := json.Unmarshal(token, &s); err == nil {
		return s
	}
	return string(token)
}

// registerCallProgress records a per-call progress handler under token and
// returns a function that removes it; the caller defers the returned cleanup so
// the handler is dropped when the call returns.
func (c *Client) registerCallProgress(token string, fn func(ProgressNotification)) func() {
	c.notifier.mu.Lock()
	if c.notifier.callProgress == nil {
		c.notifier.callProgress = make(map[string]func(ProgressNotification))
	}
	c.notifier.callProgress[token] = fn
	c.notifier.mu.Unlock()
	return func() {
		c.notifier.mu.Lock()
		delete(c.notifier.callProgress, token)
		c.notifier.mu.Unlock()
	}
}
