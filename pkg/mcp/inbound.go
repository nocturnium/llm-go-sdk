package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// maxInflightRequests bounds how many server-initiated request handlers run at
// once.
//
// This deliberately differs from the notification pump in two ways, because a
// request is not a notification:
//
//   - Overflow is REFUSED with an error response, not dropped. A dropped
//     notification is cosmetic; a dropped request leaves the server waiting
//     forever for a reply that will never come.
//   - Handlers run concurrently, not serially. A sampling request can take tens
//     of seconds, and a serial pump would head-of-line-block every other request
//     behind it.
const maxInflightRequests = 8

// inboundShutdownTimeout bounds how long Close waits for in-flight request
// handlers, matching the kill-after discipline the stdio transport uses for the
// subprocess. A wedged handler must not make Close hang forever.
const inboundShutdownTimeout = 2 * time.Second

// requestHandler serves one server-initiated request method. It returns the
// result to marshal into the response, or an error. Returning an *RPCError
// controls the wire code and message; any other error becomes CodeInternalError.
type requestHandler func(ctx context.Context, params json.RawMessage) (any, error)

// inbound routes server-initiated requests to registered handlers.
//
// It is pointer-held by Client for the same reason as notifier: a mutex by value
// would make the Client struct non-comparable.
type inbound struct {
	mu       sync.RWMutex
	handlers map[string]requestHandler

	// sem bounds concurrent handler execution. Acquisition is non-blocking:
	// a full sem means refuse, never queue and never block the read path.
	sem  chan struct{}
	wg   sync.WaitGroup
	done chan struct{}

	closeOnce sync.Once
	refused   atomic.Uint64
}

func newInbound() *inbound {
	return &inbound{
		handlers: make(map[string]requestHandler),
		sem:      make(chan struct{}, maxInflightRequests),
		done:     make(chan struct{}),
	}
}

// register installs a handler for a method. It is called during construction,
// before the client is handed to the caller.
func (in *inbound) register(method string, h requestHandler) {
	in.mu.Lock()
	defer in.mu.Unlock()
	in.handlers[method] = h
}

// handlerFor returns the handler for a method, if any.
func (in *inbound) handlerFor(method string) (requestHandler, bool) {
	in.mu.RLock()
	defer in.mu.RUnlock()
	h, ok := in.handlers[method]
	return h, ok
}

// acquire takes a concurrency slot without blocking, reporting whether one was
// free. A caller that gets false must answer the request with an error rather
// than wait: blocking here would stall the transport read path.
func (in *inbound) acquire() bool {
	select {
	case in.sem <- struct{}{}:
		return true
	default:
		in.refused.Add(1)
		return false
	}
}

func (in *inbound) release() { <-in.sem }

// stop waits for in-flight handlers to finish, bounded by
// inboundShutdownTimeout so a wedged handler cannot hang Close. Goroutines still
// running past the deadline are abandoned; their responses are written to a
// closed transport and discarded.
func (in *inbound) stop() {
	in.closeOnce.Do(func() {
		close(in.done)
		finished := make(chan struct{})
		go func() {
			in.wg.Wait()
			close(finished)
		}()
		select {
		case <-finished:
		case <-time.After(inboundShutdownTimeout):
		}
	})
}

// RefusedRequests returns the cumulative number of server-initiated requests
// refused because the bounded inbound-request concurrency limit was reached.
//
// Refused requests are answered with a JSON-RPC error, never silently dropped —
// contrast [Client.DroppedNotifications], where dropping is safe because no
// reply is expected. A non-zero count means a server asked this client to do
// more concurrent work than it will accept.
func (c *Client) RefusedRequests() uint64 {
	return c.inbound.refused.Load()
}

// dispatchRequest is the sink installed on the transport for server-initiated
// requests. It runs ON the transport read path and must never block.
//
// Parsing, handler lookup and refusal all happen here, synchronously: they are
// cheap, and a refusal must not consume a concurrency slot. Only actual handler
// execution is moved to a worker goroutine, so a slow handler cannot stall
// delivery of responses to this client's own in-flight calls.
func (c *Client) dispatchRequest(raw []byte, id json.RawMessage) {
	var probe struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil || probe.Method == "" {
		c.respondError(id, CodeParseError, "mcp: malformed request")
		return
	}

	handler, ok := c.inbound.handlerFor(probe.Method)
	if !ok {
		c.respondError(id, CodeMethodNotFound, "mcp: method not supported: "+probe.Method)
		return
	}

	select {
	case <-c.inbound.done:
		// Shutting down: answer rather than leave the server waiting.
		c.respondError(id, CodeInternalError, "mcp: client is shutting down")
		return
	default:
	}

	if !c.inbound.acquire() {
		c.respondError(id, CodeInternalError, "mcp: client is busy")
		return
	}

	c.inbound.wg.Add(1)
	go func() {
		defer c.inbound.wg.Done()
		defer c.inbound.release()
		c.serveRequest(id, handler, probe.Params)
	}()
}

// serveRequest runs a handler off the read path and writes its response. The
// handler's context is derived from the client's governing context, so canceling
// that context cancels in-flight handlers rather than orphaning them.
func (c *Client) serveRequest(id json.RawMessage, handler requestHandler, params json.RawMessage) {
	ctx, cancel := context.WithCancel(c.baseCtx)
	defer cancel()

	result, rpcErr := invokeGuarded(ctx, handler, params)
	if rpcErr != nil {
		c.respondError(id, rpcErr.Code, rpcErr.Message)
		return
	}
	payload, err := encodeResponse(id, result)
	if err != nil {
		c.respondError(id, CodeInternalError, "mcp: encode response failed")
		return
	}
	c.writeRaw(payload)
}

// invokeGuarded runs a handler, converting a panic into an InternalError
// response.
//
// A panicking handler must neither take down the client nor leave the server
// waiting for a reply. The panic value is deliberately NOT put on the wire: it
// can carry host internals (file paths, addresses, credentials in a formatted
// struct) and the peer is not necessarily trusted.
func invokeGuarded(ctx context.Context, handler requestHandler, params json.RawMessage) (result any, rpcErr *RPCError) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
			rpcErr = &RPCError{Code: CodeInternalError, Message: "mcp: request handler panicked"}
		}
	}()

	result, err := handler(ctx, params)
	if err != nil {
		var typed *RPCError
		if errors.As(err, &typed) {
			return nil, typed
		}
		return nil, &RPCError{Code: CodeInternalError, Message: err.Error()}
	}
	return result, nil
}

// respondError writes a JSON-RPC error response for a server-initiated request.
func (c *Client) respondError(id json.RawMessage, code int, message string) {
	payload, err := encodeErrorResponse(id, code, message)
	if err != nil {
		return
	}
	c.writeRaw(payload)
}

// writeRaw sends an already-encoded response frame. Failures are intentionally
// swallowed: the transport may be closed (a race with Close is normal), and
// there is no caller to surface the error to — the read loop must keep serving
// regardless.
func (c *Client) writeRaw(payload []byte) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.baseCtx), inboundShutdownTimeout)
	defer cancel()
	_ = c.transport.respond(ctx, payload)
}
