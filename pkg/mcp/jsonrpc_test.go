package mcp

import (
	"encoding/json"
	"testing"
)

func TestMessageID(t *testing.T) {
	if id, ok := messageID([]byte(`{"jsonrpc":"2.0","id":42,"result":{}}`)); !ok || id != 42 {
		t.Errorf("messageID(response) = %d,%v; want 42,true", id, ok)
	}
	if _, ok := messageID([]byte(`{"jsonrpc":"2.0","method":"notifications/x"}`)); ok {
		t.Error("messageID(notification): expected ok=false")
	}
	if _, ok := messageID([]byte(`not json`)); ok {
		t.Error("messageID(garbage): expected ok=false")
	}
}

func TestExtractJSONMessage(t *testing.T) {
	plain := []byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`)
	if got := extractJSONMessage(plain); string(got) != string(plain) {
		t.Errorf("plain JSON: got %s", got)
	}

	sse := []byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n")
	got := extractJSONMessage(sse)
	var probe struct {
		Result struct {
			OK bool `json:"ok"`
		} `json:"result"`
	}
	if err := json.Unmarshal(got, &probe); err != nil || !probe.Result.OK {
		t.Errorf("SSE extraction failed: got %q err %v", got, err)
	}

	// A multi-event SSE stream (a notification before the response) must yield only
	// the response object, not a concatenation of both events.
	multi := []byte(
		"event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n" +
			"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n\n")
	got = extractJSONMessage(multi)
	probe.Result.OK = false
	if err := json.Unmarshal(got, &probe); err != nil || !probe.Result.OK {
		t.Errorf("multi-event SSE extraction failed: got %q err %v", got, err)
	}
}

func TestDecodeResultError(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"nope"}}`)
	err := decodeResult(raw, nil)
	if err == nil || err.Error() == "" {
		t.Fatalf("expected an rpc error, got %v", err)
	}
}

func TestCallToolResultText(t *testing.T) {
	r := &CallToolResult{Content: []ContentBlock{
		{Type: "text", Text: "line1"},
		{Type: "image", Text: ""},
		{Type: "text", Text: "line2"},
	}}
	if got := r.Text(); got != "line1\nline2" {
		t.Errorf("Text() = %q, want %q", got, "line1\nline2")
	}
}
