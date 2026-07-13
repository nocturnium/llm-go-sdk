package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk/v5"
)

// MountHTTP must connect, register the server's tools into the registry, and
// return a working closer in one call.
func TestMountHTTP(t *testing.T) {
	writeResult := func(w http.ResponseWriter, id json.RawMessage, result any) {
		b, _ := json.Marshal(result)
		out, _ := json.Marshal(rpcResponse{JSONRPC: jsonRPCVersion, ID: id, Result: b})
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
				ServerInfo:      Implementation{Name: "mount-test", Version: "1.0"},
			})
		case methodInitialized:
			w.WriteHeader(http.StatusAccepted)
		case methodToolsList:
			writeResult(w, probe.ID, listToolsResult{Tools: []Tool{{Name: "echo", Description: "echo"}}})
		case methodToolsCall:
			writeResult(w, probe.ID, CallToolResult{Content: []ContentBlock{{Type: "text", Text: "echoed"}}})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	reg := llms.NewToolRegistry()
	closer, err := MountHTTP(context.Background(), reg, srv.URL, WithAllowPrivateIPs(true), WithAllowHTTP(true), WithNamePrefix("m_"))
	if err != nil {
		t.Fatalf("MountHTTP: %v", err)
	}
	defer func() { _ = closer.Close() }()

	tools := reg.Tools()
	if len(tools) != 1 || tools[0].Function.Name != "m_echo" {
		t.Fatalf("expected one tool m_echo, got %+v", tools)
	}

	// The registered handler must round-trip through the mounted client.
	msg, err := reg.Handle(context.Background(), llms.ToolCall{
		ID:       "1",
		Type:     llms.ToolTypeFunction,
		Function: &llms.FunctionCall{Name: "m_echo", Arguments: "{}"},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if msg.Content != "echoed" {
		t.Errorf("tool result = %q, want echoed", msg.Content)
	}
}

// A connection failure must surface as an error and not leak a client.
func TestMountHTTPConnectError(t *testing.T) {
	reg := llms.NewToolRegistry()
	// SSRF protection is on by default; a private URL without WithAllowPrivateIPs
	// is rejected at connect time.
	_, err := MountHTTP(context.Background(), reg, "http://127.0.0.1:0")
	if err == nil {
		t.Fatal("expected a connection error")
	}
}

// MountStdio composes NewStdioClient + Register; a bad command must error.
func TestMountStdioBadCommand(t *testing.T) {
	reg := llms.NewToolRegistry()
	if _, err := MountStdio(context.Background(), reg, "definitely-not-a-real-binary-xyz", nil); err == nil {
		t.Fatal("expected an error launching a nonexistent command")
	}
	_ = io.Closer(nil)
}
