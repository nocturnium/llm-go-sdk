package mcp

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestStdio_AbandonedWriteDoesNotWedgeLaterCalls pins that a caller who gives
// up on a write does not block the next one. The write path used to hold a
// mutex across the blocking pipe write, so an abandoned writer wedged every
// later request and notify with no context escape.
func TestStdio_AbandonedWriteDoesNotWedgeLaterCalls(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	tr := &stdioTransport{stdin: &blockingWriter{release: release}, done: make(chan struct{})}

	first, cancelFirst := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelFirst()
	if err := tr.write(first, []byte(`{"jsonrpc":"2.0"}`)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first write err = %v, want context.DeadlineExceeded", err)
	}

	second, cancelSecond := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelSecond()
	done := make(chan error, 1)
	go func() { done <- tr.write(second, []byte(`{"jsonrpc":"2.0"}`)) }()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("second write err = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the second write never returned: the abandoned writer wedged the transport")
	}
}
