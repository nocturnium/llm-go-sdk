package mcp

import (
	"encoding/json"
)

// OnProgress registers a handler for server-initiated progress notifications
// (notifications/progress). It returns the client for chaining. Pass nil to clear
// the handler. The handler may be invoked concurrently from the transport's read
// path; it must not block.
//
// Transport boundary: stdio surfaces every progress notification the server
// sends. The Streamable HTTP transport only surfaces notifications that arrive
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
		// client-level handler.
		c.notifier.mu.RLock()
		perCall := c.notifier.callProgress[progressTokenKey(pn.ProgressToken)]
		clientFn := c.notifier.progressFn
		c.notifier.mu.RUnlock()
		switch {
		case perCall != nil:
			perCall(pn)
		case clientFn != nil:
			clientFn(pn)
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
		fn(lm)
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
