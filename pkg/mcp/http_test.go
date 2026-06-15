package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

func TestHTTPTransport_StatefulSession(t *testing.T) {
	const sessionID = "session-123"
	var rejectedMissingSession atomic.Int32

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

		if probe.Method != methodInitialize && r.Header.Get(mcpSessionIDHeader) != sessionID {
			rejectedMissingSession.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"missing session"}}`))
			return
		}

		switch probe.Method {
		case methodInitialize:
			w.Header().Set(mcpSessionIDHeader, sessionID)
			writeResult(w, probe.ID, InitializeResult{
				ProtocolVersion: protocolVersion,
				ServerInfo:      Implementation{Name: "stateful-http-test", Version: "1.0"},
			})
		case methodInitialized:
			w.WriteHeader(http.StatusAccepted)
		case methodToolsList:
			writeResult(w, probe.ID, listToolsResult{Tools: []Tool{{Name: "stateful"}}})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	ctx := context.Background()
	c, err := NewHTTPClient(ctx, srv.URL, WithAllowPrivateIPs(true))
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	defer func() { _ = c.Close() }()

	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "stateful" {
		t.Fatalf("expected stateful tool, got %+v", tools)
	}
	if rejectedMissingSession.Load() != 0 {
		t.Fatalf("client sent %d post-initialize requests without session id", rejectedMissingSession.Load())
	}

	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":99,"method":"tools/list"}`))
	if err != nil {
		t.Fatalf("manual post without session: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("manual post status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if rejectedMissingSession.Load() != 1 {
		t.Fatalf("missing-session rejection count = %d, want 1", rejectedMissingSession.Load())
	}
}
