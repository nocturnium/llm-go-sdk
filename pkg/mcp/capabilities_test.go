package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// newMockTransportWithCaps builds a mock whose initialize result advertises the
// given capabilities JSON, so capability parsing can be exercised.
func newMockTransportWithCaps(t *testing.T, capsJSON string) *mockTransport {
	t.Helper()
	m := &mockTransport{results: map[string][][]byte{}, errs: map[string]*RPCError{}}
	m.queue(methodInitialize, InitializeResult{
		ProtocolVersion: protocolVersion,
		Capabilities:    json.RawMessage(capsJSON),
		ServerInfo:      Implementation{Name: "test-server", Version: "1.0"},
	})
	return m
}

func TestClient_ServerCapabilities(t *testing.T) {
	caps := `{
		"tools":{"listChanged":true},
		"resources":{"subscribe":true,"listChanged":false},
		"prompts":{"listChanged":true},
		"logging":{}
	}`
	c := mustClient(t, newMockTransportWithCaps(t, caps))

	sc := c.ServerCapabilities()
	if sc.Tools == nil || !sc.Tools.ListChanged {
		t.Errorf("Tools = %+v, want listChanged=true", sc.Tools)
	}
	if sc.Resources == nil || !sc.Resources.Subscribe || sc.Resources.ListChanged {
		t.Errorf("Resources = %+v, want subscribe=true listChanged=false", sc.Resources)
	}
	if sc.Prompts == nil || !sc.Prompts.ListChanged {
		t.Errorf("Prompts = %+v, want listChanged=true", sc.Prompts)
	}
	if sc.Logging == nil {
		t.Error("Logging = nil, want non-nil (advertised)")
	}
}

// A server that advertises no resources/prompts leaves those sub-capabilities nil
// so callers can gate calls on them.
func TestClient_ServerCapabilitiesAbsent(t *testing.T) {
	c := mustClient(t, newMockTransportWithCaps(t, `{"tools":{}}`))

	sc := c.ServerCapabilities()
	if sc.Tools == nil {
		t.Error("Tools = nil, want non-nil")
	}
	if sc.Resources != nil {
		t.Errorf("Resources = %+v, want nil", sc.Resources)
	}
	if sc.Prompts != nil {
		t.Errorf("Prompts = %+v, want nil", sc.Prompts)
	}
	if sc.Logging != nil {
		t.Errorf("Logging = %+v, want nil", sc.Logging)
	}
}

// Malformed capabilities must not fail the handshake; the typed view stays zero.
func TestClient_ServerCapabilitiesMalformed(t *testing.T) {
	c := mustClient(t, newMockTransportWithCaps(t, `"not-an-object"`))
	if sc := c.ServerCapabilities(); sc.Tools != nil || sc.Resources != nil {
		t.Errorf("expected zero capabilities on malformed input, got %+v", sc)
	}
}

// The empty default mock (no capabilities field) yields a zero ServerCapabilities.
func TestClient_ServerCapabilitiesEmpty(t *testing.T) {
	c := mustClient(t, newMockTransport())
	if sc := c.ServerCapabilities(); sc.Tools != nil || sc.Resources != nil || sc.Prompts != nil || sc.Logging != nil {
		t.Errorf("expected zero capabilities, got %+v", sc)
	}
	_ = context.Background()
}
