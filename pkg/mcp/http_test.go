package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHTTPTransport_RoundTrip exercises the Streamable HTTP transport end to end
// against an httptest server: initialize, the initialized notification, tools/list,
// and tools/call.
func TestHTTPTransport_RoundTrip(t *testing.T) {
	writeResult := func(w http.ResponseWriter, id json.RawMessage, result any) {
		b, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("marshal result: %v", err)
		}
		out, err := json.Marshal(rpcResponse{JSONRPC: jsonRPCVersion, ID: id, Result: b})
		if err != nil {
			t.Fatalf("marshal response: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(out)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var probe struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &probe)

		switch probe.Method {
		case methodInitialize:
			writeResult(w, probe.ID, InitializeResult{
				ProtocolVersion: protocolVersion,
				ServerInfo:      Implementation{Name: "http-test", Version: "1.0"},
			})
		case methodInitialized:
			w.WriteHeader(http.StatusAccepted)
		case methodToolsList:
			writeResult(w, probe.ID, listToolsResult{Tools: []Tool{{
				Name:        "echo",
				Description: "echo",
				InputSchema: json.RawMessage(`{"type":"object"}`),
			}}})
		case methodToolsCall:
			writeResult(w, probe.ID, CallToolResult{Content: []ContentBlock{{Type: "text", Text: "echoed"}}})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	ctx := context.Background()
	// httptest binds 127.0.0.1, so allow private IPs (which also permits HTTP).
	c, err := NewHTTPClient(ctx, srv.URL, WithAllowPrivateIPs(true))
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	defer func() { _ = c.Close() }()

	if c.ServerInfo().Name != "http-test" {
		t.Errorf("ServerInfo.Name = %q, want http-test", c.ServerInfo().Name)
	}

	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("expected one tool 'echo', got %+v", tools)
	}

	res, err := c.CallTool(ctx, "echo", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.Text() != "echoed" {
		t.Errorf("CallTool result = %q, want echoed", res.Text())
	}
}
