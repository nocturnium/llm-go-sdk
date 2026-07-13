package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// mcpInitHandler builds an httptest handler that completes the MCP handshake,
// optionally advertising a session id, and records any DELETE it receives.
func mcpInitHandler(t *testing.T, sessionID string, onDelete func(sess string)) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			if onDelete != nil {
				onDelete(r.Header.Get(mcpSessionIDHeader))
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var probe struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &probe)
		switch probe.Method {
		case methodInitialize:
			if sessionID != "" {
				w.Header().Set(mcpSessionIDHeader, sessionID)
			}
			result, _ := json.Marshal(InitializeResult{
				ProtocolVersion: protocolVersion,
				ServerInfo:      Implementation{Name: "http-test", Version: "1.0"},
			})
			out, _ := json.Marshal(rpcResponse{JSONRPC: jsonRPCVersion, ID: probe.ID, Result: result})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(out)
		case methodInitialized:
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}
}

// TestHTTPTransport_CloseSendsSessionDelete verifies that closing a client which
// holds an Mcp-Session-Id terminates the server session with an HTTP DELETE
// carrying that id, per the MCP Streamable HTTP spec.
func TestHTTPTransport_CloseSendsSessionDelete(t *testing.T) {
	const sessionID = "sess-abc-123"
	var (
		mu         sync.Mutex
		deleteSeen bool
		deleteSess string
	)
	srv := httptest.NewServer(mcpInitHandler(t, sessionID, func(sess string) {
		mu.Lock()
		deleteSeen = true
		deleteSess = sess
		mu.Unlock()
	}))
	defer srv.Close()

	ctx := context.Background()
	// httptest binds 127.0.0.1 and serves plain HTTP, so allow both private IPs and plain HTTP.
	c, err := NewHTTPClient(ctx, srv.URL, WithAllowPrivateIPs(true), WithAllowHTTP(true))
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !deleteSeen {
		t.Fatal("Close did not send a session-termination DELETE")
	}
	if deleteSess != sessionID {
		t.Errorf("DELETE Mcp-Session-Id = %q, want %q", deleteSess, sessionID)
	}
}

// TestHTTPTransport_CloseIgnoresDeleteRejection verifies the best-effort
// contract: a server that rejects session termination (405 Method Not Allowed)
// must not make Close fail.
func TestHTTPTransport_CloseIgnoresDeleteRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed) // server does not support termination
			return
		}
		body, _ := io.ReadAll(r.Body)
		var probe struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &probe)
		switch probe.Method {
		case methodInitialize:
			w.Header().Set(mcpSessionIDHeader, "sess-x")
			result, _ := json.Marshal(InitializeResult{
				ProtocolVersion: protocolVersion,
				ServerInfo:      Implementation{Name: "http-test", Version: "1.0"},
			})
			out, _ := json.Marshal(rpcResponse{JSONRPC: jsonRPCVersion, ID: probe.ID, Result: result})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(out)
		case methodInitialized:
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	ctx := context.Background()
	c, err := NewHTTPClient(ctx, srv.URL, WithAllowPrivateIPs(true), WithAllowHTTP(true))
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close must not fail when the server rejects session DELETE, got %v", err)
	}
}

// TestHTTPTransport_CloseWithoutSessionSendsNoDelete verifies that a client
// which never received a session id closes cleanly without a spurious DELETE.
func TestHTTPTransport_CloseWithoutSessionSendsNoDelete(t *testing.T) {
	var (
		mu         sync.Mutex
		deleteSeen bool
	)
	srv := httptest.NewServer(mcpInitHandler(t, "", func(string) {
		mu.Lock()
		deleteSeen = true
		mu.Unlock()
	}))
	defer srv.Close()

	ctx := context.Background()
	c, err := NewHTTPClient(ctx, srv.URL, WithAllowPrivateIPs(true), WithAllowHTTP(true))
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if deleteSeen {
		t.Fatal("Close sent a DELETE despite there being no server session")
	}
}
