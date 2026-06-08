package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"testing"

	llms "github.com/nocturnium/llm-go-sdk"
)

// mockTransport implements transport with a queue of canned results per method,
// so tests can model multi-call behavior (e.g. pagination) and inspect what was
// sent without closures (which keep the linter quiet about test fixtures).
type mockTransport struct {
	mu            sync.Mutex
	results       map[string][][]byte // queued result payloads per method (last is reused)
	errs          map[string]*rpcError
	calls         []recordedCall
	notifications []string
	closed        bool
}

type recordedCall struct {
	method string
	params json.RawMessage
}

func newMockTransport() *mockTransport {
	m := &mockTransport{results: map[string][][]byte{}, errs: map[string]*rpcError{}}
	m.queue(methodInitialize, InitializeResult{
		ProtocolVersion: protocolVersion,
		ServerInfo:      Implementation{Name: "test-server", Version: "1.0"},
	})
	return m
}

func (m *mockTransport) queue(method string, result any) {
	b, err := json.Marshal(result)
	if err != nil {
		panic(err)
	}
	m.results[method] = append(m.results[method], b)
}

func (m *mockTransport) request(_ context.Context, id int64, payload []byte) ([]byte, error) {
	var probe struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.calls = append(m.calls, recordedCall{method: probe.Method, params: probe.Params})
	var result []byte
	if q := m.results[probe.Method]; len(q) > 0 {
		result = q[0]
		if len(q) > 1 {
			m.results[probe.Method] = q[1:] // advance, retaining the final entry
		}
	}
	rpcErr := m.errs[probe.Method]
	m.mu.Unlock()

	resp := rpcResponse{JSONRPC: jsonRPCVersion, ID: json.RawMessage(strconv.FormatInt(id, 10))}
	switch {
	case rpcErr != nil:
		resp.Error = rpcErr
	case result != nil:
		resp.Result = result
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found: " + probe.Method}
	}
	return json.Marshal(resp)
}

func (m *mockTransport) notify(_ context.Context, payload []byte) error {
	var probe struct {
		Method string `json:"method"`
	}
	_ = json.Unmarshal(payload, &probe)
	m.mu.Lock()
	m.notifications = append(m.notifications, probe.Method)
	m.mu.Unlock()
	return nil
}

func (m *mockTransport) close() error {
	m.closed = true
	return nil
}

func (m *mockTransport) lastCall(method string) (recordedCall, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.calls) - 1; i >= 0; i-- {
		if m.calls[i].method == method {
			return m.calls[i], true
		}
	}
	return recordedCall{}, false
}

func mustClient(t *testing.T, m *mockTransport, opts ...Option) *Client {
	t.Helper()
	c, err := newClient(context.Background(), m, buildConfig(opts))
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	return c
}

func TestClient_InitializeHandshake(t *testing.T) {
	m := newMockTransport()
	c := mustClient(t, m)

	if c.ServerInfo().Name != "test-server" {
		t.Errorf("ServerInfo.Name = %q, want test-server", c.ServerInfo().Name)
	}
	// The client must send the initialized notification after initialize.
	if len(m.notifications) != 1 || m.notifications[0] != methodInitialized {
		t.Errorf("expected one %q notification, got %v", methodInitialized, m.notifications)
	}
}

func TestClient_InitializeError(t *testing.T) {
	m := newMockTransport()
	m.errs[methodInitialize] = &rpcError{Code: -32000, Message: "boom"}

	if _, err := newClient(context.Background(), m, buildConfig(nil)); err == nil {
		t.Fatal("expected initialize error")
	}
	if !m.closed {
		t.Error("expected transport closed after failed initialize")
	}
}

func TestClient_ListToolsPagination(t *testing.T) {
	m := newMockTransport()
	m.queue(methodToolsList, listToolsResult{Tools: []Tool{{Name: "a"}}, NextCursor: "page2"})
	m.queue(methodToolsList, listToolsResult{Tools: []Tool{{Name: "b"}}})
	c := mustClient(t, m)

	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 2 || tools[0].Name != "a" || tools[1].Name != "b" {
		t.Errorf("expected [a b] across pages, got %+v", tools)
	}
	// The second page must have been requested with the returned cursor.
	if call, ok := m.lastCall(methodToolsList); ok {
		var p listToolsParams
		_ = json.Unmarshal(call.params, &p)
		if p.Cursor != "page2" {
			t.Errorf("expected cursor=page2 on second call, got %q", p.Cursor)
		}
	}
}

