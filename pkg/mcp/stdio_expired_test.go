package mcp

import (
	"context"
	"errors"
	"testing"
)

// countingWriter records every frame handed to the transport's stdin.
type countingWriter struct{ frames []string }

func (w *countingWriter) Write(p []byte) (int, error) {
	if len(p) > 1 { // the trailing newline is written separately
		w.frames = append(w.frames, string(p))
	}
	return len(p), nil
}

func (w *countingWriter) Close() error { return nil }

// TestStdio_AbandonedFrameIsNotWritten pins that a queued frame whose caller
// has given up is dropped. The writer used to send it anyway, so a caller told
// the call failed would retry a non-idempotent tool the server had already run.
func TestStdio_AbandonedFrameIsNotWritten(t *testing.T) {
	w := &countingWriter{}
	tr := &stdioTransport{stdin: w, done: make(chan struct{})}

	abandoned, cancel := context.WithCancel(context.Background())
	cancel()

	err := tr.serve(&writeRequest{ctx: abandoned, payload: []byte(`{"abandoned":true}`)})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("serve err = %v, want context.Canceled", err)
	}
	if len(w.frames) != 0 {
		t.Errorf("the abandoned frame reached stdin: %v", w.frames)
	}

	// A live caller's frame still goes out.
	if err := tr.serve(&writeRequest{ctx: context.Background(), payload: []byte(`{"live":true}`)}); err != nil {
		t.Fatalf("serve: %v", err)
	}
	if len(w.frames) != 1 {
		t.Errorf("stdin saw %v, want the live frame only", w.frames)
	}
}
