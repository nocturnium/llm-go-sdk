package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/nocturnium/llm-go-sdk/v6/internal/httpclient"
)

const mcpSessionIDHeader = "Mcp-Session-Id"

// sessionDeleteTimeout bounds the best-effort session-termination DELETE issued
// on close so a slow or unresponsive server cannot stall Close indefinitely.
const sessionDeleteTimeout = 5 * time.Second

// httpTransport speaks JSON-RPC over MCP's Streamable HTTP transport: each
// message is POSTed and the response is read as either application/json or a
// short Server-Sent Events stream. It reuses the SDK's HTTP client for SSRF
// protection (MCP URLs are caller-supplied) and retries. Stateful sessions are
// supported by capturing Mcp-Session-Id response headers and echoing the session
// id on later requests.
type httpTransport struct {
	client    *httpclient.Client
	url       string
	headers   map[string]string
	sessionMu sync.RWMutex
	sessionID string
	notifyMu  sync.RWMutex
	notifyFn  func(raw []byte)
	closeOnce sync.Once
}

func newHTTPTransport(url string, client *httpclient.Client, headers map[string]string) *httpTransport {
	return &httpTransport{client: client, url: url, headers: headers}
}

// onNotification registers a sink for server-initiated frames. The Streamable
// HTTP transport has no standalone GET SSE listener, so it can only surface
// notifications that arrive interleaved on a POST response's SSE stream (e.g.
// progress/log notifications emitted by the server while a tools/call is in
// flight). Notifications a server would push out-of-band over a separate GET SSE
// channel are not delivered by this client. See deliverNotifications.
func (t *httpTransport) onNotification(fn func(raw []byte)) {
	t.notifyMu.Lock()
	t.notifyFn = fn
	t.notifyMu.Unlock()
}

func (t *httpTransport) deliverNotifications(frames [][]byte) {
	if len(frames) == 0 {
		return
	}
	t.notifyMu.RLock()
	fn := t.notifyFn
	t.notifyMu.RUnlock()
	if fn == nil {
		return
	}
	for _, frame := range frames {
		fn(frame)
	}
}

func (t *httpTransport) requestHeaders() map[string]string {
	h := map[string]string{
		"Accept": "application/json, text/event-stream",
	}
	for k, v := range t.headers {
		h[k] = v
	}
	if sessionID := t.getSessionID(); sessionID != "" {
		h[mcpSessionIDHeader] = sessionID
	}
	return h
}

func (t *httpTransport) request(ctx context.Context, _ int64, payload []byte) ([]byte, error) {
	body, headers, err := t.client.DoRawWithHeaders(ctx, httpclient.Request{
		Method:  http.MethodPost,
		URL:     t.url,
		Headers: t.requestHeaders(),
		Body:    json.RawMessage(payload),
	})
	if err != nil {
		return nil, err
	}
	t.captureSessionID(headers)
	response, notifications := extractFrames(body)
	t.deliverNotifications(notifications)
	return response, nil
}

func (t *httpTransport) notify(ctx context.Context, payload []byte) error {
	_, headers, err := t.client.DoRawWithHeaders(ctx, httpclient.Request{
		Method:  http.MethodPost,
		URL:     t.url,
		Headers: t.requestHeaders(),
		Body:    json.RawMessage(payload),
	})
	if err == nil {
		t.captureSessionID(headers)
	}
	return err
}

func (t *httpTransport) getSessionID() string {
	t.sessionMu.RLock()
	defer t.sessionMu.RUnlock()
	return t.sessionID
}

func (t *httpTransport) captureSessionID(headers http.Header) {
	sessionID := headers.Get(mcpSessionIDHeader)
	if sessionID == "" {
		return
	}
	t.sessionMu.Lock()
	t.sessionID = sessionID
	t.sessionMu.Unlock()
}

// close terminates the server-side session, if one was established. Per the MCP
// Streamable HTTP spec, a client that holds an Mcp-Session-Id SHOULD send an
// HTTP DELETE to the endpoint to let the server release the session's
// resources. It is best-effort: a server that does not support explicit
// termination may respond 405 Method Not Allowed, and any transport error on
// teardown is not worth surfacing — so the result is ignored and close never
// fails on this account. With no session there is nothing to terminate.
func (t *httpTransport) close() error {
	// Idempotent: close may be invoked both by an explicit Client.Close and by the
	// context-cancellation hook, so the DELETE must be sent at most once.
	t.closeOnce.Do(func() {
		sessionID := t.getSessionID()
		if sessionID == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), sessionDeleteTimeout)
		defer cancel()
		_, _, _ = t.client.DoRawWithHeaders(ctx, httpclient.Request{
			Method:  http.MethodDelete,
			URL:     t.url,
			Headers: t.requestHeaders(),
		})
	})
	return nil
}