func TestClient_CallTool(t *testing.T) {
	m := newMockTransport()
	m.queue(methodToolsCall, CallToolResult{Content: []ContentBlock{{Type: "text", Text: "echoed"}}})
	c := mustClient(t, m)

	res, err := c.CallTool(context.Background(), "echo", json.RawMessage(`{"x":1}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.Text() != "echoed" {
		t.Errorf("unexpected result text: %q", res.Text())
	}
	// The request must carry the tool name and raw arguments.
	call, ok := m.lastCall(methodToolsCall)
	if !ok {
		t.Fatal("no tools/call recorded")
	}
	var p callToolParams
	if err := json.Unmarshal(call.params, &p); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if p.Name != "echo" || string(p.Arguments) != `{"x":1}` {
		t.Errorf("unexpected call params: %+v", p)
	}
}

func TestClient_Register(t *testing.T) {
	m := newMockTransport()
	m.queue(methodToolsList, listToolsResult{Tools: []Tool{{
		Name:        "search",
		Description: "search the web",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
	}}})
	m.queue(methodToolsCall, CallToolResult{Content: []ContentBlock{{Type: "text", Text: "results"}}})
	c := mustClient(t, m, WithNamePrefix("web_"))

	reg := llms.NewToolRegistry()
	if err := c.Register(reg); err != nil {
		t.Fatalf("Register: %v", err)
	}

	tools := reg.Tools()
	if len(tools) != 1 || tools[0].Function.Name != "web_search" {
		t.Fatalf("expected one tool named web_search, got %+v", tools)
	}
	if string(tools[0].Function.Parameters) != `{"type":"object","properties":{"q":{"type":"string"}}}` {
		t.Errorf("input schema not propagated: %s", tools[0].Function.Parameters)
	}

	// The registered handler must round-trip through CallTool, and the remote tool
	// name (without the prefix) must be sent on the wire.
	msg, err := reg.Handle(llms.ToolCall{
		ID:       "1",
		Type:     llms.ToolTypeFunction,
		Function: &llms.FunctionCall{Name: "web_search", Arguments: `{"q":"go"}`},
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if msg.Content != "results" {
		t.Errorf("expected tool result 'results', got %q", msg.Content)
	}
	if call, ok := m.lastCall(methodToolsCall); ok {
		var p callToolParams
		_ = json.Unmarshal(call.params, &p)
		if p.Name != "search" {
			t.Errorf("expected un-prefixed remote name 'search', got %q", p.Name)
		}
	}
}

func TestClient_RegisterToolError(t *testing.T) {
	m := newMockTransport()
	m.queue(methodToolsList, listToolsResult{Tools: []Tool{{Name: "boom"}}})
	m.queue(methodToolsCall, CallToolResult{IsError: true, Content: []ContentBlock{{Type: "text", Text: "kaboom"}}})
	c := mustClient(t, m)

	reg := llms.NewToolRegistry()
	if err := c.Register(reg); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// A result flagged IsError surfaces as a tool-error message so RunTools lets the
	// model react rather than aborting the loop.
	msg, err := reg.Handle(llms.ToolCall{ID: "1", Type: llms.ToolTypeFunction, Function: &llms.FunctionCall{Name: "boom", Arguments: "{}"}})
	if err != nil {
		t.Fatalf("Handle returned a hard error: %v", err)
	}
	if msg.Content == "" {
		t.Error("expected an error tool-result message")
	}
}

// TestStdioTransport_RoundTrip exercises the stdio framing, background reader, and
// id correlation using in-memory pipes in place of a real subprocess.
func TestStdioTransport_RoundTrip(t *testing.T) {
	clientReads, serverWrites := io.Pipe() // server -> client
	serverReads, clientWrites := io.Pipe() // client -> server

	tr := &stdioTransport{
		cmd:     &exec.Cmd{},
		stdin:   clientWrites,
		pending: make(map[int64]chan []byte),
		done:    make(chan struct{}),
	}
	go tr.readLoop(clientReads)

	// Fake server: echo each request id back in a JSON-RPC response.
	go func() {
		defer func() { _ = serverWrites.Close() }()
		br := bufio.NewReader(serverReads)
		for {
			line, err := br.ReadBytes('\n')
			if len(line) > 0 {
				id, _ := messageID(line)
				resp, _ := json.Marshal(rpcResponse{
					JSONRPC: jsonRPCVersion,
					ID:      json.RawMessage(strconv.FormatInt(id, 10)),
					Result:  json.RawMessage(`{"ok":true}`),
				})
				_, _ = serverWrites.Write(append(resp, '\n'))
			}
			if err != nil {
				return
			}
		}
	}()

	payload, err := encodeRequest(7, "ping", nil)
	if err != nil {
		t.Fatalf("encodeRequest: %v", err)
	}
	raw, err := tr.request(context.Background(), 7, payload)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := decodeResult(raw, &out); err != nil {
		t.Fatalf("decodeResult: %v", err)
	}
	if !out.OK {
		t.Error("expected ok=true from echo server")
	}
	_ = tr.close()
}
