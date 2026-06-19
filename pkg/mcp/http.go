package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/nocturnium/llm-go-sdk/v3/internal/httpclient"
)

const mcpSessionIDHeader = "Mcp-Session-Id"

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

func (t *httpTransport) close() error { return nil }
