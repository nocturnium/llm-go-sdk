package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestClient_ListResourcesPagination(t *testing.T) {
	m := newMockTransport()
	m.queue(methodResourcesList, listResourcesResult{
		Resources:  []Resource{{URI: "file:///a", Name: "a"}},
		NextCursor: "page2",
	})
	m.queue(methodResourcesList, listResourcesResult{
		Resources: []Resource{{URI: "file:///b", Name: "b"}},
	})
	c := mustClient(t, m)

	res, err := c.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(res) != 2 || res[0].URI != "file:///a" || res[1].URI != "file:///b" {
		t.Errorf("expected [a b] across pages, got %+v", res)
	}
	// The second page must have been requested with the returned cursor.
	if call, ok := m.lastCall(methodResourcesList); ok {
		var p listResourcesParams
		_ = json.Unmarshal(call.params, &p)
		if p.Cursor != "page2" {
			t.Errorf("expected cursor=page2 on second call, got %q", p.Cursor)
		}
	}
}

// A server that returns the same non-empty nextCursor forever must terminate
// quickly via the cursor-cycle guard rather than looping until ctx cancellation.
func TestClient_ListResourcesCursorCycleGuard(t *testing.T) {
	m := newMockTransport()
	m.queue(methodResourcesList, listResourcesResult{
		Resources:  []Resource{{URI: "file:///a", Name: "a"}},
		NextCursor: "loop",
	})
	c := mustClient(t, m)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := c.ListResources(ctx)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("ListResources looped until context timeout instead of breaking on a repeated cursor")
	}
	if len(res) == 0 {
		t.Fatalf("expected at least one resource, got %+v", res)
	}
	if calls := m.callCount(methodResourcesList); calls != 2 {
		t.Errorf("expected exactly 2 list calls before the guard trips, got %d", calls)
	}
}

func TestClient_ListResourcesContextCanceled(t *testing.T) {
	m := newMockTransport()
	c := mustClient(t, m)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.ListResources(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestClient_ReadResource(t *testing.T) {
	m := newMockTransport()
	m.queue(methodResourcesRead, ReadResourceResult{Contents: []ResourceContents{
		{URI: "file:///a", MimeType: "text/plain", Text: "hello"},
		{URI: "file:///a.bin", MimeType: "application/octet-stream", Blob: "aGVsbG8="},
	}})
	c := mustClient(t, m)

	res, err := c.ReadResource(context.Background(), "file:///a")
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(res.Contents) != 2 || res.Contents[0].Text != "hello" || res.Contents[1].Blob != "aGVsbG8=" {
		t.Errorf("unexpected contents: %+v", res.Contents)
	}
	// The request must carry the requested URI.
	call, ok := m.lastCall(methodResourcesRead)
	if !ok {
		t.Fatal("no resources/read recorded")
	}
	var p readResourceParams
	if err := json.Unmarshal(call.params, &p); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if p.URI != "file:///a" {
		t.Errorf("unexpected read URI: %q", p.URI)
	}
}

// A protocol-level failure on resources/read must surface as an inspectable *RPCError.
func TestClient_ReadResourceRPCError(t *testing.T) {
	m := newMockTransport()
	m.errs[methodResourcesRead] = &RPCError{Code: CodeInvalidParams, Message: "no such resource"}
	c := mustClient(t, m)

	_, err := c.ReadResource(context.Background(), "file:///missing")
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("error = %v, want *RPCError", err)
	}
	if rpcErr.Code != CodeInvalidParams {
		t.Errorf("code = %d, want %d", rpcErr.Code, CodeInvalidParams)
	}
}
