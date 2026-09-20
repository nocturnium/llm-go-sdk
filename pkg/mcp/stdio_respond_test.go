package mcp

import (
	"context"
	"errors"
	"testing"
	"time"
)

// blockingWriter stands in for a server that stopped draining its stdin: once
// the pipe buffer is full the write never returns.
type blockingWriter struct{ release chan struct{} }

func (w *blockingWriter) Write(p []byte) (int, error) {
	<-w.release
	return len(p), nil
}

func (w *blockingWriter) Close() error { return nil }

// TestStdioRespond_HonorsContext pins that a response write cannot park the
// read loop forever. respond used to discard its context and block in write,
// so a server that stopped reading stalled every in-flight caller waiting for
// responses already sitting unread in the pipe.
func TestStdioRespond_HonorsContext(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	tr := &stdioTransport{stdin: &blockingWriter{release: release}}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- tr.respond(ctx, []byte(`{"jsonrpc":"2.0"}`)) }()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("err = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("respond ignored its context and blocked on the write")
	}
}
