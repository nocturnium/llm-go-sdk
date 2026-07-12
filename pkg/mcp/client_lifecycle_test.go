package mcp

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

// TestClient_ContextCancelStopsPump verifies that canceling the governing
// context tears the client down — stopping the notification pump — even when the
// caller never calls Close. Without this, an abandoned-but-canceled client
// leaks its pump goroutine (the WS-5 transport-lifecycle TODO).
func TestClient_ContextCancelStopsPump(t *testing.T) {
	srv := httptest.NewServer(mcpInitHandler(t, "", nil))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c, err := NewHTTPClient(ctx, srv.URL, WithAllowPrivateIPs(true))
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	// Deliberately do NOT call Close: canceling the context must stop the pump.

	select {
	case <-c.notifier.done:
		t.Fatal("pump stopped before the context was canceled")
	default:
	}

	cancel()

	select {
	case <-c.notifier.done:
		// Pump stopped: the context-cancellation hook tore the client down.
	case <-time.After(2 * time.Second):
		t.Fatal("canceling the context did not stop the notification pump (leak)")
	}
}

// TestClient_CloseIdempotent verifies Close is safe to call more than once — the
// property the context hook relies on, since cancel and an explicit Close can
// both run.
func TestClient_CloseIdempotent(t *testing.T) {
	srv := httptest.NewServer(mcpInitHandler(t, "sess-1", nil))
	defer srv.Close()

	c, err := NewHTTPClient(context.Background(), srv.URL, WithAllowPrivateIPs(true))
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	select {
	case <-c.notifier.done:
	default:
		t.Fatal("Close did not stop the pump")
	}
}

// TestClient_ExplicitCloseThenCancelIsSafe verifies that canceling the context
// after an explicit Close is harmless (the hook is deregistered / idempotent).
func TestClient_ExplicitCloseThenCancelIsSafe(t *testing.T) {
	srv := httptest.NewServer(mcpInitHandler(t, "", nil))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c, err := NewHTTPClient(ctx, srv.URL, WithAllowPrivateIPs(true))
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	cancel() // must not panic or double-tear-down
	// Give any (incorrectly re-fired) hook a moment; the test simply must not panic.
	time.Sleep(20 * time.Millisecond)
}
