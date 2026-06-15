package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"testing"
	"time"
)

func TestMessageID(t *testing.T) {
	if id, ok := messageID([]byte(`{"jsonrpc":"2.0","id":42,"result":{}}`)); !ok || id != 42 {
		t.Errorf("messageID(response) = %d,%v; want 42,true", id, ok)
	}
	if id, ok := messageID([]byte(`{"jsonrpc":"2.0","id":"42","result":{}}`)); !ok || id != 42 {
		t.Errorf("messageID(string response) = %d,%v; want 42,true", id, ok)
	}
	if id, ok := messageID([]byte(`{"jsonrpc":"2.0","id":42.0,"result":{}}`)); !ok || id != 42 {
		t.Errorf("messageID(float response) = %d,%v; want 42,true", id, ok)
	}
	if _, ok := messageID([]byte(`{"jsonrpc":"2.0","id":42.5,"result":{}}`)); ok {
		t.Error("messageID(fractional response): expected ok=false")
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

	batch := []byte(`[{"jsonrpc":"2.0","method":"notifications/progress"},{"jsonrpc":"2.0","id":1,"result":{"ok":true}}]`)
	if got := extractJSONMessage(batch); string(got) != string(batch) {
		t.Errorf("batch JSON: got %s", got)
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

	batchSSE := []byte("event: message\ndata: [{\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"},{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}]\n\n")
	got = extractJSONMessage(batchSSE)
	if string(got) != string(batch) {
		t.Errorf("batch SSE extraction failed: got %s", got)
	}
}

func TestStdioTransport_StringIDResponse(t *testing.T) {
	tr, serverReads, serverWrites := newPipeStdioTransport(t)
	defer func() { _ = tr.close() }()

	go func() {
		defer func() { _ = serverWrites.Close() }()
		br := bufio.NewReader(serverReads)
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			resp := []byte(`{"jsonrpc":"2.0","id":"7","result":{"ok":true}}`)
			_, _ = serverWrites.Write(append(resp, '\n'))
		}
		if err != nil {
			return
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
		t.Fatal("expected string-id response to resolve the request")
	}
}

func TestStdioTransport_NullIDErrorResponse(t *testing.T) {
	tr, serverReads, serverWrites := newPipeStdioTransport(t)
	defer func() { _ = tr.close() }()

	go func() {
		defer func() { _ = serverWrites.Close() }()
		br := bufio.NewReader(serverReads)
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			resp := []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"invalid request"}}`)
			_, _ = serverWrites.Write(append(resp, '\n'))
		}
		if err != nil {
			return
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	payload, err := encodeRequest(7, "ping", nil)
	if err != nil {
		t.Fatalf("encodeRequest: %v", err)
	}
	raw, err := tr.request(ctx, 7, payload)
	if err != nil {
		t.Fatalf("request should be resolved by id:null error, got %v", err)
	}
	if err := decodeResult(raw, nil); err == nil {
		t.Fatal("expected decoded RPC error")
	}
}

func TestStdioTransport_BatchResponse(t *testing.T) {
	tr, serverReads, serverWrites := newPipeStdioTransport(t)
	defer func() { _ = tr.close() }()

	go func() {
		defer func() { _ = serverWrites.Close() }()
		br := bufio.NewReader(serverReads)
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			resp := []byte(`[{"jsonrpc":"2.0","method":"notifications/progress"},{"jsonrpc":"2.0","id":7,"result":{"ok":true}}]`)
			_, _ = serverWrites.Write(append(resp, '\n'))
		}
		if err != nil {
			return
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
		t.Fatal("expected batch response to resolve the request")
	}
}

func newPipeStdioTransport(t *testing.T) (*stdioTransport, *io.PipeReader, *io.PipeWriter) {
	t.Helper()
	clientReads, serverWrites := io.Pipe()
	serverReads, clientWrites := io.Pipe()
	tr := &stdioTransport{
		cmd:     &exec.Cmd{},
		stdin:   clientWrites,
		pending: make(map[int64]chan []byte),
		done:    make(chan struct{}),
	}
	go tr.readLoop(clientReads)
	return tr, serverReads, serverWrites
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
