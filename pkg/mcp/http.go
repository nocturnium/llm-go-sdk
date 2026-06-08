package mcp

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/nocturnium/llm-go-sdk/internal/httpclient"
)

// httpTransport speaks JSON-RPC over MCP's Streamable HTTP transport: each
// message is POSTed and the response is read as either application/json or a
// short Server-Sent Events stream. It reuses the SDK's HTTP client for SSRF
// protection (MCP URLs are caller-supplied) and retries.
//
// Session resumption (the Mcp-Session-Id header) is not yet supported, so this
// transport targets stateless HTTP MCP endpoints.
type httpTransport struct {
	client  *httpclient.Client
	url     string
	headers map[string]string
}

func newHTTPTransport(url string, client *httpclient.Client, headers map[string]string) *httpTransport {
	return &httpTransport{client: client, url: url, headers: headers}
}

func (t *httpTransport) requestHeaders() map[string]string {
	h := map[string]string{
		"Accept": "application/json, text/event-stream",
	}
	for k, v := range t.headers {
		h[k] = v
	}
	return h
}

func (t *httpTransport) request(ctx context.Context, _ int64, payload []byte) ([]byte, error) {
	body, err := t.client.DoRaw(ctx, httpclient.Request{
		Method:  http.MethodPost,
		URL:     t.url,
		Headers: t.requestHeaders(),
		Body:    json.RawMessage(payload),
	})
	if err != nil {
		return nil, err
	}
	return extractJSONMessage(body), nil
}

func (t *httpTransport) notify(ctx context.Context, payload []byte) error {
	_, err := t.client.DoRaw(ctx, httpclient.Request{
		Method:  http.MethodPost,
		URL:     t.url,
		Headers: t.requestHeaders(),
		Body:    json.RawMessage(payload),
	})
	return err
}

func (t *httpTransport) close() error { return nil }
