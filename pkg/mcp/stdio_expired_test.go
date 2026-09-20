package mcp

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestStdio_ExpiredWriteIsNotExecuted pins that a frame whose caller has given
// up is not written. The writer used to send it anyway, so a caller told the
// call failed would retry a non-idempotent tool the server had already run.
func TestStdio_ExpiredWriteIsNotExecuted(t *testing.T) {
	block := make(chan struct{})
	written := make(chan struct{}, 4)
	tr := &stdioTransport{stdin: &recordingWriter{block: block, wrote: written}, done: make(chan struct{})}

	// Occupy the writer so the second frame waits in the queue.
	go func() { _ = tr.write(context.Background(), []byte(`{"first":true}`)) }()

	expired, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- tr.write(expired, []byte(`{"second":true}`)) }()

	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second write err = %v, want context.DeadlineExceeded", err)
	}
	close(block)

	// The first frame is written; the abandoned one must not be.
	deadline := time.After(time.Second)
	count := 0
	for count < 1 {
		select {
		case <-written:
			count++
		case <-deadline:
			t.Fatal("the first frame was never written")
		}
	}
	select {
	case <-written:
		t.Error("the abandoned frame was written after its caller gave up")
	case <-time.After(200 * time.Millisecond):
	}
}

type recordingWriter struct {
	block chan struct{}
	wrote chan struct{}
	first bool
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	if !w.first {
		w.first = true
		<-w.block
	}
	if len(p) > 1 {
		w.wrote <- struct{}{}
	}
	return len(p), nil
}

func (w *recordingWriter) Close() error { return nil }
